package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ResolveTag looks up a Gamma tag by slug (e.g. "politics", "elections").
func (c *Client) ResolveTag(ctx context.Context, slug string) (Tag, error) {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return Tag{}, fmt.Errorf("empty tag slug")
	}
	u := gammaBase + "/tags/slug/" + url.PathEscape(slug)
	var t gammaTag
	if err := c.getJSON(ctx, u, &t); err != nil {
		return Tag{}, err
	}
	if t.ID == 0 {
		return Tag{}, fmt.Errorf("tag %q not found", slug)
	}
	return Tag{ID: int(t.ID), Slug: t.Slug, Label: t.Label}, nil
}

// ListMarkets fetches markets for a tag, sorted by 24h volume descending,
// keeping only those with volume_24hr > MinVolume24.
func (c *Client) ListMarkets(ctx context.Context, opt ListMarketsOptions) (*MarketCatalog, error) {
	if opt.PageSize <= 0 {
		opt.PageSize = 100
	}
	if opt.PageSize > 500 {
		opt.PageSize = 500
	}
	if opt.MinVolume24 < 0 {
		opt.MinVolume24 = 0
	}

	var tag Tag
	var err error
	switch {
	case opt.TagID > 0:
		tag = Tag{ID: opt.TagID, Slug: opt.TagSlug}
	case opt.TagSlug != "":
		tag, err = c.ResolveTag(ctx, opt.TagSlug)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("tag slug or tag id required")
	}

	catalog := &MarketCatalog{
		FetchedAt:   time.Now().UTC().Format(time.RFC3339),
		Tag:         tag,
		MinVolume24: opt.MinVolume24,
		BinaryOnly:  opt.BinaryOnly,
		ActiveOnly:  opt.ActiveOnly,
		Markets:     make([]CatalogMarket, 0, 64),
	}

	cursor := ""
	for {
		page, next, err := c.fetchMarketsPage(ctx, tag.ID, opt, cursor)
		if err != nil {
			return nil, err
		}

		stop := false
		for _, g := range page {
			if opt.ActiveOnly && (!g.Active || g.Closed) {
				continue
			}
			vol24 := g.Volume24hr
			// Ordered by volume24hr desc; once at/below threshold, done.
			if vol24 <= opt.MinVolume24 {
				stop = true
				break
			}
			cm := toCatalogMarket(g)
			if opt.BinaryOnly && !isBinaryYesNo(cm.Outcomes) {
				continue
			}
			catalog.Markets = append(catalog.Markets, cm)
			if opt.Limit > 0 && len(catalog.Markets) >= opt.Limit {
				stop = true
				break
			}
		}

		if stop || next == "" || next == cursor {
			break
		}
		cursor = next
	}

	catalog.Count = len(catalog.Markets)
	return catalog, nil
}

func (c *Client) fetchMarketsPage(ctx context.Context, tagID int, opt ListMarketsOptions, afterCursor string) ([]gammaListMarket, string, error) {
	q := url.Values{}
	q.Set("tag_id", strconv.Itoa(tagID))
	q.Set("limit", strconv.Itoa(opt.PageSize))
	q.Set("order", "volume24hr")
	q.Set("ascending", "false")
	q.Set("closed", "false")
	if afterCursor != "" {
		q.Set("after_cursor", afterCursor)
	}

	u := gammaBase + "/markets/keyset?" + q.Encode()
	var resp marketsKeysetResponse
	if err := c.getJSON(ctx, u, &resp); err != nil {
		return nil, "", err
	}
	return resp.Markets, resp.NextCursor, nil
}

func toCatalogMarket(g gammaListMarket) CatalogMarket {
	outcomes := parseJSONStringArray(g.Outcomes)
	prices := parseJSONFloatArray(g.OutcomePrices)
	cm := CatalogMarket{
		ConditionID:   g.ConditionID,
		Slug:          g.Slug,
		Question:      g.Question,
		Outcomes:      outcomes,
		OutcomePrices: prices,
		Volume24hr:    g.Volume24hr,
		Volume:        firstFloat(g.VolumeNum, g.Volume),
		Liquidity:     firstFloat(g.LiquidityNum, g.Liquidity),
		Active:        g.Active,
		Closed:        g.Closed,
	}
	if len(g.Events) > 0 {
		cm.EventSlug = g.Events[0].Slug
		cm.EventTitle = g.Events[0].Title
		cm.URL = "https://polymarket.com/event/" + g.Events[0].Slug + "/" + g.Slug
	} else if g.Slug != "" {
		cm.URL = "https://polymarket.com/market/" + g.Slug
	}
	return cm
}

func isBinaryYesNo(outcomes []string) bool {
	if len(outcomes) != 2 {
		return false
	}
	a, b := strings.EqualFold(outcomes[0], "Yes"), strings.EqualFold(outcomes[1], "No")
	c, d := strings.EqualFold(outcomes[0], "No"), strings.EqualFold(outcomes[1], "Yes")
	return (a && b) || (c && d)
}

func parseJSONFloatArray(raw string) []float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var asFloat []float64
	if err := json.Unmarshal([]byte(raw), &asFloat); err == nil {
		return asFloat
	}
	var asString []string
	if err := json.Unmarshal([]byte(raw), &asString); err != nil {
		return nil
	}
	out := make([]float64, 0, len(asString))
	for _, s := range asString {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}

func firstFloat(num float64, raw any) float64 {
	if num != 0 {
		return num
	}
	switch v := raw.(type) {
	case float64:
		return v
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}
