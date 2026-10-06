package kalshi

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/holgstr/detector/internal/venuequery"
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
// A Polymarket question ("Will the Republicans win the Nevada governor race")
// drops boilerplate and matches the main winner market, then the named party.
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

	toks := venuequery.MatchTokens(query)
	if len(toks) == 0 {
		return Pick{}, fmt.Errorf("empty query")
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

	event := chooseEvent(matched, best, query)
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

type scored struct {
	m    Market
	rank int
}

// chooseEvent picks one event among the best text-match rank.
// Ties prefer the main market (fewer extra title words such as margin,
// county, or lieutenant) over a side market, then higher volume.
func chooseEvent(matched []scored, best int, query string) string {
	type cand struct {
		code    string
		penalty int
		volume  float64
		order   int
	}
	var cands []cand
	index := make(map[string]int)
	for i, s := range matched {
		if s.rank != best {
			continue
		}
		code := s.m.EventTicker
		j, ok := index[code]
		if !ok {
			index[code] = len(cands)
			cands = append(cands, cand{
				code:    code,
				penalty: venuequery.EventPenalty(s.m.Event, query),
				order:   i,
			})
			j = index[code]
		}
		c := cands[j]
		c.volume += s.m.Volume
		cands[j] = c
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].penalty != cands[j].penalty {
			return cands[i].penalty < cands[j].penalty
		}
		if best == 2 && cands[i].volume != cands[j].volume {
			return cands[i].volume > cands[j].volume
		}
		return cands[i].order < cands[j].order
	})
	if len(cands) == 0 {
		return ""
	}
	return cands[0].code
}

func queryNamesEvent(query, event string) bool {
	q := strings.Trim(strings.ToUpper(strings.TrimSpace(query)), "-")
	return q != "" && q == strings.ToUpper(event)
}

func matchRank(m Market, toks []string) int {
	hint := venuequery.PartyHint(m.Ticker)
	name := strings.ToLower(strings.TrimSpace(m.Name + " " + hint))
	event := strings.ToLower(strings.TrimSpace(m.Event))
	title := strings.ToLower(strings.TrimSpace(m.Title))
	sym := strings.ToLower(m.Ticker + " " + m.EventTicker)
	if venuequery.ContainsAll(name, toks) {
		return 3
	}
	if venuequery.ContainsAll(event, toks) {
		return 2
	}
	if venuequery.ContainsAll(sym, toks) || venuequery.ContainsAll(name+" "+event+" "+title+" "+sym, toks) {
		return 1
	}
	return 0
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
