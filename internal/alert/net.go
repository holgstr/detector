package alert

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const (
	defaultNetWindow = 24 * time.Hour
	minNetWindow     = 15 * time.Minute
	maxNetWindow     = 7 * 24 * time.Hour
	netShareEps      = 0.5
	telegramChunk    = 3900
)

type activitySinceLookup interface {
	FetchActivitySinceBatch(ctx context.Context, users []string, since int64, typ string) ([]polymarket.Activity, bool, error)
}

// MarketDelta is the signed net-share change in one market over a window.
type MarketDelta struct {
	Title   string
	Slug    string
	Size    float64
	Outcome string
}

// TraderDelta is one wallet's non-zero market nets.
type TraderDelta struct {
	Name    string
	Wallet  string
	Markets []MarketDelta
}

// NetReport is /net output: traders who actually moved net exposure.
type NetReport struct {
	Window    time.Duration
	Since     int64
	Truncated bool
	Traders   []TraderDelta
}

// ResolveNetWallets picks tracked wallets for /net [trader].
func ResolveNetWallets(query string) ([]sharps.Wallet, string) {
	q := strings.TrimSpace(query)
	if q == "" || strings.EqualFold(q, "all") {
		return sharps.Lookup(""), ""
	}
	hits := sharps.Lookup(q)
	if len(hits) == 0 {
		return nil, fmt.Sprintf("No tracked trader matching %q. /help for the list.", q)
	}
	return hits, ""
}

// BuildNetReport sums BUY/SELL Yes−No per market in [since, now] and drops
// markets whose net exposure did not move (buy 30 Y / sell 30 Y → omitted).
func BuildNetReport(ctx context.Context, api sportsLookup, acts []polymarket.Activity, wallets []sharps.Wallet, window time.Duration, since int64, truncated bool) NetReport {
	report := NetReport{Window: window, Since: since, Truncated: truncated}
	if len(acts) == 0 {
		return report
	}

	want := make(map[string]string, len(wallets))
	for _, w := range wallets {
		want[strings.ToLower(w.Address)] = w.Name
	}

	sportsCache := make(map[string]bool)
	failed := make(map[string]struct{})
	isSports := func(slug string) bool {
		if slug == "" {
			return false
		}
		if _, ok := failed[slug]; ok {
			return false
		}
		if v, ok := sportsCache[slug]; ok {
			return v
		}
		if api == nil {
			sportsCache[slug] = false
			return false
		}
		v, err := api.EventIsSports(ctx, slug)
		if err != nil {
			failed[slug] = struct{}{}
			return false
		}
		sportsCache[slug] = v
		return v
	}

	type mkey struct{ wallet, market string }
	type acc struct {
		title    string
		slug     string
		event    string
		yes, no  float64
		other    map[string]float64
		otherOut string
	}
	grouped := make(map[mkey]*acc)
	for _, a := range acts {
		if since > 0 && a.Timestamp < since {
			continue
		}
		if isSports(a.EventSlug) {
			continue
		}
		w := strings.ToLower(strings.TrimSpace(a.ProxyWallet))
		if _, ok := want[w]; !ok {
			continue
		}
		cid := strings.TrimSpace(a.ConditionID)
		if cid == "" {
			cid = strings.TrimSpace(a.Slug)
		}
		if cid == "" {
			continue
		}
		k := mkey{wallet: w, market: cid}
		row := grouped[k]
		if row == nil {
			row = &acc{other: make(map[string]float64)}
			grouped[k] = row
		}
		if a.Title != "" {
			row.title = a.Title
		}
		if a.Slug != "" {
			row.slug = a.Slug
		}
		row.event = a.EventSlug
		signed := a.Size
		if strings.EqualFold(strings.TrimSpace(a.Side), "SELL") {
			signed = -signed
		}
		switch parseYesNo(a.Outcome) {
		case "yes":
			row.yes += signed
		case "no":
			row.no += signed
		default:
			out := strings.TrimSpace(a.Outcome)
			if out == "" {
				out = "—"
			}
			row.other[out] += signed
			if math.Abs(row.other[out]) >= math.Abs(row.other[row.otherOut]) {
				row.otherOut = out
			}
		}
	}

	byWallet := make(map[string][]MarketDelta)
	for k, row := range grouped {
		net := row.yes - row.no
		if math.Abs(net) >= netShareEps {
			md := MarketDelta{Title: row.title, Slug: row.slug}
			if net > 0 {
				md.Size = net
				md.Outcome = "YES"
			} else {
				md.Size = -net
				md.Outcome = "NO"
			}
			byWallet[k.wallet] = append(byWallet[k.wallet], md)
			continue
		}
		for out, v := range row.other {
			if math.Abs(v) < netShareEps {
				continue
			}
			md := MarketDelta{Title: row.title, Slug: row.slug, Size: math.Abs(v), Outcome: strings.ToUpper(out)}
			if v < 0 {
				md.Size = -md.Size
			}
			byWallet[k.wallet] = append(byWallet[k.wallet], md)
		}
	}

	traders := make([]TraderDelta, 0, len(wallets))
	for _, w := range wallets {
		addr := strings.ToLower(w.Address)
		mkts := byWallet[addr]
		if len(mkts) == 0 {
			continue
		}
		sort.Slice(mkts, func(i, j int) bool {
			ai, aj := math.Abs(mkts[i].Size), math.Abs(mkts[j].Size)
			if ai != aj {
				return ai > aj
			}
			return mkts[i].Title < mkts[j].Title
		})
		traders = append(traders, TraderDelta{Name: w.Name, Wallet: addr, Markets: mkts})
	}
	report.Traders = traders
	return report
}

func parseYesNo(outcome string) string {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "yes":
		return "yes"
	case "no":
		return "no"
	default:
		return ""
	}
}

// FetchNetReport loads TRADE activity for the window and builds the report.
func FetchNetReport(ctx context.Context, api interface {
	activitySinceLookup
	sportsLookup
}, wallets []sharps.Wallet, window time.Duration) (NetReport, error) {
	window = clampNetWindow(window)
	since := time.Now().Add(-window).Unix()
	users := make([]string, len(wallets))
	for i, w := range wallets {
		users[i] = w.Address
	}
	acts, truncated, err := api.FetchActivitySinceBatch(ctx, users, since, "TRADE")
	if err != nil && len(acts) == 0 {
		return NetReport{}, err
	}
	rep := BuildNetReport(ctx, api, acts, wallets, window, since, truncated)
	return rep, err
}

func clampNetWindow(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultNetWindow
	}
	if d < minNetWindow {
		return minNetWindow
	}
	if d > maxNetWindow {
		return maxNetWindow
	}
	return d
}

// FormatNetReport is one or more Telegram bodies (split under the 4096 cap).
func FormatNetReport(r NetReport, query string) []string {
	head := fmt.Sprintf("Net change · last %s", formatWindow(r.Window))
	if n := strings.TrimSpace(query); n != "" && !strings.EqualFold(n, "all") {
		head += " · " + n
	}

	if len(r.Traders) == 0 {
		body := head + "\nNo net position changes (sports excluded; flat markets omitted)."
		if r.Truncated {
			body += "\n(Feed truncated — try a shorter window.)"
		}
		return []string{body}
	}

	var blocks []string
	for _, t := range r.Traders {
		var b strings.Builder
		b.WriteString(t.Name)
		for _, m := range t.Markets {
			title := strings.TrimSpace(m.Title)
			if title == "" {
				title = m.Slug
			}
			if title == "" {
				title = "—"
			}
			sign := "+"
			size := m.Size
			if size < 0 {
				sign = "-"
				size = -size
			}
			fmt.Fprintf(&b, "\n%s%s %s  %s", sign, formatShares(size), m.Outcome, title)
		}
		blocks = append(blocks, b.String())
	}

	prefix := head
	if r.Truncated {
		prefix += "\n(Feed truncated — some fills may be missing.)"
	}
	return chunkTelegram(prefix, blocks)
}

func chunkTelegram(prefix string, blocks []string) []string {
	var chunks []string
	cur := prefix
	for i, block := range blocks {
		sep := "\n\n"
		next := cur + sep + block
		if i > 0 && len(next) > telegramChunk {
			chunks = append(chunks, cur)
			cur = prefix + " (cont.)" + sep + block
			continue
		}
		cur = next
	}
	if cur != "" {
		chunks = append(chunks, cur)
	}
	return chunks
}

func formatWindow(d time.Duration) string {
	d = d.Round(time.Minute)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	switch {
	case h > 0 && m == 0 && h%24 == 0:
		days := h / 24
		if days == 1 {
			return "1d"
		}
		return fmt.Sprintf("%dd", days)
	case h > 0 && m == 0:
		return fmt.Sprintf("%dh", h)
	case h > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}
