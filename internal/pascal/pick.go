package pascal

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pick is which outcome books /obp should load.
// ExpandEvent means the query named the event, so every live outcome belongs.
type Pick struct {
	EventCode   string
	Markets     []Market
	ExpandEvent bool
}

// PickMarkets chooses an event from text-search hits.
// An event-level query (florida governor) expands to every outcome.
// A query that also names one outcome (republicans) stays on that book.
func PickMarkets(query string, hits []Market) (Pick, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Pick{}, fmt.Errorf("empty query")
	}
	if !looksLikeSymbol(query) && utf8.RuneCountInString(query) < 3 {
		return Pick{}, fmt.Errorf("short query")
	}

	for _, h := range hits {
		if strings.EqualFold(h.Symbol, query) && !h.Resolved {
			return Pick{EventCode: h.EventCode, Markets: []Market{h}}, nil
		}
	}

	toks := tokens(query)
	if len(toks) == 0 {
		return Pick{}, fmt.Errorf("empty query")
	}

	var matched []scored
	best := 0
	for _, h := range hits {
		if h.Resolved || h.Symbol == "" {
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
		return Pick{}, fmt.Errorf("no live Pascal market matching %q", query)
	}

	event := chooseEvent(matched, best, toks)
	var chosen []Market
	for _, s := range matched {
		if s.rank == best && s.m.EventCode == event {
			chosen = append(chosen, s.m)
		}
	}
	if len(chosen) == 0 {
		return Pick{}, fmt.Errorf("no live Pascal market matching %q", query)
	}
	expand := best == 2 || queryNamesEvent(query, event)
	return Pick{EventCode: event, Markets: chosen, ExpandEvent: expand}, nil
}

type scored struct {
	m    Market
	rank int
}

// chooseEvent picks one event among the best text-match rank.
// Event-level ties prefer the main market over a side market the query did
// not name (poll, margin, spread, turnout), then higher 24h volume.
// Outcome-level ties stay with the first hit, which text-search already
// orders by volume.
func chooseEvent(matched []scored, best int, queryToks []string) string {
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
		code := s.m.EventCode
		j, ok := index[code]
		if !ok {
			pen := 0
			if best == 2 {
				pen = sidePenalty(s.m.Event, queryToks)
			}
			index[code] = len(cands)
			cands = append(cands, cand{code: code, penalty: pen, order: i})
			j = index[code]
		}
		c := cands[j]
		c.volume += s.m.Volume24h
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

// sidePenalty counts derivative words in an event title that the query did
// not ask for. "Michigan Senate Winner" scores 0 for "michigan senate";
// "Emerson Michigan Senate poll margin" scores 2.
func sidePenalty(event string, queryToks []string) int {
	seen := make(map[string]struct{})
	n := 0
	for _, w := range tokens(event) {
		key, ok := sideKey(w)
		if !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		if tokenCovered(key, queryToks) || tokenCovered(w, queryToks) {
			continue
		}
		seen[key] = struct{}{}
		n++
	}
	return n
}

func sideKey(w string) (string, bool) {
	for _, s := range []string{"poll", "margin", "spread", "turnout"} {
		if w == s || strings.HasPrefix(w, s) {
			return s, true
		}
	}
	return "", false
}

func tokenCovered(word string, toks []string) bool {
	for _, t := range toks {
		if t == word || strings.Contains(word, t) || strings.Contains(t, word) {
			return true
		}
	}
	return false
}

func queryNamesEvent(query, event string) bool {
	q := strings.Trim(strings.ToUpper(strings.TrimSpace(query)), ".")
	return q != "" && q == strings.ToUpper(event)
}

func matchRank(m Market, toks []string) int {
	name := hay(m.Name)
	event := hay(m.Event)
	sym := hay(m.Symbol)
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

// looksLikeSymbol reports an EVENT.MARKET identifier.
func looksLikeSymbol(s string) bool {
	s = strings.TrimSpace(s)
	i := strings.IndexByte(s, '.')
	if i <= 0 || i == len(s)-1 || strings.ContainsAny(s, " \t/") {
		return false
	}
	for _, r := range s {
		if r == '.' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return strings.Count(s, ".") == 1
}
