package alert

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const (
	portPageSize  = 500
	portMaxOffset = 5000
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
type PortReport struct {
	Name      string
	Wallet    string
	Holdings  []PortHolding
	Truncated bool
}

// PortUsage is the reply when /port has no trader (or "all").
const PortUsage = "Usage: /port <trader> — open non-sports nets, shares sorted by market value."

// ResolvePortWallet requires exactly one tracked trader.
func ResolvePortWallet(query string) (sharps.Wallet, string) {
	q := strings.TrimSpace(query)
	if q == "" || strings.EqualFold(q, "all") {
		return sharps.Wallet{}, PortUsage
	}
	hits, errMsg := ResolveNetWallets(q)
	if errMsg != "" {
		return sharps.Wallet{}, errMsg
	}
	if len(hits) != 1 {
		return sharps.Wallet{}, PortUsage
	}
	return hits[0], ""
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
	var keep []PortHolding
	for _, h := range holdings {
		if isSports(h.EventSlug, h.Slug) {
			continue
		}
		keep = append(keep, h)
	}
	return keep
}

// BuildPortReport nets, drops sports/flats, and sorts leftover size by market value.
func BuildPortReport(ctx context.Context, api sportsLookup, w sharps.Wallet, positions []polymarket.Position, truncated bool) PortReport {
	holdings := dropSportsHoldings(ctx, api, NetPortHoldings(positions))
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

func fetchAllPositions(ctx context.Context, api positionLookup, user string) ([]polymarket.Position, bool, error) {
	var all []polymarket.Position
	truncated := false
	for offset := 0; offset < portMaxOffset; offset += portPageSize {
		page, err := api.FetchPositions(ctx, polymarket.FetchPositionsOptions{
			User:   user,
			Limit:  portPageSize,
			Offset: offset,
		})
		if err != nil {
			return all, truncated, err
		}
		all = append(all, page...)
		if len(page) < portPageSize {
			return all, truncated, nil
		}
	}
	return all, true, nil
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
	head := "Portfolio · " + name
	if r.Truncated {
		head += "\n(Position book truncated — some small holdings may be missing.)"
	}
	if len(r.Holdings) == 0 {
		return []string{head + "\nNo open non-sports holdings."}
	}

	var blocks []string
	for _, h := range r.Holdings {
		title := strings.TrimSpace(h.Title)
		if title == "" {
			title = h.Slug
		}
		if title == "" {
			title = "—"
		}
		line := fmt.Sprintf("%s %s  %s", formatShares(h.Size), h.Outcome, title)
		if h.HasCur {
			line += " | " + formatCents(h.CurPrice)
			if h.HasAvg {
				line += " " + formatCentsDelta(h.CurPrice-h.AvgPrice)
			}
		}
		blocks = append(blocks, line)
	}
	return chunkTelegram(head, blocks)
}

func formatCentsDelta(d float64) string {
	c := math.Round(d * 100)
	if c > 0 {
		return fmt.Sprintf("(+%.0fc)", c)
	}
	if c < 0 {
		return fmt.Sprintf("(%.0fc)", c)
	}
	return "(0c)"
}
