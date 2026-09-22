package alert

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/holgstr/detector/internal/kalshi"
	"github.com/holgstr/detector/internal/polymarket"
)

type kalshiBookAPI interface {
	TextSearch(ctx context.Context, query string) ([]kalshi.Market, error)
	Market(ctx context.Context, ticker string) (kalshi.Market, error)
	MarketsByEvent(ctx context.Context, event string) ([]kalshi.Market, error)
	Books(ctx context.Context, tickers []string) (map[string]kalshi.Book, error)
}

// FetchKalshiOB resolves a Kalshi event or outcome and loads the inside of each Yes book.
func FetchKalshiOB(ctx context.Context, api kalshiBookAPI, query string) (OBReport, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return OBReport{}, fmt.Errorf("empty query")
	}
	markets, err := resolveKalshiMarkets(ctx, api, query)
	if err != nil {
		return OBReport{Query: query}, err
	}
	sort.SliceStable(markets, func(i, j int) bool {
		if markets[i].LastPrice != markets[j].LastPrice {
			return markets[i].LastPrice > markets[j].LastPrice
		}
		return markets[i].Name < markets[j].Name
	})
	if len(markets) > 50 {
		markets = markets[:50]
	}
	syms := make([]string, len(markets))
	for i, m := range markets {
		syms[i] = m.Ticker
	}
	books, err := api.Books(ctx, syms)
	rep := OBReport{
		Query: query,
		Title: kalshiTitle(markets),
		Slug:  markets[0].EventTicker,
		Books: kalshiBooks(markets, books),
	}
	return rep, err
}

func resolveKalshiMarkets(ctx context.Context, api kalshiBookAPI, query string) ([]kalshi.Market, error) {
	if looksKalshiTicker(query) {
		ticker := strings.ToUpper(query)
		m, err := api.Market(ctx, ticker)
		if err == nil && strings.EqualFold(m.Ticker, ticker) {
			if m.Resolved {
				return nil, fmt.Errorf("%q is resolved, not a live Kalshi market", query)
			}
			if m.EventTicker != "" {
				if all, evErr := api.MarketsByEvent(ctx, m.EventTicker); evErr == nil {
					for _, sib := range all {
						if strings.EqualFold(sib.Ticker, ticker) && !sib.Resolved {
							return []kalshi.Market{sib}, nil
						}
					}
				}
			}
			return []kalshi.Market{m}, nil
		}
		if err != nil && !errors.Is(err, kalshi.ErrNotFound) {
			return nil, err
		}
		found, evErr := api.MarketsByEvent(ctx, ticker)
		if evErr == nil {
			live, resolved := liveKalshi(found, ticker)
			if len(live) > 0 {
				return live, nil
			}
			if resolved {
				return nil, fmt.Errorf("%q is resolved, not a live Kalshi market", query)
			}
		} else if !errors.Is(evErr, kalshi.ErrNotFound) {
			return nil, evErr
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
	pick, err := kalshi.PickMarkets(query, hits)
	if err != nil {
		return nil, err
	}
	markets := pick.Markets
	if pick.ExpandEvent && pick.EventTicker != "" {
		all, evErr := api.MarketsByEvent(ctx, pick.EventTicker)
		if evErr == nil {
			live, _ := liveKalshi(all, pick.EventTicker)
			if len(live) > 0 {
				markets = live
			}
		}
	}
	if len(markets) == 0 {
		return nil, fmt.Errorf("no live Kalshi market matching %q", query)
	}
	return markets, nil
}

func liveKalshi(markets []kalshi.Market, event string) (live []kalshi.Market, resolved bool) {
	for _, m := range markets {
		if event != "" && !strings.EqualFold(m.EventTicker, event) {
			continue
		}
		if m.Resolved {
			resolved = true
			continue
		}
		live = append(live, m)
	}
	return live, resolved
}

func kalshiTitle(markets []kalshi.Market) string {
	if len(markets) == 0 {
		return ""
	}
	if t := strings.TrimSpace(markets[0].Event); t != "" {
		return t
	}
	return markets[0].EventTicker
}

func kalshiBooks(markets []kalshi.Market, books map[string]kalshi.Book) []polymarket.OutcomeBook {
	out := make([]polymarket.OutcomeBook, 0, len(markets))
	for _, m := range markets {
		b, ok := books[m.Ticker]
		if !ok {
			for sym, book := range books {
				if strings.EqualFold(sym, m.Ticker) {
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
			name = "Yes"
		}
		tick := m.Tick
		if tick <= 0 {
			tick = 0.01
		}
		out = append(out, polymarket.OutcomeBook{
			Outcome: name,
			Tick:    tick,
			Bids:    kalshiSide(b.Bids),
			Asks:    kalshiSide(b.Asks),
		})
	}
	return out
}

func kalshiSide(levels []kalshi.Level) []polymarket.BookLevel {
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

func looksKalshiTicker(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t/") || !strings.Contains(s, "-") {
		return false
	}
	for _, r := range s {
		if r == '-' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// FormatKalshiOB is the Telegram body for /obk.
// Every outcome in the event is shown. A lone Yes book omits the side name.
func FormatKalshiOB(r OBReport) string {
	return FormatPascalOB(r)
}
