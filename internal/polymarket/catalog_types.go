package polymarket

import (
	"encoding/json"
	"strconv"
)

// Tag is a Gamma category tag (Politics, Elections, …).
type Tag struct {
	ID    int    `json:"id"`
	Slug  string `json:"slug"`
	Label string `json:"label"`
}

// CatalogMarket is one row in the markets catalog intermediate.
type CatalogMarket struct {
	ConditionID   string    `json:"condition_id"`
	Slug          string    `json:"slug"`
	Question      string    `json:"question"`
	Outcomes      []string  `json:"outcomes"`
	OutcomePrices []float64 `json:"outcome_prices,omitempty"`
	EventSlug     string    `json:"event_slug,omitempty"`
	EventTitle    string    `json:"event_title,omitempty"`
	URL           string    `json:"url,omitempty"`
	Volume24hr    float64   `json:"volume_24hr"`
	Volume        float64   `json:"volume,omitempty"`
	Liquidity     float64   `json:"liquidity,omitempty"`
	Active        bool      `json:"active"`
	Closed        bool      `json:"closed"`
}

// MarketCatalog is the intermediate artifact for dashboard pipelines.
type MarketCatalog struct {
	FetchedAt   string          `json:"fetched_at"`
	Tag         Tag             `json:"tag"`
	MinVolume24 float64         `json:"min_volume_24hr"`
	BinaryOnly  bool            `json:"binary_only"`
	ActiveOnly  bool            `json:"active_only"`
	Count       int             `json:"count"`
	Markets     []CatalogMarket `json:"markets"`
}

// ListMarketsOptions controls catalog discovery.
type ListMarketsOptions struct {
	TagSlug     string  // e.g. "politics", "elections"
	TagID       int     // if set, skips slug lookup
	MinVolume24 float64 // exclusive minimum 24h volume filter
	BinaryOnly  bool    // keep only Yes/No (or 2-outcome) markets
	ActiveOnly  bool    // active=true & closed=false
	Limit       int     // max markets to return (0 = no cap besides volume filter)
	PageSize    int     // API page size (max 500)
}

type gammaTag struct {
	ID    flexInt `json:"id"`
	Slug  string  `json:"slug"`
	Label string  `json:"label"`
}

// flexInt accepts JSON numbers or numeric strings (Gamma tag ids vary).
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*f = flexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}

type gammaListMarket struct {
	ConditionID   string  `json:"conditionId"`
	Slug          string  `json:"slug"`
	Question      string  `json:"question"`
	Outcomes      string  `json:"outcomes"`
	OutcomePrices string  `json:"outcomePrices"`
	Volume24hr    float64 `json:"volume24hr"`
	VolumeNum     float64 `json:"volumeNum"`
	Volume        any     `json:"volume"`
	LiquidityNum  float64 `json:"liquidityNum"`
	Liquidity     any     `json:"liquidity"`
	Active        bool    `json:"active"`
	Closed        bool    `json:"closed"`
	Events        []struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
	} `json:"events"`
}

type marketsKeysetResponse struct {
	Markets    []gammaListMarket `json:"markets"`
	NextCursor string            `json:"next_cursor"`
}
