// Package venuequery turns a Polymarket-style question into the few words
// Kalshi and Pascal use for the same main market.
//
// "Will the Republicans win the Nevada governor race" is the Nevada governor
// winner, Republican side. The venues title that "Nevada Governor winner?"
// and name the side "Joe Lombardo" or "Republicans", so a match that insists
// on every word in the question misses it.
package venuequery

import (
	"strings"
	"unicode"
)

// SearchQuery is the text to send to a venue search API: the subject, without
// question boilerplate or a party name. Party words are kept for local
// matching (MatchTokens) but dropped here because Pascal's search misses the
// event when the party is not part of the event title.
func SearchQuery(query string) string {
	kept := withoutParty(MatchTokens(query))
	if len(kept) == 0 {
		kept = MatchTokens(query)
	}
	if len(kept) == 0 {
		return strings.TrimSpace(query)
	}
	return strings.Join(kept, " ")
}

// MatchTokens is the query with question boilerplate and years removed.
// "Will the Republicans win the Nevada governor race in 2026" becomes
// republicans, nevada, governor. If that drops everything, the raw tokens
// are returned.
func MatchTokens(query string) []string {
	raw := Tokens(query)
	kept := make([]string, 0, len(raw))
	for _, t := range raw {
		if stopword(t) || isYear(t) {
			continue
		}
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		return raw
	}
	return kept
}

// Tokens splits query into lowercase words.
func Tokens(query string) []string {
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

// ContainsAll reports whether every token appears in hay, allowing a party
// or office stem (republicans → republican, governorship → governor).
func ContainsAll(hay string, toks []string) bool {
	hay = strings.ToLower(hay)
	if len(toks) == 0 {
		return false
	}
	for _, t := range toks {
		if !tokenIn(hay, t) {
			return false
		}
	}
	return true
}

// PartyHint is "republican" or "democrat" when symbol's last segment is a
// party code (GOVPARTYNV-26-R, NV_GOV_2026.REP). Candidate names on Kalshi
// do not say the party; the ticker does.
func PartyHint(symbol string) string {
	switch lastSegment(strings.ToLower(strings.TrimSpace(symbol))) {
	case "r", "rep", "gop":
		return "republican"
	case "d", "dem":
		return "democrat"
	default:
		return ""
	}
}

// EventPenalty counts content words in an event title the query did not ask
// for. "Nevada Governor winner?" is 0 for "nevada governor"; "Nevada Governor
// margin of victory" is 2. Generic words (winner, the, of) are ignored.
func EventPenalty(eventTitle, query string) int {
	asked := make(map[string]struct{})
	for _, t := range MatchTokens(query) {
		asked[t] = struct{}{}
		if f := fold(t); f != t {
			asked[f] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	n := 0
	for _, w := range Tokens(eventTitle) {
		if stopword(w) || isYear(w) || w == "winner" || w == "winners" {
			continue
		}
		fw := fold(w)
		if _, ok := asked[w]; ok {
			continue
		}
		if _, ok := asked[fw]; ok {
			continue
		}
		if _, dup := seen[fw]; dup {
			continue
		}
		seen[fw] = struct{}{}
		n++
	}
	return n
}

func withoutParty(toks []string) []string {
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		switch fold(t) {
		case "republican", "democrat":
			continue
		}
		out = append(out, t)
	}
	return out
}

func tokenIn(hay, tok string) bool {
	if tok == "" {
		return false
	}
	if strings.Contains(hay, tok) {
		return true
	}
	f := fold(tok)
	return f != tok && strings.Contains(hay, f)
}

func fold(tok string) string {
	switch tok {
	case "gop", "republican", "republicans":
		return "republican"
	case "dem", "dems", "democrat", "democrats", "democratic":
		return "democrat"
	case "governors", "governorship":
		return "governor"
	default:
		return tok
	}
}

func lastSegment(s string) string {
	cut := -1
	for i, r := range s {
		if r == '-' || r == '.' || r == '_' {
			cut = i
		}
	}
	if cut < 0 || cut+1 >= len(s) {
		return s
	}
	return s[cut+1:]
}

func isYear(t string) bool {
	if len(t) != 4 || (!strings.HasPrefix(t, "19") && !strings.HasPrefix(t, "20")) {
		return false
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func stopword(t string) bool {
	switch t {
	case "a", "an", "the", "of", "to", "be", "in", "on", "for", "by", "and", "or",
		"will", "would", "can", "may",
		"who", "what", "when", "which",
		"do", "does", "did", "is", "are", "was", "were",
		"win", "wins", "winning", "winner", "winners",
		"race", "races",
		"election", "elections",
		"party",
		"this", "that", "these", "those",
		"from", "with", "at", "into", "about", "over", "under",
		"it", "its", "their",
		"yes", "no":
		return true
	default:
		return false
	}
}
