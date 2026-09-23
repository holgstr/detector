package alert

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const (
	portPageSize  = 500
	portMaxOffset = 5000
	portMinUSD    = 100
)

// PortHolding is one market's leftover net for /port.
type PortHolding struct {
	Title       string
	Slug        string
	EventSlug   string
	Size        float64
	Outcome     string
	CurPrice    float64
	AvgPrice    float64
	HasCur      bool
	HasAvg      bool
	MarketValue float64
}

// PortReport is /port output: one trader's open non-sports nets.
// Limit, when positive, keeps only that many holdings after the market-value sort.
type PortReport struct {
	Name      string
	Wallet    string
	Holdings  []PortHolding
	Truncated bool
	Limit     int
}

// PortUsage is the reply when /port has no trader (or "all").
const PortUsage = "Usage: /port [N] <trader> — open non-sports nets of $100+, shares sorted by market value. N keeps the top N."

// ResolvePortWallet requires exactly one trader (tracked or a Polymarket name/wallet).
func ResolvePortWallet(ctx context.Context, api userLookup, query string) (sharps.Wallet, string) {
	q := strings.TrimSpace(query)
	if q == "" || strings.EqualFold(q, "all") {
		return sharps.Wallet{}, PortUsage
	}
	return ResolveAnyTrader(ctx, api, q)
}

// NetPortHoldings collapses Yes/No legs per market and drops flats/dust.
func NetPortHoldings(positions []polymarket.Position) []PortHolding {
	type otherLeg struct {
		size, cash, cur    float64
		hasPx, hasCur      bool
		title, slug, event string
	}
	type acc struct {
		title, slug, event  string
		yes, no             float64
		yesCash, noCash     float64
		yesCur, noCur       float64
		hasYesPx, hasNoPx   bool
		hasYesCur, hasNoCur bool
		other               map[string]*otherLeg
	}
	by := make(map[string]*acc)
	for _, p := range positions {
		cid := strings.ToLower(strings.TrimSpace(p.ConditionID))
		if cid == "" {
			cid = strings.ToLower(strings.TrimSpace(p.Slug))
		}
		if cid == "" {
			continue
		}
		row := by[cid]
		if row == nil {
			row = &acc{other: make(map[string]*otherLeg)}
			by[cid] = row
		}
		if t := strings.TrimSpace(p.Title); t != "" {
			row.title = t
		}
		if s := strings.TrimSpace(p.Slug); s != "" {
			row.slug = s
		}
		if e := strings.TrimSpace(p.EventSlug); e != "" {
			row.event = e
		}
		switch parseYesNo(p.Outcome) {
		case "yes":
			row.yes += p.Size
			if p.AvgPrice != 0 {
				row.yesCash += p.Size * p.AvgPrice
				row.hasYesPx = true
			}
			if p.CurPrice != 0 {
				row.yesCur = p.CurPrice
				row.hasYesCur = true
			}
		case "no":
			row.no += p.Size
			if p.AvgPrice != 0 {
				row.noCash += p.Size * p.AvgPrice
				row.hasNoPx = true
			}
			if p.CurPrice != 0 {
				row.noCur = p.CurPrice
				row.hasNoCur = true
			}
		default:
			out := strings.TrimSpace(p.Outcome)
			if out == "" {
				out = "—"
			}
			leg := row.other[out]
			if leg == nil {
				leg = &otherLeg{}
				row.other[out] = leg
			}
			leg.size += p.Size
			if t := strings.TrimSpace(p.Title); t != "" {
				leg.title = t
			}
			if s := strings.TrimSpace(p.Slug); s != "" {
				leg.slug = s
			}
			if e := strings.TrimSpace(p.EventSlug); e != "" {
				leg.event = e
			}
			if p.AvgPrice != 0 {
				leg.cash += p.Size * p.AvgPrice
				leg.hasPx = true
			}
			if p.CurPrice != 0 {
				leg.cur = p.CurPrice
				leg.hasCur = true
			}
		}
	}

	var out []PortHolding
	for _, row := range by {
		net := row.yes - row.no
		if math.Abs(net) >= posShareEps {
			h := PortHolding{Title: row.title, Slug: row.slug, EventSlug: row.event}
			if net > 0 {
				h.Size = net
				h.Outcome = "YES"
				if row.hasYesCur {
					h.CurPrice = row.yesCur
					h.HasCur = true
				}
			} else {
				h.Size = -net
				h.Outcome = "NO"
				if row.hasNoCur {
					h.CurPrice = row.noCur
					h.HasCur = true
				}
			}
			if avg, ok := effectiveNetAvg(row.yes, row.no, row.yesCash, row.noCash); ok && (row.hasYesPx || row.hasNoPx) {
				h.AvgPrice = avg
				h.HasAvg = true
			}
			if h.HasCur {
				h.MarketValue = h.Size * h.CurPrice
			}
			out = append(out, h)
			continue
		}
		for name, leg := range row.other {
			if math.Abs(leg.size) < posShareEps {
				continue
			}
			h := PortHolding{
				Title:     firstNonEmpty(leg.title, row.title),
				Slug:      firstNonEmpty(leg.slug, row.slug),
				EventSlug: firstNonEmpty(leg.event, row.event),
				Size:      math.Abs(leg.size),
				Outcome:   strings.ToUpper(name),
			}
			if leg.hasCur {
				h.CurPrice = leg.cur
				h.HasCur = true
				h.MarketValue = h.Size * h.CurPrice
			}
			if leg.hasPx && leg.size != 0 {
				h.AvgPrice = leg.cash / leg.size
				h.HasAvg = true
			}
			out = append(out, h)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func dropSportsHoldings(ctx context.Context, api sportsLookup, holdings []PortHolding) []PortHolding {
	sportsCache := make(map[string]bool)
	failed := make(map[string]struct{})

	need := make([]string, 0)
	seen := make(map[string]struct{})
	for _, h := range holdings {
		if polymarket.LooksLikeSportsSlug(h.EventSlug, h.Slug) {
			continue
		}
		slug := strings.TrimSpace(h.EventSlug)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		need = append(need, slug)
	}
	if api != nil && len(need) > 0 {
		var mu sync.Mutex
		workers := 8
		if workers > len(need) {
			workers = len(need)
		}
		jobs := make(chan string)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for slug := range jobs {
					v, err := api.EventIsSports(ctx, slug)
					mu.Lock()
					if err != nil {
						failed[slug] = struct{}{}
					} else {
						sportsCache[slug] = v
					}
					mu.Unlock()
				}
			}()
		}
		for _, slug := range need {
			jobs <- slug
		}
		close(jobs)
		wg.Wait()
	}

	var keep []PortHolding
	for _, h := range holdings {
		if polymarket.LooksLikeSportsSlug(h.EventSlug, h.Slug) {
			continue
		}
		slug := strings.TrimSpace(h.EventSlug)
		if slug == "" {
			keep = append(keep, h)
			continue
		}
		if _, ok := failed[slug]; ok {
			continue
		}
		if sportsCache[slug] {
			continue
		}
		keep = append(keep, h)
	}
	return keep
}

func dropSmallHoldings(holdings []PortHolding) []PortHolding {
	var keep []PortHolding
	for _, h := range holdings {
		if h.MarketValue < portMinUSD {
			continue
		}
		keep = append(keep, h)
	}
	return keep
}

// BuildPortReport nets, drops sports/flats/sub-$100 value, and sorts leftover size by market value.
func BuildPortReport(ctx context.Context, api sportsLookup, w sharps.Wallet, positions []polymarket.Position, truncated bool) PortReport {
	holdings := dropSmallHoldings(dropSportsHoldings(ctx, api, NetPortHoldings(positions)))
	sort.SliceStable(holdings, func(i, j int) bool {
		if holdings[i].MarketValue != holdings[j].MarketValue {
			return holdings[i].MarketValue > holdings[j].MarketValue
		}
		if holdings[i].Size != holdings[j].Size {
			return holdings[i].Size > holdings[j].Size
		}
		return holdings[i].Title < holdings[j].Title
	})
	return PortReport{
		Name:      w.Name,
		Wallet:    strings.ToLower(w.Address),
		Holdings:  holdings,
		Truncated: truncated,
	}
}

func portPositionOpts(user string, offset int) polymarket.FetchPositionsOptions {
	return polymarket.FetchPositionsOptions{
		User:          user,
		Limit:         portPageSize,
		Offset:        offset,
		SortBy:        "CURRENT",
		SortDirection: "DESC",
	}
}

func fetchAllPositions(ctx context.Context, api positionLookup, user string) ([]polymarket.Position, bool, error) {
	first, err := api.FetchPositions(ctx, portPositionOpts(user, 0))
	if err != nil {
		return first, false, err
	}
	if len(first) < portPageSize {
		return first, false, nil
	}

	type slot struct {
		offset int
		page   []polymarket.Position
		err    error
	}
	n := (portMaxOffset / portPageSize) - 1
	ch := make(chan slot, n)
	var wg sync.WaitGroup
	for offset := portPageSize; offset < portMaxOffset; offset += portPageSize {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			page, err := api.FetchPositions(ctx, portPositionOpts(user, offset))
			ch <- slot{offset: offset, page: page, err: err}
		}(offset)
	}
	go func() {
		wg.Wait()
		close(ch)
	}()

	byOff := make(map[int][]polymarket.Position, n)
	var firstErr error
	for s := range ch {
		if s.err != nil {
			if firstErr == nil {
				firstErr = s.err
			}
			continue
		}
		byOff[s.offset] = s.page
	}

	all := append([]polymarket.Position{}, first...)
	for offset := portPageSize; offset < portMaxOffset; offset += portPageSize {
		page, ok := byOff[offset]
		if !ok {
			return all, false, firstErr
		}
		all = append(all, page...)
		if len(page) < portPageSize {
			return all, false, firstErr
		}
	}
	return all, true, firstErr
}

// FetchPortReport loads one wallet's open positions and builds /port.
func FetchPortReport(ctx context.Context, api interface {
	positionLookup
	sportsLookup
}, w sharps.Wallet) (PortReport, error) {
	pos, truncated, err := fetchAllPositions(ctx, api, w.Address)
	if err != nil && len(pos) == 0 {
		return PortReport{Name: w.Name, Wallet: strings.ToLower(w.Address)}, err
	}
	rep := BuildPortReport(ctx, api, w, pos, truncated)
	return rep, err
}

// FormatPortReport is one or more Telegram bodies (split under the 4096 cap).
func FormatPortReport(r PortReport) []string {
	name := strings.TrimSpace(r.Name)
	if name == "" {
		name = r.Wallet
	}
	head := name
	if r.Truncated {
		head += "\n(Position book truncated — some small holdings may be missing.)"
	}
	holdings := r.Holdings
	if r.Limit > 0 && len(holdings) > r.Limit {
		head += fmt.Sprintf("\n(top %d of %d by market value)", r.Limit, len(holdings))
		holdings = holdings[:r.Limit]
	}
	if len(holdings) == 0 {
		return []string{head + "\nNo open non-sports holdings of $100+."}
	}

	var blocks []string
	for _, h := range holdings {
		title := strings.TrimSpace(h.Title)
		if title == "" {
			title = h.Slug
		}
		if title == "" {
			title = "—"
		}
		line := fmt.Sprintf("%s %s  %s%s", formatShares(h.Size), h.Outcome, title, formatAcqCur(h.HasAvg, h.HasCur, h.AvgPrice, h.CurPrice))
		blocks = append(blocks, line)
	}
	return chunkTelegram(head, blocks)
}

func formatAcqCur(hasAvg, hasCur bool, avg, cur float64) string {
	switch {
	case hasAvg && hasCur:
		return " | " + formatCents(avg) + " → " + formatCents(cur)
	case hasAvg:
		return " | " + formatCents(avg)
	case hasCur:
		return " | " + formatCents(cur)
	default:
		return ""
	}
}

func formatAtPrice(ok bool, p float64) string {
	if !ok {
		return ""
	}
	return " @ " + formatCents(p)
}

// primaryNetHolding nets one market's legs (already filtered) and keeps
// acquisition / live prices the same way /port does.
func primaryNetHolding(positions []polymarket.Position) (PortHolding, bool) {
	cloned := append([]polymarket.Position(nil), positions...)
	for i := range cloned {
		if strings.TrimSpace(cloned[i].ConditionID) == "" && strings.TrimSpace(cloned[i].Slug) == "" {
			cloned[i].ConditionID = "_"
		}
	}
	nets := NetPortHoldings(cloned)
	if len(nets) == 0 {
		return PortHolding{}, false
	}
	best := nets[0]
	for _, h := range nets[1:] {
		if h.Size > best.Size {
			best = h
		}
	}
	return best, true
}
