package alert

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const maxLastTrades = 80

// ResolveLastTradesWallets picks wallets for /lasttrades [trader] [market].
// A unique tracked or Polymarket name is that trader. If the first token is
// not a person, it is treated as the start of the market filter instead.
func ResolveLastTradesWallets(ctx context.Context, api userLookup, trader, market string) (wallets []sharps.Wallet, marketQuery, label, errMsg string) {
	trader = strings.TrimSpace(trader)
	market = strings.TrimSpace(market)
	if trader == "" {
		return sharps.Lookup(""), market, "", ""
	}
	w, errMsg := ResolveAnyTrader(ctx, api, trader)
	if errMsg == "" {
		name := strings.TrimSpace(w.Name)
		if name == "" {
			name = trader
		}
		return []sharps.Wallet{w}, market, name, ""
	}
	if strings.HasPrefix(errMsg, "Couldn't look up") {
		return nil, "", "", errMsg
	}
	if len(sharps.Lookup(trader)) > 1 {
		return nil, "", "", errMsg
	}
	if market == "" && strings.HasPrefix(errMsg, "Several") {
		return nil, "", "", errMsg
	}
	combined := trader
	if market != "" {
		combined += " " + market
	}
	return sharps.Lookup(""), combined, "", ""
}

// LastTrade is one fill in a /lasttrades report.
type LastTrade struct {
	Name      string
	Wallet    string
	Side      string
	Outcome   string
	Title     string
	Slug      string
	Size      float64
	Price     float64
	Timestamp int64
}

// LastTradesReport is /lasttrades output: recent fills for tracked wallets.
type LastTradesReport struct {
	Window       time.Duration
	Since        int64
	Now          time.Time
	TraderQuery  string
	MarketQuery  string
	MarketTitle  string
	Truncated    bool
	Capped       bool
	DroppedSport bool
	Trades       []LastTrade
}

// BuildLastTradesReport keeps TRADE fills in the window for the given wallets.
// Sports are omitted unless a market filter is set. Newest fills come first.
func BuildLastTradesReport(ctx context.Context, api sportsLookup, acts []polymarket.Activity, wallets []sharps.Wallet, window time.Duration, since int64, truncated bool, marketCID, marketSlug, marketQuery, marketTitle, traderQuery string) LastTradesReport {
	now := time.Now()
	report := LastTradesReport{
		Window:      window,
		Since:       since,
		Now:         now,
		TraderQuery: traderQuery,
		MarketQuery: marketQuery,
		MarketTitle: marketTitle,
		Truncated:   truncated,
	}
	if len(acts) == 0 {
		return report
	}

	want := make(map[string]string, len(wallets))
	for _, w := range wallets {
		want[strings.ToLower(w.Address)] = w.Name
	}

	filterMarket := strings.TrimSpace(marketCID) != "" || strings.TrimSpace(marketSlug) != "" || strings.TrimSpace(marketQuery) != ""
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
			return true
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

	out := make([]LastTrade, 0, len(acts))
	for _, a := range acts {
		if since > 0 && a.Timestamp < since {
			continue
		}
		w := strings.ToLower(strings.TrimSpace(a.ProxyWallet))
		name, ok := want[w]
		if !ok {
			continue
		}
		if !activityMatchesMarket(a, marketCID, marketSlug, marketQuery) {
			continue
		}
		if !filterMarket && isSports(a.EventSlug, a.Slug) {
			report.DroppedSport = true
			continue
		}
		side := strings.ToUpper(strings.TrimSpace(a.Side))
		if side == "" {
			side = "TRADE"
		}
		outcome := strings.TrimSpace(a.Outcome)
		if outcome == "" {
			outcome = "—"
		} else {
			outcome = strings.ToUpper(outcome)
		}
		title := strings.TrimSpace(a.Title)
		if title == "" {
			title = a.Slug
		}
		if title == "" {
			title = "—"
		}
		out = append(out, LastTrade{
			Name:      name,
			Wallet:    w,
			Side:      side,
			Outcome:   outcome,
			Title:     title,
			Slug:      a.Slug,
			Size:      a.Size,
			Price:     fillPrice(a),
			Timestamp: a.Timestamp,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Timestamp != out[j].Timestamp {
			return out[i].Timestamp > out[j].Timestamp
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Title < out[j].Title
	})

	if len(out) > maxLastTrades {
		report.Capped = true
		out = out[:maxLastTrades]
	}
	report.Trades = out
	return report
}

func activityMatchesMarket(a polymarket.Activity, cid, slug, query string) bool {
	cid = strings.TrimSpace(cid)
	slug = strings.ToLower(strings.TrimSpace(slug))
	query = strings.ToLower(strings.TrimSpace(query))
	if cid == "" && slug == "" && query == "" {
		return true
	}
	if cid != "" || slug != "" {
		if cid != "" && strings.EqualFold(strings.TrimSpace(a.ConditionID), cid) {
			return true
		}
		if slug != "" && (strings.EqualFold(strings.TrimSpace(a.Slug), slug) || strings.EqualFold(strings.TrimSpace(a.EventSlug), slug)) {
			return true
		}
		return false
	}
	hay := strings.ToLower(strings.TrimSpace(a.Title) + " " + strings.TrimSpace(a.Slug) + " " + strings.TrimSpace(a.EventSlug) + " " + strings.TrimSpace(a.ConditionID))
	return strings.Contains(hay, query)
}

// FetchLastTradesReport loads TRADE activity for the window and builds the list.
func FetchLastTradesReport(ctx context.Context, api interface {
	activitySinceLookup
	sportsLookup
	posMarketAPI
	positionLookup
}, wallets []sharps.Wallet, window time.Duration, marketQuery, traderQuery string) (LastTradesReport, error) {
	window = clampNetWindow(window)
	since := time.Now().Add(-window).Unix()

	var marketCID, marketSlug, marketTitle string
	if q := strings.TrimSpace(marketQuery); q != "" {
		if hit, err := resolvePosMarket(ctx, api, q, wallets); err == nil {
			marketCID = strings.TrimSpace(hit.Market.ConditionID)
			marketSlug = strings.TrimSpace(hit.Market.Slug)
			marketTitle = strings.TrimSpace(hit.Market.Question)
			if marketTitle == "" {
				marketTitle = strings.TrimSpace(hit.GroupItemTitle)
			}
			if marketTitle == "" {
				marketTitle = marketSlug
			}
		}
	}

	users := make([]string, len(wallets))
	for i, w := range wallets {
		users[i] = w.Address
	}
	acts, truncated, err := api.FetchActivitySinceBatch(ctx, users, since, "TRADE")
	if err != nil && len(acts) == 0 {
		return LastTradesReport{Window: window, Since: since, Now: time.Now(), TraderQuery: traderQuery, MarketQuery: marketQuery, MarketTitle: marketTitle}, err
	}
	rep := BuildLastTradesReport(ctx, api, acts, wallets, window, since, truncated, marketCID, marketSlug, marketQuery, marketTitle, traderQuery)
	return rep, err
}

// FormatLastTradesReport is one or more Telegram bodies (split under the 4096 cap).
func FormatLastTradesReport(r LastTradesReport) []string {
	var headParts []string
	if n := strings.TrimSpace(r.TraderQuery); n != "" && !strings.EqualFold(n, "all") {
		headParts = append(headParts, n)
	}
	if t := strings.TrimSpace(r.MarketTitle); t != "" {
		headParts = append(headParts, t)
	} else if m := strings.TrimSpace(r.MarketQuery); m != "" {
		headParts = append(headParts, m)
	}
	head := strings.Join(headParts, " · ")

	if len(r.Trades) == 0 {
		body := "No fills"
		if head != "" {
			body = head + "\n" + body
		}
		if r.DroppedSport && strings.TrimSpace(r.MarketQuery) == "" {
			body += " (sports excluded)"
		}
		body += "."
		if r.Truncated {
			body += "\n(Feed truncated — try a shorter window or a market filter.)"
		}
		return []string{body}
	}

	omitTitle := strings.TrimSpace(r.MarketTitle) != "" || strings.TrimSpace(r.MarketQuery) != ""
	singleTrader := true
	if len(r.Trades) > 0 {
		for _, t := range r.Trades[1:] {
			if t.Name != r.Trades[0].Name {
				singleTrader = false
				break
			}
		}
	}

	var blocks []string
	for _, t := range r.Trades {
		ago := formatAgo(t.Timestamp, r.Now)
		line := fmt.Sprintf("%s %s %s @ %s", t.Side, t.Outcome, formatShares(t.Size), formatCents(t.Price))
		if !singleTrader {
			line = t.Name + " " + line
		}
		if ago != "" {
			line += " · " + ago
		}
		if !omitTitle {
			line += "\n" + t.Title
		}
		blocks = append(blocks, line)
	}
	var notes []string
	if r.Truncated {
		notes = append(notes, "(Feed truncated — some fills may be missing.)")
	}
	if r.Capped {
		notes = append(notes, fmt.Sprintf("(Showing the %d most recent.)", maxLastTrades))
	}
	if len(notes) > 0 {
		note := strings.Join(notes, "\n")
		if head != "" {
			head += "\n" + note
		} else {
			head = note
		}
	}
	return chunkTelegram(head, blocks)
}

func formatAgo(ts int64, now time.Time) string {
	if ts <= 0 || now.IsZero() {
		return ""
	}
	d := now.Sub(time.Unix(ts, 0))
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Minute)
	if d < time.Minute {
		return "just now"
	}
	return formatWindow(d) + " ago"
}
