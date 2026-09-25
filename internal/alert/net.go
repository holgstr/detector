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
	Title    string
	Slug     string
	Size     float64
	Outcome  string
	AvgPrice float64 // effective acquisition of the remaining net; 0 if unknown
	HasAvg   bool
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

// ResolveNetWallets picks wallets for /net [trader].
// A unique prefix/substring of a tracked name is enough (Flip → Flipadelphia).
// An exact Polymarket name or wallet id is used even when that trader is not tracked.
// Several matches at the same rank are listed instead of guessed.
func ResolveNetWallets(ctx context.Context, api userLookup, query string) ([]sharps.Wallet, string) {
	q := strings.TrimSpace(query)
	if q == "" || strings.EqualFold(q, "all") {
		return sharps.Lookup(""), ""
	}
	hits := sharps.Lookup(q)
	if len(hits) > 1 {
		return nil, fmt.Sprintf("Several traders match %q: %s", q, joinWalletNames(hits))
	}
	if len(hits) == 1 {
		return hits, ""
	}
	if addr, ok := polymarket.ParseWalletAddress(q); ok {
		if tracked := sharps.Lookup(addr); len(tracked) == 1 {
			return tracked, ""
		}
		return []sharps.Wallet{walletFromAddress(ctx, api, addr)}, ""
	}
	w, errMsg := resolveExactUntracked(ctx, api, q)
	if errMsg != "" {
		return nil, errMsg
	}
	return []sharps.Wallet{w}, ""
}

// resolveExactUntracked accepts only a full Polymarket username, not a prefix.
func resolveExactUntracked(ctx context.Context, api userLookup, query string) (sharps.Wallet, string) {
	miss := fmt.Sprintf("No tracked trader matching %q.", query)
	if api == nil {
		return sharps.Wallet{}, miss
	}
	hits, err := api.SearchUsers(ctx, query)
	if err != nil {
		return sharps.Wallet{}, fmt.Sprintf("Couldn't look up %q: %v", query, err)
	}
	exact := exactUserMatches(query, hits)
	if len(exact) == 0 {
		return sharps.Wallet{}, miss
	}
	if len(exact) > 1 {
		names := make([]string, len(exact))
		for i, p := range exact {
			n := strings.TrimSpace(p.Name)
			if n == "" {
				n = shortWallet(p.Address)
			}
			names[i] = n
		}
		return sharps.Wallet{}, fmt.Sprintf("Several users match %q: %s — use a wallet id.", query, strings.Join(names, ", "))
	}
	name := strings.TrimSpace(exact[0].Name)
	if name == "" {
		name = query
	}
	return sharps.Wallet{Address: exact[0].Address, Name: name}, ""
}

func exactUserMatches(query string, hits []polymarket.UserProfile) []polymarket.UserProfile {
	q := strings.ToLower(strings.TrimSpace(query))
	q = strings.Trim(q, "\"'`“”„")
	if q == "" {
		return nil
	}
	var exact []polymarket.UserProfile
	seen := make(map[string]struct{})
	for _, u := range hits {
		addr := strings.ToLower(strings.TrimSpace(u.Address))
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(u.Name))
		if name != q && addr != q {
			continue
		}
		seen[addr] = struct{}{}
		exact = append(exact, polymarket.UserProfile{Address: addr, Name: strings.TrimSpace(u.Name)})
	}
	return exact
}

func walletFromAddress(ctx context.Context, api userLookup, addr string) sharps.Wallet {
	if api == nil {
		return sharps.Wallet{Address: addr, Name: shortWallet(addr)}
	}
	p, err := api.FetchProfile(ctx, addr)
	if err != nil {
		return sharps.Wallet{Address: addr, Name: shortWallet(addr)}
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = shortWallet(addr)
	}
	if p.Address != "" {
		addr = strings.ToLower(strings.TrimSpace(p.Address))
	}
	return sharps.Wallet{Address: addr, Name: name}
}

// BuildNetReport sums BUY/SELL Yes−No per market in [since, now] and drops
// markets whose net exposure did not move (buy 30 Y / sell 30 Y → omitted).
// AvgPrice is the merge-adjusted cost of the leftover net (No @ p ≡ selling Yes @ 1−p).
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
	isSports := func(eventSlug, marketSlug string) bool {
		if polymarket.LooksLikeSportsSlug(eventSlug, marketSlug) {
			return true
		}
		slug := eventSlug
		if slug == "" {
			return false
		}
		if _, ok := failed[slug]; ok {
			return true // Gamma down: hide rather than leak sports
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
			return true
		}
		sportsCache[slug] = v
		return v
	}

	type mkey struct{ wallet, market string }
	type acc struct {
		title           string
		slug            string
		event           string
		yes, no         float64
		yesCash, noCash float64
		other           map[string]float64
		otherCash       map[string]float64
		otherOut        string
		hasPrice        bool
	}
	grouped := make(map[mkey]*acc)
	for _, a := range acts {
		if since > 0 && a.Timestamp < since {
			continue
		}
		if isSports(a.EventSlug, a.Slug) {
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
			row = &acc{other: make(map[string]float64), otherCash: make(map[string]float64)}
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
		px := fillPrice(a)
		if px != 0 || a.USDCSize != 0 {
			row.hasPrice = true
		}
		cash := signed * px
		switch parseYesNo(a.Outcome) {
		case "yes":
			row.yes += signed
			row.yesCash += cash
		case "no":
			row.no += signed
			row.noCash += cash
		default:
			out := strings.TrimSpace(a.Outcome)
			if out == "" {
				out = "—"
			}
			row.other[out] += signed
			row.otherCash[out] += cash
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
			if avg, ok := effectiveNetAvg(row.yes, row.no, row.yesCash, row.noCash); ok && row.hasPrice {
				md.AvgPrice = avg
				md.HasAvg = true
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
			if row.hasPrice && v != 0 {
				md.AvgPrice = row.otherCash[out] / v
				md.HasAvg = true
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

func fillPrice(a polymarket.Activity) float64 {
	if a.Price != 0 {
		return a.Price
	}
	if a.Size != 0 && a.USDCSize != 0 {
		return a.USDCSize / a.Size
	}
	return 0
}

// effectiveNetAvg is the merge-adjusted cost of leftover Yes or No.
// Buying No at p is treated as selling Yes at 1−p (a complete set is $1).
func effectiveNetAvg(yes, no, yesCash, noCash float64) (float64, bool) {
	net := yes - no
	if math.Abs(net) < netShareEps {
		return 0, false
	}
	cash := yesCash + noCash
	var avg float64
	if net > 0 {
		avg = (cash - no) / net
	} else {
		avg = (cash - yes) / -net
	}
	if math.IsNaN(avg) || math.IsInf(avg, 0) {
		return 0, false
	}
	return avg, true
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
func FormatNetReport(r NetReport, _ string) []string {
	if len(r.Traders) == 0 {
		body := "No net position changes."
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
			if m.HasAvg {
				fmt.Fprintf(&b, "\n%s%s %s  %s @ %s", sign, formatShares(size), m.Outcome, title, formatCents(m.AvgPrice))
			} else {
				fmt.Fprintf(&b, "\n%s%s %s  %s", sign, formatShares(size), m.Outcome, title)
			}
		}
		blocks = append(blocks, b.String())
	}

	prefix := ""
	if r.Truncated {
		prefix = "(Feed truncated — some fills may be missing.)"
	}
	return chunkTelegram(prefix, blocks)
}

func chunkTelegram(prefix string, blocks []string) []string {
	var chunks []string
	cur := prefix
	for i, block := range blocks {
		sep := ""
		if cur != "" {
			sep = "\n\n"
		}
		next := cur + sep + block
		if i > 0 && len(next) > telegramChunk {
			chunks = append(chunks, cur)
			cur = prefix
			if cur != "" {
				cur += " (cont.)"
			}
			if cur != "" {
				cur += "\n\n"
			}
			cur += block
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
