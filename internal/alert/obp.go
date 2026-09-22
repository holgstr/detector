package alert

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/holgstr/detector/internal/pascal"
	"github.com/holgstr/detector/internal/polymarket"
)

type pascalBookAPI interface {
	TextSearch(ctx context.Context, query string) ([]pascal.Market, error)
	MarketsBySymbols(ctx context.Context, symbols []string) ([]pascal.Market, error)
	MarketsByEvent(ctx context.Context, event string) ([]pascal.Market, error)
	Books(ctx context.Context, symbols []string) (map[string]pascal.Book, error)
}

// FetchPascalOB resolves a Pascal event or outcome and loads the inside of each book.
func FetchPascalOB(ctx context.Context, api pascalBookAPI, query string) (OBReport, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return OBReport{}, fmt.Errorf("empty query")
	}
	markets, err := resolvePascalMarkets(ctx, api, query)
	if err != nil {
		return OBReport{Query: query}, err
	}
	sort.SliceStable(markets, func(i, j int) bool {
		if markets[i].MarkPrice != markets[j].MarkPrice {
			return markets[i].MarkPrice > markets[j].MarkPrice
		}
		return markets[i].Name < markets[j].Name
	})
	if len(markets) > 50 {
		markets = markets[:50]
	}
	syms := make([]string, len(markets))
	for i, m := range markets {
		syms[i] = m.Symbol
	}
	books, err := api.Books(ctx, syms)
	rep := OBReport{
		Query: query,
		Title: pascalTitle(markets),
		Slug:  markets[0].EventCode,
		Books: pascalBooks(markets, books),
	}
	return rep, err
}

func resolvePascalMarkets(ctx context.Context, api pascalBookAPI, query string) ([]pascal.Market, error) {
	if looksLikePascalSymbol(query) {
		found, err := api.MarketsBySymbols(ctx, []string{strings.ToUpper(query)})
		if err == nil {
			var live []pascal.Market
			resolved := false
			for _, m := range found {
				if strings.EqualFold(m.Symbol, query) {
					if m.Resolved {
						resolved = true
						continue
					}
					live = append(live, m)
				}
			}
			if len(live) > 0 {
				return live, nil
			}
			if resolved {
				return nil, fmt.Errorf("%q is resolved, not a live Pascal market", query)
			}
		}
	}

	if utf8.RuneCountInString(query) < 3 {
		return nil, fmt.Errorf("short query")
	}
	searchQuery := query
	if utf8.RuneCountInString(searchQuery) > 64 {
		searchQuery = string([]rune(searchQuery)[:64])
	}
	hits, err := api.TextSearch(ctx, searchQuery)
	if err != nil {
		return nil, err
	}
	pick, err := pascal.PickMarkets(query, hits)
	if err != nil {
		return nil, err
	}
	markets := pick.Markets
	if pick.ExpandEvent && pick.EventCode != "" {
		all, evErr := api.MarketsByEvent(ctx, pick.EventCode)
		if evErr == nil {
			var live []pascal.Market
			for _, m := range all {
				if !m.Resolved && m.EventCode == pick.EventCode {
					live = append(live, m)
				}
			}
			if len(live) > 0 {
				markets = live
			}
		}
	}
	if len(markets) == 0 {
		return nil, fmt.Errorf("no live Pascal market matching %q", query)
	}
	return markets, nil
}

func pascalTitle(markets []pascal.Market) string {
	if len(markets) == 0 {
		return ""
	}
	if t := strings.TrimSpace(markets[0].Event); t != "" {
		return t
	}
	return markets[0].EventCode
}

func pascalBooks(markets []pascal.Market, books map[string]pascal.Book) []polymarket.OutcomeBook {
	out := make([]polymarket.OutcomeBook, 0, len(markets))
	for _, m := range markets {
		b, ok := books[m.Symbol]
		if !ok {
			for sym, book := range books {
				if strings.EqualFold(sym, m.Symbol) {
					b = book
					ok = true
					break
				}
			}
		}
		if !ok {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.Symbol
		}
		tick := m.TickMin
		if tick <= 0 {
			tick = 0.01
		}
		out = append(out, polymarket.OutcomeBook{
			Outcome: name,
			Tick:    tick,
			Bids:    pascalSide(b.Bids),
			Asks:    pascalSide(b.Asks),
		})
	}
	return out
}

func pascalSide(levels []pascal.Level) []polymarket.BookLevel {
	if len(levels) > polymarket.OrderBookDepth {
		levels = levels[:polymarket.OrderBookDepth]
	}
	out := make([]polymarket.BookLevel, 0, len(levels))
	for _, lv := range levels {
		if lv.Size <= 0 || lv.Price <= 0 {
			continue
		}
		out = append(out, polymarket.BookLevel{Price: lv.Price, Size: lv.Size})
	}
	return out
}

func looksLikePascalSymbol(s string) bool {
	s = strings.TrimSpace(s)
	i := strings.IndexByte(s, '.')
	if i <= 0 || i == len(s)-1 || strings.ContainsAny(s, " \t/") {
		return false
	}
	return strings.Count(s, ".") == 1
}

// FormatPascalOB is the Telegram body for /obp.
// Every outcome in the event is shown. A lone Yes book omits the side name.
func FormatPascalOB(r OBReport) string {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = strings.TrimSpace(r.Query)
	}
	var b strings.Builder
	b.WriteString(title)
	if len(r.Books) == 0 {
		b.WriteString("\n\nNo orders.")
		return b.String()
	}
	nameOutcomes := !(len(r.Books) == 1 && isYesOutcome(r.Books[0].Outcome))
	for _, book := range r.Books {
		if nameOutcomes {
			b.WriteByte('\n')
			b.WriteString(strings.ToUpper(strings.TrimSpace(book.Outcome)))
		}
		b.WriteString(formatBookLadder(book))
	}
	return b.String()
}
