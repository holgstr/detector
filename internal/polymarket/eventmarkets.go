package polymarket

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// EventMarket is one live outcome market on a Gamma event, with its Yes price.
type EventMarket struct {
	SearchMarket
	YesPrice float64
}

type gammaEventWithMarkets struct {
	Slug    string             `json:"slug"`
	Title   string             `json:"title"`
	Markets []gammaEventMarket `json:"markets"`
}

type gammaEventMarket struct {
	ConditionID    string          `json:"conditionId"`
	Slug           string          `json:"slug"`
	Question       string          `json:"question"`
	Outcomes       json.RawMessage `json:"outcomes"`
	OutcomePrices  json.RawMessage `json:"outcomePrices"`
	GroupItemTitle string          `json:"groupItemTitle"`
	Volume24hr     float64         `json:"volume24hr"`
	VolumeNum      float64         `json:"volumeNum"`
	Volume         any             `json:"volume"`
	LiquidityNum   float64         `json:"liquidityNum"`
	Liquidity      any             `json:"liquidity"`
	Active         bool            `json:"active"`
	Closed         bool            `json:"closed"`
	Archived       bool            `json:"archived"`
	BestBid        *float64        `json:"bestBid"`
	BestAsk        *float64        `json:"bestAsk"`
	LastTradePrice *float64        `json:"lastTradePrice"`
}

// FetchEventMarkets loads the live markets on one event, each with a Yes price.
func (c *Client) FetchEventMarkets(ctx context.Context, eventSlug string) ([]EventMarket, error) {
	eventSlug = strings.TrimSpace(eventSlug)
	if eventSlug == "" {
		return nil, nil
	}
	u := c.gammaAPI() + "/events?slug=" + url.QueryEscape(eventSlug)
	var events []gammaEventWithMarkets
	if err := c.getJSON(ctx, u, &events); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	ev := events[0]
	out := make([]EventMarket, 0, len(ev.Markets))
	for _, g := range ev.Markets {
		if !marketIsLive(g.Active, g.Closed, g.Archived) {
			continue
		}
		cid := strings.TrimSpace(g.ConditionID)
		if cid == "" {
			continue
		}
		outcomes := parseFlexStrings(g.Outcomes)
		if len(outcomes) == 0 {
			outcomes = []string{"Yes", "No"}
		}
		prices := parseFlexFloats(g.OutcomePrices)
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
		out = append(out, EventMarket{
			SearchMarket: SearchMarket{
				Market:         mar,
				GroupItemTitle: g.GroupItemTitle,
				EventTitle:     ev.Title,
				Volume:         firstFloat(g.VolumeNum, g.Volume),
				Volume24hr:     g.Volume24hr,
				Liquidity:      firstFloat(g.LiquidityNum, g.Liquidity),
				Active:         g.Active,
				Closed:         g.Closed,
			},
			YesPrice: yesPrice(outcomes, prices, g.LastTradePrice, g.BestBid, g.BestAsk),
		})
	}
	return out, nil
}

func yesPrice(outcomes []string, prices []float64, last, bid, ask *float64) float64 {
	idx := 0
	for i, o := range outcomes {
		if strings.EqualFold(strings.TrimSpace(o), "yes") {
			idx = i
			break
		}
	}
	if idx < len(prices) {
		if p := normalizePrice(prices[idx]); p > 0 {
			return p
		}
	}
	bidV, askV := deref(bid), deref(ask)
	if bidV > 0 && askV > 0 {
		if p := normalizePrice((bidV + askV) / 2); p > 0 {
			return p
		}
	}
	for _, p := range []*float64{last, bid, ask} {
		if v := normalizePrice(deref(p)); v > 0 {
			return v
		}
	}
	return 0
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func normalizePrice(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p > 1 && p <= 100 {
		return p / 100
	}
	if p > 1 {
		return 0
	}
	return p
}

func parseFlexStrings(raw json.RawMessage) []string {
	raw = trimRaw(raw)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil
		}
		return parseJSONStringArray(s)
	}
	var out []string
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func parseFlexFloats(raw json.RawMessage) []float64 {
	raw = trimRaw(raw)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil
		}
		return parseJSONFloatArray(s)
	}
	return parseJSONFloatArray(string(raw))
}

func trimRaw(raw json.RawMessage) json.RawMessage {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return nil
	}
	return json.RawMessage(s)
}
