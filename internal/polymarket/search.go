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
				Active:         g.Active,
				Closed:         g.Closed,
			})
		}
	}
	return out, nil
}

// FindMarket resolves a condition id, URL, slug, or free-text name.
// Only active, unresolved markets are returned. Word queries use
// public-search and pick the highest-volume live hit whose market
// (or event) title contains the words.
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
// in the same event). Then 24h volume, then total.
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
		if matched[i].hit.Volume24hr != matched[j].hit.Volume24hr {
			return matched[i].hit.Volume24hr > matched[j].hit.Volume24hr
		}
		if matched[i].hit.Volume != matched[j].hit.Volume {
			return matched[i].hit.Volume > matched[j].hit.Volume
		}
		return matched[i].hit.Market.Question < matched[j].hit.Market.Question
	})
	return matched[0].hit, true
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
