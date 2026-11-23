package alert

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

func TestChartStepAndCandles(t *testing.T) {
	fidelity, bucket := ChartStep(24 * time.Hour)
	if fidelity != 2 || bucket != 15*time.Minute {
		t.Fatalf("24h step fidelity=%d bucket=%s", fidelity, bucket)
	}
	fidelity, bucket = ChartStep(6 * time.Hour)
	if fidelity != 1 || bucket != 5*time.Minute {
		t.Fatalf("6h step fidelity=%d bucket=%s", fidelity, bucket)
	}
	if got := ClampChartWindow(time.Minute); got != 15*time.Minute {
		t.Fatalf("min clamp %s", got)
	}
	if got := ClampChartWindow(200 * 24 * time.Hour); got != 90*24*time.Hour {
		t.Fatalf("max clamp %s", got)
	}

	start := int64(1_000_000)
	points := []polymarket.PricePoint{
		{Time: start + 10, Price: 0.20},
		{Time: start + 80, Price: 0.50},
		{Time: start + 140, Price: 0.30},
		{Time: start + 400, Price: 0.10},
		{Time: start - 5, Price: 0.99},
	}
	got := BuildCandles(points, start, start+900, 5*time.Minute)
	if len(got) != 2 {
		t.Fatalf("candles %+v", got)
	}
	if got[0].Open != 0.20 || got[0].High != 0.50 || got[0].Low != 0.20 || got[0].Close != 0.30 {
		t.Fatalf("first %+v", got[0])
	}
	if got[1].Open != 0.10 || got[1].Close != 0.10 || got[1].Start != start+300 {
		t.Fatalf("second %+v", got[1])
	}
}

func TestRenderCandlesPNG(t *testing.T) {
	start := time.Now().Add(-time.Hour).Unix()
	rep := ChartReport{
		Title:   "Will it happen?",
		Window:  time.Hour,
		Bucket:  5 * time.Minute,
		Outcome: "Yes",
		Candles: []Candle{
			{Start: start, Open: 0.40, High: 0.55, Low: 0.35, Close: 0.50},
			{Start: start + 300, Open: 0.50, High: 0.52, Low: 0.22, Close: 0.25},
		},
	}
	pngBytes, err := RenderCandles(rep, start, start+3600)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != chartW || cfg.Height != chartH {
		t.Fatalf("size %dx%d", cfg.Width, cfg.Height)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	var green, red bool
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y && !(green && red); y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if g > 20000 && g > r*2 && g > bl {
				green = true
			}
			if r > 20000 && r > g*2 && r > bl {
				red = true
			}
		}
	}
	if !green || !red {
		t.Fatalf("green=%v red=%v", green, red)
	}
}

func TestFormatChartCaptionReply(t *testing.T) {
	rep := ChartReport{
		Title:   "Fed decision in 2026?",
		URL:     "https://polymarket.com/event/fed/will-the-fed-hike",
		Outcome: "Yes",
		Window:  24 * time.Hour,
		Bucket:  15 * time.Minute,
		Candles: []Candle{{Open: 0.31, Close: 0.26}, {Open: 0.26, Close: 0.26}},
	}
	text := FormatChartCaption(rep)
	if text != rep.Title {
		t.Fatalf("caption %q", text)
	}
	got, n := MarketFromReply(text)
	if n != 1 || got != rep.Title {
		t.Fatalf("reply n=%d %q\n%s", n, got, text)
	}
}

type fakeChartAPI struct {
	hit     polymarket.SearchMarket
	hist    polymarket.PriceHistory
	books   []polymarket.OutcomeBook
	bookErr error
}

func (f fakeChartAPI) FindMarket(context.Context, string) (polymarket.SearchMarket, error) {
	return f.hit, nil
}
func (f fakeChartAPI) SearchMarkets(context.Context, string) ([]polymarket.SearchMarket, error) {
	return []polymarket.SearchMarket{f.hit}, nil
}
func (f fakeChartAPI) FetchPositions(context.Context, polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	return nil, nil
}
func (f fakeChartAPI) FetchYesPriceHistory(context.Context, string, int64, int64, int) (polymarket.PriceHistory, error) {
	return f.hist, nil
}
func (f fakeChartAPI) FetchOutcomeBooks(context.Context, string) ([]polymarket.OutcomeBook, error) {
	return f.books, f.bookErr
}

func TestFetchChart(t *testing.T) {
	now := time.Now().Unix()
	api := fakeChartAPI{
		hit: polymarket.SearchMarket{Market: polymarket.Market{
			ConditionID: "0xabc",
			Question:    "Will it happen?",
			Slug:        "will-it",
			URL:         "https://polymarket.com/event/ev/will-it",
		}, Active: true},
		hist: polymarket.PriceHistory{
			Outcome: "Yes",
			Points: []polymarket.PricePoint{
				{Time: now - 50*60, Price: 0.40},
				{Time: now - 40*60, Price: 0.55},
				{Time: now - 10*60, Price: 0.33},
			},
		},
	}
	rep, err := FetchChart(context.Background(), api, "https://polymarket.com/event/ev/will-it", time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Title != "Will it happen?" || rep.URL == "" || len(rep.Candles) == 0 || len(rep.PNG) < 8 {
		t.Fatalf("%+v png=%d", rep.Title, len(rep.PNG))
	}
	if rep.Window != time.Hour || rep.Bucket != time.Minute {
		t.Fatalf("window %s bucket %s", rep.Window, rep.Bucket)
	}
	if len(rep.Books) != 0 {
		t.Fatalf("books %+v", rep.Books)
	}
}

func TestFetchChartKeepsBook(t *testing.T) {
	now := time.Now().Unix()
	api := fakeChartAPI{
		hit: polymarket.SearchMarket{Market: polymarket.Market{
			ConditionID: "0xabc",
			Question:    "Will it happen?",
			Slug:        "will-it",
		}, Active: true},
		hist: polymarket.PriceHistory{
			Outcome: "Yes",
			Points:  []polymarket.PricePoint{{Time: now - 60, Price: 0.4}, {Time: now - 30, Price: 0.5}},
		},
		books: []polymarket.OutcomeBook{{
			Outcome: "Yes",
			Tick:    0.01,
			Asks:    []polymarket.BookLevel{{Price: 0.46, Size: 5}},
			Bids:    []polymarket.BookLevel{{Price: 0.42, Size: 50}},
		}},
	}
	rep, err := FetchChart(context.Background(), api, "will-it", time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Books) != 1 || FormatChartCaption(rep) != "Will it happen?" {
		t.Fatalf("books=%d caption=%q", len(rep.Books), FormatChartCaption(rep))
	}
	plain := rep
	plain.Books = nil
	without, err := RenderCandles(plain, now-3600, now)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(without, rep.PNG) {
		t.Fatal("book overlay missing")
	}
}

func TestFetchChartIgnoresBookError(t *testing.T) {
	now := time.Now().Unix()
	api := fakeChartAPI{
		hit: polymarket.SearchMarket{Market: polymarket.Market{
			ConditionID: "0xabc",
			Question:    "Will it happen?",
		}, Active: true},
		hist: polymarket.PriceHistory{
			Points: []polymarket.PricePoint{{Time: now - 60, Price: 0.4}},
		},
		bookErr: errors.New("clob down"),
	}
	rep, err := FetchChart(context.Background(), api, "will-it", time.Hour, nil)
	if err != nil || len(rep.PNG) < 8 || len(rep.Books) != 0 {
		t.Fatalf("err=%v books=%d png=%d", err, len(rep.Books), len(rep.PNG))
	}
}

func TestChartBookOverlayCorner(t *testing.T) {
	lines := chartBookLines([]polymarket.OutcomeBook{{
		Outcome: "Yes",
		Tick:    0.01,
		Asks:    []polymarket.BookLevel{{Price: 0.44, Size: 20}, {Price: 0.46, Size: 1200}},
		Bids:    []polymarket.BookLevel{{Price: 0.42, Size: 50}},
	}})
	want := []string{"46¢  1.2k", "44¢    20", "- - -", "42¢    50"}
	if len(lines) != len(want) {
		t.Fatalf("lines %#v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d %q", i, lines[i])
		}
	}

	start := time.Now().Add(-time.Hour).Unix()
	rep := ChartReport{
		Window: time.Hour,
		Bucket: 5 * time.Minute,
		Candles: []Candle{
			{Start: start, Open: 0.50, High: 0.55, Low: 0.48, Close: 0.52},
		},
		Books: []polymarket.OutcomeBook{{
			Outcome: "Yes",
			Tick:    0.01,
			Asks:    []polymarket.BookLevel{{Price: 0.46, Size: 5}},
			Bids:    []polymarket.BookLevel{{Price: 0.42, Size: 50}},
		}},
	}
	pngBytes, err := RenderCandles(rep, start, start+3600)
	if err != nil {
		t.Fatal(err)
	}
	plain := rep
	plain.Books = nil
	without, err := RenderCandles(plain, start, start+3600)
	if err != nil {
		t.Fatal(err)
	}
	withImg, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	withoutImg, err := png.Decode(bytes.NewReader(without))
	if err != nil {
		t.Fatal(err)
	}
	topDiff := cornerDiff(withImg, withoutImg, 64, 28, 320, 240)
	botDiff := cornerDiff(withImg, withoutImg, 64, 360, 320, 520)
	if topDiff == 0 || botDiff != 0 {
		t.Fatalf("book should sit in the open top-left, top=%d bottom=%d", topDiff, botDiff)
	}

	busy := image.NewRGBA(image.Rect(0, 0, chartW, chartH))
	bg := color.RGBA{248, 249, 251, 255}
	fill(busy, bg)
	for y := 0; y < 180; y++ {
		for x := 60; x < 220; x++ {
			busy.SetRGBA(x, y, colorInk())
		}
	}
	const scale = 1
	blockW, blockH := 40, 30
	x, y := bookOrigin(busy, blockW, blockH, 64, 28, chartH-46, bg)
	if y < 200 {
		t.Fatalf("busy top should use the bottom corner, origin %d,%d", x, y)
	}
}

func cornerDiff(a, b image.Image, x0, y0, x1, y1 int) int {
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				n++
			}
		}
	}
	return n
}

func colorInk() color.RGBA {
	return color.RGBA{22, 150, 82, 255}
}
