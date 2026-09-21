package polymarket

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

const defaultSearchLimit = 25

// SearchMarket is one Gamma public-search hit (a single tradeable market).
type SearchMarket struct {
	Market         Market
	GroupItemTitle string
	EventTitle     string
	Volume         float64
	Volume24hr     float64
	Liquidity      float64
	Active         bool
	Closed         bool
}

type publicSearchResponse struct {
	Events []publicSearchEvent `json:"events"`
}

type publicSearchEvent struct {
	Title   string               `json:"title"`
	Slug    string               `json:"slug"`
	Active  bool                 `json:"active"`
	Closed  bool                 `json:"closed"`
	Volume  any                  `json:"volume"`
	Markets []publicSearchMarket `json:"markets"`
}

type publicSearchMarket struct {
	ConditionID    string  `json:"conditionId"`
	Slug           string  `json:"slug"`
	Question       string  `json:"question"`
	Outcomes       string  `json:"outcomes"`
	GroupItemTitle string  `json:"groupItemTitle"`
	Volume24hr     float64 `json:"volume24hr"`
	VolumeNum      float64 `json:"volumeNum"`
	Volume         any     `json:"volume"`
	LiquidityNum   float64 `json:"liquidityNum"`
	Liquidity      any     `json:"liquidity"`
	Active         bool    `json:"active"`
	Closed         bool    `json:"closed"`
	Archived       bool    `json:"archived"`
}

// SearchMarkets runs Gamma /public-search and flattens nested event markets.
func (c *Client) SearchMarkets(ctx context.Context, query string) ([]SearchMarket, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}

	q := url.Values{}
	q.Set("q", query)
	q.Set("limit_per_type", fmt.Sprintf("%d", defaultSearchLimit))
	q.Set("events_status", "active")
	q.Set("keep_closed_markets", "0")
	q.Set("sort", "volume24hr")
	q.Set("ascending", "false")
	q.Set("search_profiles", "false")
	q.Set("search_tags", "false")

	var resp publicSearchResponse
	if err := c.getJSON(ctx, gammaBase+"/public-search?"+q.Encode(), &resp); err != nil {
		return nil, err
	}

	out := make([]SearchMarket, 0, 32)
	seen := make(map[string]struct{})
	for _, ev := range resp.Events {
		if ev.Closed || !ev.Active {
			continue
		}
		for _, g := range ev.Markets {
			if !marketIsLive(g.Active, g.Closed, g.Archived) {
				continue
			}
			cid := strings.TrimSpace(g.ConditionID)
			if cid == "" {
				continue
			}
			if _, ok := seen[cid]; ok {
				continue
			}
			seen[cid] = struct{}{}
			outcomes := parseJSONStringArray(g.Outcomes)
			if len(outcomes) == 0 {
				outcomes = []string{"Yes", "No"}
			}
			mar := Market{
				ConditionID: cid,
				Slug:        g.Slug,
				Question:    g.Question,
				Outcomes:    outcomes,
				EventSlug:   ev.Slug,
			}
			if ev.Slug != "" && g.Slug != "" {
				mar.URL = "https://polymarket.com/event/" + ev.Slug + "/" + g.Slug
			} else if g.Slug != "" {
				mar.URL = "https://polymarket.com/market/" + g.Slug
			}
			out = append(out, SearchMarket{
				Market:         mar,
				GroupItemTitle: g.GroupItemTitle,
				EventTitle:     ev.Title,
				Volume:         firstFloat(g.VolumeNum, g.Volume),
				Volume24hr:     g.Volume24hr,
				Liquidity:      firstFloat(g.LiquidityNum, g.Liquidity),
				Active:         g.Active,
				Closed:         g.Closed,
			})
		}
	}
	return out, nil
}

// FindMarket resolves a condition id, URL, slug, or free-text name.
// Only active, unresolved markets are returned. Word queries use
// public-search and pick the live hit whose market (or event) title
// contains the words with the strongest volume / liquidity interest.
func (c *Client) FindMarket(ctx context.Context, query string) (SearchMarket, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchMarket{}, fmt.Errorf("empty market query")
	}

	if isConditionID(query) || looksLikeMarketURL(query) || looksLikeSlug(query) {
		sm, err := c.resolveSearchMarket(ctx, query)
		if err == nil {
			if !isLiveMarket(sm) {
				return SearchMarket{}, fmt.Errorf("market %q is resolved, not active", query)
			}
			return sm, nil
		}
		if isConditionID(query) || looksLikeMarketURL(query) {
			return SearchMarket{}, err
		}
	}

	hits, err := c.SearchMarkets(ctx, query)
	if err != nil {
		return SearchMarket{}, err
	}
	best, ok := PickBestMarket(query, hits)
	if !ok {
		return SearchMarket{}, fmt.Errorf("no active market matching %q", query)
	}
	return best, nil
}

func (c *Client) resolveSearchMarket(ctx context.Context, query string) (SearchMarket, error) {
	var g gammaMarket
	var err error
	if isConditionID(query) {
		g, err = c.fetchGammaByCondition(ctx, query)
	} else {
		slug, sErr := extractMarketSlug(query)
		if sErr != nil {
			return SearchMarket{}, sErr
		}
		g, err = c.fetchGammaBySlug(ctx, slug)
	}
	if err != nil {
		return SearchMarket{}, err
	}
	return searchMarketFromGamma(g), nil
}

func searchMarketFromGamma(g gammaMarket) SearchMarket {
	return SearchMarket{
		Market: toMarket(g),
		Active: g.Active,
		Closed: g.Closed || g.Archived,
	}
}

// PickBestMarket chooses an active, unresolved market for a word query.
// Own titles/slugs outrank event titles (Andersson → Magdalena, not a sibling
// in the same event). Name queries prefer the overall winner market (Flavio →
// presidential election, not first-round most-votes). Then MarketInterest.
func PickBestMarket(query string, hits []SearchMarket) (SearchMarket, bool) {
	top := TopRankMatches(query, hits)
	if len(top) == 0 {
		return SearchMarket{}, false
	}
	return top[0], true
}

// MarketInterest is how "real" a colliding live market is: recent volume,
// then lifetime volume, then quoted liquidity (open-interest proxy).
func MarketInterest(h SearchMarket) float64 {
	return h.Volume24hr*2 + h.Volume + h.Liquidity
}

func preferMarket(a, b SearchMarket) bool {
	ia, ib := MarketInterest(a), MarketInterest(b)
	if ia != ib {
		return ia > ib
	}
	if a.Volume24hr != b.Volume24hr {
		return a.Volume24hr > b.Volume24hr
	}
	if a.Volume != b.Volume {
		return a.Volume > b.Volume
	}
	if a.Liquidity != b.Liquidity {
		return a.Liquidity > b.Liquidity
	}
	return a.Market.Question < b.Market.Question
}

// TopRankMatches returns live markets sharing the best text-match rank for
// query. Unless the query itself names a side market (most votes, first round,
// …), overall winner contracts are kept and side markets dropped. Sorted by
// MarketInterest (same order as PickBestMarket).
func TopRankMatches(query string, hits []SearchMarket) []SearchMarket {
	toks := searchTokens(query)
	if len(toks) == 0 || len(hits) == 0 {
		return nil
	}

	type scored struct {
		hit  SearchMarket
		rank int
	}
	var matched []scored
	bestRank := 0
	for _, h := range hits {
		if !isLiveMarket(h) || strings.TrimSpace(h.Market.ConditionID) == "" {
			continue
		}
		rank := matchRank(h, toks)
		if rank == 0 {
			continue
		}
		if rank > bestRank {
			bestRank = rank
		}
		matched = append(matched, scored{hit: h, rank: rank})
	}
	if bestRank == 0 {
		return nil
	}
	top := make([]SearchMarket, 0, len(matched))
	for _, m := range matched {
		if m.rank == bestRank {
			top = append(top, m.hit)
		}
	}
	top = preferPrimaryMarkets(top, toks)
	sort.SliceStable(top, func(i, j int) bool {
		return preferMarket(top[i], top[j])
	})
	return top
}

func preferPrimaryMarkets(hits []SearchMarket, toks []string) []SearchMarket {
	if len(hits) <= 1 || queryWantsSideMarket(toks) {
		return hits
	}
	var winners, rest []SearchMarket
	for _, h := range hits {
		if isSideMarket(h) {
			continue
		}
		rest = append(rest, h)
		if isOverallWinner(h) {
			winners = append(winners, h)
		}
	}
	if len(winners) > 0 {
		return winners
	}
	if len(rest) > 0 {
		return rest
	}
	return hits
}

func queryWantsSideMarket(toks []string) bool {
	joined := " " + strings.Join(toks, " ") + " "
	for _, w := range []string{
		"votes", "round", "share", "runoff", "second", "third", "2nd", "3rd",
		"debate", "arrest", "arrested", "charged", "qualify", "place",
		"percent", "pct",
	} {
		if strings.Contains(joined, " "+w+" ") {
			return true
		}
	}
	return false
}

func isSideMarket(h SearchMarket) bool {
	hay := marketText(h)
	for _, p := range []string{
		"most votes",
		"first round",
		"second place",
		"third place",
		"2nd place",
		"3rd place",
		"finish in",
		"vote share",
		"valid vote",
		"runoff",
		"qualify for",
		"debate",
		"charged",
		"arrested",
		"less than",
		" or more of ",
		"between ",
	} {
		if strings.Contains(hay, p) {
			return true
		}
	}
	return false
}

func isOverallWinner(h SearchMarket) bool {
	if isSideMarket(h) {
		return false
	}
	hay := marketText(h)
	switch {
	case strings.Contains(hay, "next prime minister"),
		strings.Contains(hay, "next president"),
		strings.Contains(hay, "be the next"):
		return true
	case strings.Contains(hay, " win the ") &&
		(strings.Contains(hay, "election") ||
			strings.Contains(hay, "president") ||
			strings.Contains(hay, "prime minister") ||
			strings.Contains(hay, "championship")):
		return true
	default:
		return false
	}
}

func marketText(h SearchMarket) string {
	return foldSearchText(strings.Join([]string{
		h.GroupItemTitle,
		h.Market.Question,
		h.EventTitle,
		strings.ReplaceAll(h.Market.Slug, "-", " "),
		strings.ReplaceAll(h.Market.EventSlug, "-", " "),
	}, " "))
}

// IsExplicitMarketRef reports whether query is a condition id, market URL, or slug.
func IsExplicitMarketRef(query string) bool {
	query = strings.TrimSpace(query)
	return isConditionID(query) || looksLikeMarketURL(query) || looksLikeSlug(query)
}

func isLiveMarket(h SearchMarket) bool {
	return marketIsLive(h.Active, h.Closed, false)
}

func marketIsLive(active, closed, archived bool) bool {
	return active && !closed && !archived
}

func matchRank(h SearchMarket, toks []string) int {
	if haystackHasAll(marketHaystack(h), toks) {
		return 2
	}
	if haystackHasAll(eventHaystack(h), toks) {
		return 1
	}
	return 0
}

func marketHaystack(h SearchMarket) string {
	parts := []string{
		h.GroupItemTitle,
		h.Market.Question,
		strings.ReplaceAll(h.Market.Slug, "-", " "),
	}
	return foldSearchText(strings.Join(parts, " "))
}

func eventHaystack(h SearchMarket) string {
	parts := []string{
		h.EventTitle,
		strings.ReplaceAll(h.Market.EventSlug, "-", " "),
	}
	return foldSearchText(strings.Join(parts, " "))
}

func searchTokens(query string) []string {
	var b strings.Builder
	for _, r := range foldSearchText(query) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte(' ')
	}
	return strings.Fields(b.String())
}

// foldSearchText lowercases and strips Latin diacritics so "Flavio" matches "Flávio".
func foldSearchText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		b.WriteRune(foldRune(r))
	}
	return b.String()
}

func foldRune(r rune) rune {
	switch r {
	case 'á', 'à', 'ä', 'â', 'ã', 'å', 'ă', 'ą':
		return 'a'
	case 'é', 'è', 'ë', 'ê', 'ě', 'ę':
		return 'e'
	case 'í', 'ì', 'ï', 'î':
		return 'i'
	case 'ó', 'ò', 'ö', 'ô', 'õ', 'ø':
		return 'o'
	case 'ú', 'ù', 'ü', 'û', 'ů':
		return 'u'
	case 'ý', 'ÿ':
		return 'y'
	case 'ç', 'ć', 'č':
		return 'c'
	case 'ñ', 'ń', 'ň':
		return 'n'
	case 'š', 'ś':
		return 's'
	case 'ž', 'ź', 'ż':
		return 'z'
	case 'ł':
		return 'l'
	case 'ř':
		return 'r'
	case 'ď':
		return 'd'
	case 'ť':
		return 't'
	default:
		return r
	}
}

func haystackHasAll(haystack string, toks []string) bool {
	for _, t := range toks {
		if !strings.Contains(haystack, t) {
			return false
		}
	}
	return true
}

func looksLikeMarketURL(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Contains(s, "://") || strings.HasPrefix(s, "polymarket.com")
}

func looksLikeSlug(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t") || !strings.Contains(s, "-") {
		return false
	}
	for _, r := range strings.ToLower(s) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !ok {
			return false
		}
	}
	return true
}
