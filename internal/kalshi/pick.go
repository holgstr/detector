package kalshi

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pick is which outcome books /obk should load.
// ExpandEvent means the query named the event, so every live outcome belongs.
type Pick struct {
	EventTicker string
	Markets     []Market
	ExpandEvent bool
}

// PickMarkets chooses an event from text-search hits.
// An event-level query (florida governor) expands to every outcome.
// A query that also names one outcome (Byron Donalds) stays on that book.
func PickMarkets(query string, hits []Market) (Pick, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Pick{}, fmt.Errorf("empty query")
	}
	if !looksLikeTicker(query) && utf8.RuneCountInString(query) < 3 {
		return Pick{}, fmt.Errorf("short query")
	}

	for _, h := range hits {
		if strings.EqualFold(h.Ticker, query) && !h.Resolved {
			return Pick{EventTicker: h.EventTicker, Markets: []Market{h}}, nil
		}
	}

	toks := tokens(query)
	if len(toks) == 0 {
		return Pick{}, fmt.Errorf("empty query")
	}

	type scored struct {
		m    Market
		rank int
	}
	var matched []scored
	best := 0
	for _, h := range hits {
		if h.Resolved || h.Ticker == "" {
			continue
		}
		rank := matchRank(h, toks)
		if rank == 0 {
			continue
		}
		if rank > best {
			best = rank
		}
		matched = append(matched, scored{m: h, rank: rank})
	}
	if best == 0 {
		return Pick{}, fmt.Errorf("no live Kalshi market matching %q", query)
	}

	event := ""
	for _, s := range matched {
		if s.rank == best {
			event = s.m.EventTicker
			break
		}
	}
	var chosen []Market
	for _, s := range matched {
		if s.rank == best && s.m.EventTicker == event {
			chosen = append(chosen, s.m)
		}
	}
	if len(chosen) == 0 {
		return Pick{}, fmt.Errorf("no live Kalshi market matching %q", query)
	}
	expand := best == 2 || queryNamesEvent(query, event)
	return Pick{EventTicker: event, Markets: chosen, ExpandEvent: expand}, nil
}

func queryNamesEvent(query, event string) bool {
	q := strings.Trim(strings.ToUpper(strings.TrimSpace(query)), "-")
	return q != "" && q == strings.ToUpper(event)
}

func matchRank(m Market, toks []string) int {
	name := hay(m.Name)
	event := hay(m.Event)
	sym := hay(m.Ticker + " " + m.EventTicker)
	if hasAll(name, toks) {
		return 3
	}
	if hasAll(event, toks) {
		return 2
	}
	if hasAll(sym, toks) || hasAll(name+" "+event+" "+sym, toks) {
		return 1
	}
	return 0
}

func hay(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func hasAll(haystack string, toks []string) bool {
	for _, t := range toks {
		if !strings.Contains(haystack, t) {
			return false
		}
	}
	return true
}

func tokens(query string) []string {
	var b strings.Builder
	for _, r := range strings.ToLower(query) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte(' ')
	}
	return strings.Fields(b.String())
}

// looksLikeTicker reports a Kalshi event or market ticker (KX...-26OCT-H0).
func looksLikeTicker(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t/") || !strings.Contains(s, "-") {
		return false
	}
	for _, r := range s {
		if r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return true
}
