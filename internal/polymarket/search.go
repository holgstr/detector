package polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

const (
	defaultSearchLimit = 25
	// outcomeSearchURL is the site search that indexes outcome and option titles
	// (groupItemTitle), which Gamma public-search does not.
	outcomeSearchURL     = "https://polymarket.com/api/search/execute"
	maxOutcomeHydrations = 6
)

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
// When that misses a name, it searches by outcome/option title (the candidate
// you can buy, such as "Jared Moskowitz (D)" on FL-25 House Election Winner).
func (c *Client) SearchMarkets(ctx context.Context, query string) ([]SearchMarket, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}
	hits, err := c.searchLiveMarkets(ctx, query)
	if err != nil {
		return nil, err
	}
	if anyTextMatch(query, hits) || !nameLikeQuery(searchTokens(query)) {
		return hits, nil
	}
	byOption, err := c.searchMarketsByOption(ctx, query)
	if err != nil || len(byOption) == 0 {
		return hits, nil
	}
	return mergeSearchMarkets(hits, byOption), nil
}

func (c *Client) searchLiveMarkets(ctx context.Context, query string) ([]SearchMarket, error) {
	resp, err := c.searchPublic(ctx, query)
	if err != nil {
		return nil, err
	}
	return liveMarketsFromSearch(resp), nil
}

func (c *Client) searchPublic(ctx context.Context, query string) (publicSearchResponse, error) {
	q := url.Values{}
	q.Set("q", query)
	q.Set("limit_per_type", fmt.Sprintf("%d", defaultSearchLimit))
	q.Set("sort", "volume24hr")
	q.Set("ascending", "false")
	q.Set("search_profiles", "false")
	q.Set("search_tags", "false")
	q.Set("events_status", "active")
	q.Set("keep_closed_markets", "0")

	var resp publicSearchResponse
	if err := c.getJSON(ctx, gammaBase+"/public-search?"+q.Encode(), &resp); err != nil {
		return publicSearchResponse{}, err
	}
	return resp, nil
}

type outcomeSearchResponse struct {
	Results []outcomeSearchEvent `json:"results"`
}

type outcomeSearchEvent struct {
	Title      string                `json:"title"`
	Slug       string                `json:"slug"`
	Closed     bool                  `json:"closed"`
	Volume24hr float64               `json:"volume24hr"`
	Volume     float64               `json:"volume"`
	Markets    []outcomeSearchMarket `json:"markets"`
}

type outcomeSearchMarket struct {
	Slug           string          `json:"slug"`
	Question       string          `json:"question"`
	GroupItemTitle string          `json:"groupItemTitle"`
	Outcomes       json.RawMessage `json:"outcomes"`
	Active         *bool           `json:"active"`
	Closed         bool            `json:"closed"`
	Archived       bool            `json:"archived"`
}

// searchMarketsByOption asks site search for live markets whose outcome or
// option title matches the query, then loads each slug from Gamma.
func (c *Client) searchMarketsByOption(ctx context.Context, query string) ([]SearchMarket, error) {
	toks := searchTokens(query)
	if len(toks) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{
		"params": map[string]any{
			"searchMode":    "query",
			"query":         query,
			"filters":       map[string]string{"status": "active"},
			"showFavorites": false,
			"optimized":     false,
		},
		"pageParam": 1,
		"locale":    "en",
	})
	if err != nil {
		return nil, err
	}
	var resp outcomeSearchResponse
	if err := c.postJSON(ctx, outcomeSearchURL, body, &resp); err != nil {
		return nil, err
	}

	type hit struct {
		ev outcomeSearchEvent
		m  outcomeSearchMarket
	}
	var matched []hit
	for _, ev := range resp.Results {
		if ev.Closed {
			continue
		}
		for _, m := range ev.Markets {
			if !optionMarketLive(m) {
				continue
			}
			outcomes := parseOutcomesField(m.Outcomes)
			if !haystackHasAll(optionHaystack(m.GroupItemTitle, outcomes), toks) {
				continue
			}
			matched = append(matched, hit{ev: ev, m: m})
			if len(matched) >= maxOutcomeHydrations {
				break
			}
		}
		if len(matched) >= maxOutcomeHydrations {
			break
		}
	}

	out := make([]SearchMarket, 0, len(matched))
	for _, h := range matched {
		slug := strings.TrimSpace(h.m.Slug)
		if slug == "" {
			continue
		}
		g, err := c.fetchGammaBySlug(ctx, slug)
		if err != nil {
			continue
		}
		sm := searchMarketFromGamma(g)
		if !isLiveMarket(sm) || strings.TrimSpace(sm.Market.ConditionID) == "" {
			continue
		}
		sm.GroupItemTitle = h.m.GroupItemTitle
		sm.EventTitle = h.ev.Title
		if sm.Market.EventSlug == "" {
			sm.Market.EventSlug = h.ev.Slug
		}
		if sm.Volume24hr == 0 {
			sm.Volume24hr = h.ev.Volume24hr
		}
		if sm.Volume == 0 {
			sm.Volume = h.ev.Volume
		}
		out = append(out, sm)
	}
	return out, nil
}

func optionMarketLive(m outcomeSearchMarket) bool {
	if m.Closed || m.Archived {
		return false
	}
	if m.Active != nil && !*m.Active {
		return false
	}
	return true
}

func optionHaystack(title string, outcomes []string) string {
	return strings.ToLower(strings.TrimSpace(title + " " + strings.Join(outcomes, " ")))
}

func parseOutcomesField(raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '[' {
		var xs []string
		if json.Unmarshal(raw, &xs) == nil {
			return xs
		}
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return parseJSONStringArray(s)
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, rawURL string, body []byte, dest any) error {
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "detector-market-holders/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: %s", rawURL, resp.Status)
	}
	if err := json.Unmarshal(buf, dest); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

func liveMarketsFromSearch(resp publicSearchResponse) []SearchMarket {
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
	return out
}

func mergeSearchMarkets(base, extra []SearchMarket) []SearchMarket {
	seen := make(map[string]struct{}, len(base))
	out := make([]SearchMarket, 0, len(base)+len(extra))
	for _, h := range base {
		cid := strings.TrimSpace(h.Market.ConditionID)
		if cid == "" {
			continue
		}
		if _, ok := seen[cid]; ok {
			continue
		}
		seen[cid] = struct{}{}
		out = append(out, h)
	}
	for _, h := range extra {
		cid := strings.TrimSpace(h.Market.ConditionID)
		if cid == "" {
			continue
		}
		if _, ok := seen[cid]; ok {
			continue
		}
		seen[cid] = struct{}{}
		out = append(out, h)
	}
	return out
}

func anyTextMatch(query string, hits []SearchMarket) bool {
	toks := searchTokens(query)
	if len(toks) == 0 {
		return false
	}
	for _, h := range hits {
		if !isLiveMarket(h) {
			continue
		}
		if matchRank(h, toks) > 0 {
			return true
		}
	}
	return false
}

func nameLikeQuery(toks []string) bool {
	for _, t := range toks {
		if len([]rune(t)) >= 6 {
			return true
		}
	}
	return false
}

// FindMarket resolves a condition id, URL, slug, or free-text name.
// Only active, unresolved markets are returned. Word queries use
// public-search, then outcome/option titles, and pick the live hit
// with the strongest volume / liquidity interest.
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
// in the same event). Then MarketInterest (24h volume, lifetime volume, liquidity).
func PickBestMarket(query string, hits []SearchMarket) (SearchMarket, bool) {
	toks := searchTokens(query)
	if len(toks) == 0 || len(hits) == 0 {
		return SearchMarket{}, false
	}

	type scored struct {
		hit  SearchMarket
		rank int
	}
	var matched []scored
	for _, h := range hits {
		if !isLiveMarket(h) || strings.TrimSpace(h.Market.ConditionID) == "" {
			continue
		}
		rank := matchRank(h, toks)
		if rank == 0 {
			continue
		}
		matched = append(matched, scored{hit: h, rank: rank})
	}
	if len(matched) == 0 {
		return SearchMarket{}, false
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].rank != matched[j].rank {
			return matched[i].rank > matched[j].rank
		}
		return preferMarket(matched[i].hit, matched[j].hit)
	})
	return matched[0].hit, true
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
// query, sorted by MarketInterest (same order as PickBestMarket).
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
	sort.SliceStable(top, func(i, j int) bool {
		return preferMarket(top[i], top[j])
	})
	return top
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
		strings.Join(h.Market.Outcomes, " "),
		h.Market.Question,
		strings.ReplaceAll(h.Market.Slug, "-", " "),
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func eventHaystack(h SearchMarket) string {
	parts := []string{
		h.EventTitle,
		strings.ReplaceAll(h.Market.EventSlug, "-", " "),
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func searchTokens(query string) []string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(query)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte(' ')
	}
	return strings.Fields(b.String())
}

func haystackHasAll(haystack string, toks []string) bool {
	words := haystackWords(haystack)
	for _, t := range toks {
		if strings.Contains(haystack, t) {
			continue
		}
		if !closeWordIn(t, words) {
			return false
		}
	}
	return true
}

func haystackWords(haystack string) []string {
	fields := strings.Fields(haystack)
	words := make([]string, 0, len(fields))
	for _, f := range fields {
		w := strings.TrimFunc(f, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
		if w != "" {
			words = append(words, w)
		}
	}
	return words
}

// closeWordIn accepts a single-character slip on a name (Mowkowitz → Moskowitz).
// Short tokens stay exact so "fl" / "25" do not drift.
func closeWordIn(tok string, words []string) bool {
	tr := []rune(tok)
	if len(tr) < 6 {
		return false
	}
	for _, w := range words {
		wr := []rune(w)
		if len(wr) == 0 || wr[0] != tr[0] {
			continue
		}
		if editDistanceAtMost(tr, wr, 1) {
			return true
		}
	}
	return false
}

func editDistanceAtMost(a, b []rune, max int) bool {
	if a == nil || b == nil {
		return false
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	if len(a)-len(b) > max {
		return false
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := cur[j-1] + 1
			sub := prev[j-1] + cost
			best := del
			if ins < best {
				best = ins
			}
			if sub < best {
				best = sub
			}
			cur[j] = best
			if best < rowMin {
				rowMin = best
			}
		}
		if rowMin > max {
			return false
		}
		prev, cur = cur, prev
	}
	return prev[len(b)] <= max
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
