package alert

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const (
	defaultChartWindow = 24 * time.Hour
	minChartWindow     = 15 * time.Minute
	maxChartWindow     = 90 * 24 * time.Hour
)

// ErrNoPriceHistory is a resolved market with no samples in the window.
var ErrNoPriceHistory = errors.New("no price history")

// Candle is one OHLC bucket of sampled Yes prices.
type Candle struct {
	Start int64
	Open  float64
	High  float64
	Low   float64
	Close float64
}

// ChartReport is a rendered /chart reply.
type ChartReport struct {
	Query   string
	Title   string
	Slug    string
	URL     string
	Outcome string
	Window  time.Duration
	Bucket  time.Duration
	Candles []Candle
	PNG     []byte
}

type chartAPI interface {
	posMarketAPI
	positionLookup
	FetchYesPriceHistory(ctx context.Context, conditionID string, start, end int64, fidelityMin int) (polymarket.PriceHistory, error)
}

// ClampChartWindow snaps a requested window into the range /chart will fetch.
func ClampChartWindow(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultChartWindow
	}
	if d < minChartWindow {
		return minChartWindow
	}
	if d > maxChartWindow {
		return maxChartWindow
	}
	return d
}

// ChartStep picks the CLOB sample spacing and the candle width for a window.
// Samples are finer than a candle so each candle has an open, high, low, and close.
func ChartStep(window time.Duration) (fidelityMin int, bucket time.Duration) {
	window = ClampChartWindow(window)
	mins := int(window / time.Minute)
	if mins < 1 {
		mins = 1
	}
	fidelityMin = mins / 720
	if fidelityMin < 1 {
		fidelityMin = 1
	}
	if fidelityMin > 24*60 {
		fidelityMin = 24 * 60
	}
	bucket = snapBucket(window / 72)
	if bucket < time.Duration(fidelityMin)*time.Minute {
		bucket = snapBucket(time.Duration(fidelityMin) * time.Minute)
		if bucket < time.Duration(fidelityMin)*time.Minute {
			bucket = time.Duration(fidelityMin) * time.Minute
		}
	}
	return fidelityMin, bucket
}

func snapBucket(d time.Duration) time.Duration {
	steps := []time.Duration{
		time.Minute,
		5 * time.Minute,
		15 * time.Minute,
		30 * time.Minute,
		time.Hour,
		2 * time.Hour,
		4 * time.Hour,
		12 * time.Hour,
		24 * time.Hour,
	}
	best := steps[0]
	for _, s := range steps {
		if s <= d {
			best = s
		}
	}
	return best
}

// BuildCandles groups oldest-first samples into OHLC buckets of width bucket,
// aligned to start. Empty buckets are omitted. Times are unix seconds.
func BuildCandles(points []polymarket.PricePoint, start, end int64, bucket time.Duration) []Candle {
	if len(points) == 0 || end <= start {
		return nil
	}
	sec := int64(bucket / time.Second)
	if sec < 60 {
		sec = 60
	}
	ordered := append([]polymarket.PricePoint(nil), points...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Time == ordered[j].Time {
			return i < j
		}
		return ordered[i].Time < ordered[j].Time
	})
	var out []Candle
	var cur *Candle
	var curKey int64
	for _, p := range ordered {
		if p.Time < start || p.Time > end || p.Price < 0 || p.Price > 1 {
			continue
		}
		key := start + ((p.Time-start)/sec)*sec
		if cur == nil || key != curKey {
			out = append(out, Candle{Start: key, Open: p.Price, High: p.Price, Low: p.Price, Close: p.Price})
			cur = &out[len(out)-1]
			curKey = key
			continue
		}
		if p.Price > cur.High {
			cur.High = p.Price
		}
		if p.Price < cur.Low {
			cur.Low = p.Price
		}
		cur.Close = p.Price
	}
	return out
}

// FetchChart resolves a Polymarket market the same way /ob does, loads Yes
// price samples, and renders a candle PNG.
func FetchChart(ctx context.Context, api chartAPI, query string, window time.Duration, wallets []sharps.Wallet) (ChartReport, error) {
	query = strings.TrimSpace(query)
	window = ClampChartWindow(window)
	rep := ChartReport{Query: query, Window: window}
	if query == "" {
		return rep, fmt.Errorf("empty market query")
	}
	hit, err := resolvePosMarket(ctx, api, query, wallets)
	if err != nil {
		return rep, err
	}
	rep.Title = chartTitle(hit)
	rep.Slug = hit.Market.Slug
	rep.URL = hit.Market.URL
	fidelity, bucket := ChartStep(window)
	rep.Bucket = bucket
	end := time.Now().Unix()
	start := end - int64(window/time.Second)
	hist, err := api.FetchYesPriceHistory(ctx, hit.Market.ConditionID, start, end, fidelity)
	if err != nil {
		return rep, err
	}
	rep.Outcome = hist.Outcome
	if rep.Outcome == "" {
		rep.Outcome = "Yes"
	}
	rep.Candles = BuildCandles(hist.Points, start, end, bucket)
	if len(rep.Candles) == 0 {
		return rep, ErrNoPriceHistory
	}
	png, err := RenderCandles(rep, start, end)
	if err != nil {
		return rep, err
	}
	rep.PNG = png
	return rep, nil
}

func chartTitle(hit polymarket.SearchMarket) string {
	if t := strings.TrimSpace(hit.Market.Question); t != "" {
		return t
	}
	if t := strings.TrimSpace(hit.GroupItemTitle); t != "" {
		return t
	}
	return strings.TrimSpace(hit.Market.Slug)
}

// FormatChartCaption is the text under the photo. A Polymarket URL, when
// present, is what a reply uses as the market.
func FormatChartCaption(r ChartReport) string {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = strings.TrimSpace(r.Slug)
	}
	if title == "" {
		title = strings.TrimSpace(r.Query)
	}
	outcome := strings.TrimSpace(r.Outcome)
	if outcome == "" {
		outcome = "Yes"
	}
	move := ""
	if n := len(r.Candles); n > 0 {
		move = formatCents(r.Candles[n-1].Close)
		if r.Candles[0].Open != r.Candles[n-1].Close {
			move = formatCents(r.Candles[0].Open) + " → " + move
		}
		move = " " + move
	}
	var b strings.Builder
	b.WriteString(title)
	b.WriteByte('\n')
	fmt.Fprintf(&b, "%s · %s candles · %s%s · %s",
		FormatChartWindow(r.Window),
		FormatChartWindow(r.Bucket),
		outcome,
		move,
		chartZoneLabel(),
	)
	if u := strings.TrimSpace(r.URL); u != "" {
		b.WriteByte('\n')
		b.WriteString(u)
	}
	s := b.String()
	if runes := []rune(s); len(runes) > 1000 {
		s = string(runes[:1000])
	}
	return s
}

// FormatChartWindow prints a duration the way /chart accepts it.
func FormatChartWindow(d time.Duration) string {
	if d <= 0 {
		return "24h"
	}
	if d%(24*time.Hour) == 0 && d >= 48*time.Hour {
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return d.Truncate(time.Minute).String()
}
