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
	// FullPortDefaultLimit is how many lines /fullport shows when no count is given.
	FullPortDefaultLimit = 20
	// Slack past the rows still needed, so a few sports lines in the window
	// don't force another sports-lookup round.
	fullPortSportsSlack = 16
	fullPortSportsBatch = 48
)

// FullPortReport is /fullport: every tracked wallet's open book netted together.
type FullPortReport struct {
	Holdings  []PortHolding
	Truncated bool
	Failed    int
	// Limit is the requested top-N. Zero means the whole book.
	Limit int
	// More means at least one qualifying line was left off because of Limit.
	More bool
}

// BuildFullPortReport nets every wallet into one book, then drops sports and
// sub-$100 lines the same way /port does. limit keeps the top lines by market
// value; zero keeps them all. Sports lookups run only for lines that can still
// make that list. Wallets on a line is how many of those wallets are themselves
// net that side (the other side is in the share count, and is not named).
func BuildFullPortReport(ctx context.Context, api sportsLookup, byWallet map[string][]polymarket.Position, truncated bool, failed, limit int) FullPortReport {
	holdings := aggregateFullPort(byWallet)
	sort.SliceStable(holdings, func(i, j int) bool {
		if holdings[i].MarketValue != holdings[j].MarketValue {
			return holdings[i].MarketValue > holdings[j].MarketValue
		}
		if holdings[i].Size != holdings[j].Size {
			return holdings[i].Size > holdings[j].Size
		}
		return holdings[i].Title < holdings[j].Title
	})
	kept, more := limitFullPort(ctx, api, dropSmallHoldings(holdings), limit)
	return FullPortReport{Holdings: kept, Truncated: truncated, Failed: failed, Limit: limit, More: more}
}

// limitFullPort drops sports, then keeps the first limit lines. Sub-$100 rows
// are already gone, so a short top-N never asks Gamma about the rest of the book.
func limitFullPort(ctx context.Context, api sportsLookup, holdings []PortHolding, limit int) ([]PortHolding, bool) {
	if limit <= 0 {
		return dropSportsHoldings(ctx, api, holdings), false
	}
	var kept []PortHolding
	i := 0
	for len(kept) < limit && i < len(holdings) {
		need := limit - len(kept)
		batch := need + fullPortSportsSlack
		if batch > fullPortSportsBatch {
			batch = fullPortSportsBatch
		}
		end := i + batch
		if end > len(holdings) {
			end = len(holdings)
		}
		filtered := dropSportsHoldings(ctx, api, holdings[i:end])
		if len(filtered) > need {
			return append(kept, filtered[:need]...), true
		}
		kept = append(kept, filtered...)
		i = end
	}
	if len(kept) < limit {
		return kept, false
	}
	for _, h := range holdings[i:] {
		slug := strings.TrimSpace(h.EventSlug)
		if slug == "" || !polymarket.LooksLikeSportsSlug(h.EventSlug, h.Slug) {
			return kept, true
		}
	}
	return kept, false
}

type fullLeg struct {
	size, cash, cur float64
	hasPx, hasCur   bool
	title, slug     string
	event           string
}

type fullBook struct {
	title, slug, event  string
	yes, no             float64
	yesCash, noCash     float64
	yesCur, noCur       float64
	hasYesPx, hasNoPx   bool
	hasYesCur, hasNoCur bool
	other               map[string]*fullLeg
}

type fullMarket struct {
	title, slug, event string
	books              map[string]*fullBook
}

func aggregateFullPort(byWallet map[string][]polymarket.Position) []PortHolding {
	markets := make(map[string]*fullMarket)
	for addr, positions := range byWallet {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if addr == "" {
			continue
		}
		for _, p := range positions {
			cid := strings.ToLower(strings.TrimSpace(p.ConditionID))
			if cid == "" {
				cid = strings.ToLower(strings.TrimSpace(p.Slug))
			}
			if cid == "" {
				continue
			}
			m := markets[cid]
			if m == nil {
				m = &fullMarket{books: make(map[string]*fullBook)}
				markets[cid] = m
			}
			b := m.books[addr]
			if b == nil {
				b = &fullBook{other: make(map[string]*fullLeg)}
				m.books[addr] = b
			}
			if t := strings.TrimSpace(p.Title); t != "" {
				m.title = t
				b.title = t
			}
			if s := strings.TrimSpace(p.Slug); s != "" {
				m.slug = s
				b.slug = s
			}
			if e := strings.TrimSpace(p.EventSlug); e != "" {
				m.event = e
				b.event = e
			}
			switch parseYesNo(p.Outcome) {
			case "yes":
				b.yes += p.Size
				if p.AvgPrice != 0 {
					b.yesCash += p.Size * p.AvgPrice
					b.hasYesPx = true
				}
				if p.CurPrice != 0 {
					b.yesCur = p.CurPrice
					b.hasYesCur = true
				}
			case "no":
				b.no += p.Size
				if p.AvgPrice != 0 {
					b.noCash += p.Size * p.AvgPrice
					b.hasNoPx = true
				}
				if p.CurPrice != 0 {
					b.noCur = p.CurPrice
					b.hasNoCur = true
				}
			default:
				out := strings.TrimSpace(p.Outcome)
				if out == "" {
					out = "—"
				}
				leg := b.other[out]
				if leg == nil {
					leg = &fullLeg{}
					b.other[out] = leg
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
	}

	var out []PortHolding
	for _, m := range markets {
		var yes, no, yesCash, noCash float64
		var hasYesPx, hasNoPx bool
		var yesCur, noCur float64
		var hasYesCur, hasNoCur bool
		var yesCurRank, noCurRank float64
		yesN, noN := 0, 0
		for _, b := range m.books {
			yes += b.yes
			no += b.no
			yesCash += b.yesCash
			noCash += b.noCash
			if b.hasYesPx {
				hasYesPx = true
			}
			if b.hasNoPx {
				hasNoPx = true
			}
			net := b.yes - b.no
			switch {
			case net >= posShareEps:
				yesN++
				if b.hasYesCur && (!hasYesCur || b.yes >= yesCurRank) {
					yesCur = b.yesCur
					hasYesCur = true
					yesCurRank = b.yes
				}
			case -net >= posShareEps:
				noN++
				if b.hasNoCur && (!hasNoCur || b.no >= noCurRank) {
					noCur = b.noCur
					hasNoCur = true
					noCurRank = b.no
				}
			}
		}
		net := yes - no
		if math.Abs(net) >= posShareEps {
			h := PortHolding{Title: m.title, Slug: m.slug, EventSlug: m.event}
			if net > 0 {
				h.Size = net
				h.Outcome = "YES"
				h.Wallets = yesN
				if hasYesCur {
					h.CurPrice = yesCur
					h.HasCur = true
				}
			} else {
				h.Size = -net
				h.Outcome = "NO"
				h.Wallets = noN
				if hasNoCur {
					h.CurPrice = noCur
					h.HasCur = true
				}
			}
			if avg, ok := effectiveNetAvg(yes, no, yesCash, noCash); ok && (hasYesPx || hasNoPx) {
				h.AvgPrice = avg
				h.HasAvg = true
			}
			if h.HasCur {
				h.MarketValue = h.Size * h.CurPrice
			}
			out = append(out, h)
			continue
		}

		type otherAcc struct {
			title, slug, event string
			size, cash, cur    float64
			hasPx, hasCur      bool
			curRank            float64
			wallets            int
		}
		others := make(map[string]*otherAcc)
		for _, b := range m.books {
			for name, leg := range b.other {
				if math.Abs(leg.size) < posShareEps {
					continue
				}
				o := others[name]
				if o == nil {
					o = &otherAcc{}
					others[name] = o
				}
				o.wallets++
				o.size += leg.size
				if leg.hasPx {
					o.cash += leg.cash
					o.hasPx = true
				}
				if leg.hasCur && (!o.hasCur || leg.size >= o.curRank) {
					o.cur = leg.cur
					o.hasCur = true
					o.curRank = leg.size
				}
				if t := strings.TrimSpace(leg.title); t != "" {
					o.title = t
				}
				if s := strings.TrimSpace(leg.slug); s != "" {
					o.slug = s
				}
				if e := strings.TrimSpace(leg.event); e != "" {
					o.event = e
				}
			}
		}
		for name, o := range others {
			if math.Abs(o.size) < posShareEps {
				continue
			}
			h := PortHolding{
				Title:     firstNonEmpty(o.title, m.title),
				Slug:      firstNonEmpty(o.slug, m.slug),
				EventSlug: firstNonEmpty(o.event, m.event),
				Size:      math.Abs(o.size),
				Outcome:   strings.ToUpper(name),
				Wallets:   o.wallets,
			}
			if o.hasCur {
				h.CurPrice = o.cur
				h.HasCur = true
				h.MarketValue = h.Size * h.CurPrice
			}
			if o.hasPx && o.size != 0 {
				h.AvgPrice = o.cash / o.size
				h.HasAvg = true
			}
			out = append(out, h)
		}
	}
	return out
}

// FetchFullPortReport loads each tracked wallet's open positions and builds /fullport.
// A wallet that fails is counted on the report and left out of the book.
// err is set when every wallet failed, or the context ended the fetch early.
func FetchFullPortReport(ctx context.Context, api interface {
	positionLookup
	sportsLookup
}, wallets []sharps.Wallet, limit int) (FullPortReport, error) {
	byWallet, failed, truncated, err := fetchFullPortBooks(ctx, api, wallets)
	if err != nil && len(byWallet) == 0 {
		return FullPortReport{Failed: failed, Limit: limit}, err
	}
	rep := BuildFullPortReport(ctx, api, byWallet, truncated, failed, limit)
	return rep, err
}

func fetchFullPortBooks(ctx context.Context, api positionLookup, wallets []sharps.Wallet) (map[string][]polymarket.Position, int, bool, error) {
	byWallet := make(map[string][]polymarket.Position)
	if len(wallets) == 0 || api == nil {
		return byWallet, 0, false, nil
	}
	type res struct {
		addr      string
		pos       []polymarket.Position
		truncated bool
		err       error
	}
	results := make(chan res, len(wallets))
	jobs := make(chan sharps.Wallet)
	var wg sync.WaitGroup
	workers := posWorkerCount(len(wallets))
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range jobs {
				pos, truncated, err := fetchAllPositions(ctx, api, w.Address)
				results <- res{
					addr:      strings.ToLower(strings.TrimSpace(w.Address)),
					pos:       pos,
					truncated: truncated,
					err:       err,
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, w := range wallets {
			select {
			case <-ctx.Done():
				return
			case jobs <- w:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	failed := 0
	truncated := false
	var firstErr error
	for r := range results {
		if r.err != nil {
			failed++
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		if r.truncated {
			truncated = true
		}
		if r.addr == "" {
			continue
		}
		byWallet[r.addr] = append(byWallet[r.addr], r.pos...)
	}
	if ctx.Err() != nil && len(byWallet) < len(wallets) {
		if firstErr == nil {
			firstErr = ctx.Err()
		}
		return byWallet, failed, truncated, firstErr
	}
	if failed == len(wallets) && firstErr != nil {
		return byWallet, failed, truncated, firstErr
	}
	return byWallet, failed, truncated, nil
}

// FormatFullPortReport is one or more Telegram bodies (split under the 4096 cap).
// The parenthetical after the market name is the wallet count. Names are omitted.
func FormatFullPortReport(r FullPortReport) []string {
	head := "All tracked"
	if r.Truncated {
		head += "\n(Position book truncated — some small holdings may be missing.)"
	}
	if r.More && r.Limit > 0 {
		head += fmt.Sprintf("\n(top %d by market value)", r.Limit)
	}
	if len(r.Holdings) == 0 {
		body := head + "\nNo open non-sports holdings of $100+."
		if r.Failed > 0 {
			body += fmt.Sprintf("\n(%d wallet lookups failed.)", r.Failed)
		}
		return []string{body}
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
		line := fmt.Sprintf("%s %s  %s (%d)%s", formatShares(h.Size), h.Outcome, title, h.Wallets, formatAcqCur(h.HasAvg, h.HasCur, h.AvgPrice, h.CurPrice))
		blocks = append(blocks, line)
	}
	if r.Failed > 0 {
		blocks = append(blocks, fmt.Sprintf("(%d wallet lookups failed.)", r.Failed))
	}
	return chunkTelegram(head, blocks)
}
