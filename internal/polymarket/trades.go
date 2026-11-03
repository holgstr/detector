package polymarket

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultTradePage = 500
	maxTradePages    = 40
)

// Trade is one Data API fill. With taker_only=true (the API default),
// proxyWallet is the aggressor who hit the book.
type Trade struct {
	ProxyWallet     string  `json:"proxyWallet"`
	Side            string  `json:"side"`
	Asset           string  `json:"asset"`
	ConditionID     string  `json:"conditionId"`
	Size            float64 `json:"size"`
	Price           float64 `json:"price"`
	Timestamp       int64   `json:"timestamp"`
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	Icon            string  `json:"icon"`
	EventSlug       string  `json:"eventSlug"`
	Outcome         string  `json:"outcome"`
	OutcomeIndex    int     `json:"outcomeIndex"`
	Name            string  `json:"name"`
	Pseudonym       string  `json:"pseudonym"`
	TransactionHash string  `json:"transactionHash"`
}

// FetchTradesOptions pages fills for a market in [Start, End].
type FetchTradesOptions struct {
	Market        string // condition ID
	Start         int64  // epoch seconds, inclusive lower bound
	End           int64  // epoch seconds; rows newer than End are excluded (0 = now)
	IncludeMakers bool   // if false (default), only taker/aggressor fills
	PageSize      int    // default 500
}

// FetchTakerTrades returns aggressor fills for a market in [start, end].
// truncated is true if the page budget was exhausted before the window.
func (c *Client) FetchTakerTrades(ctx context.Context, market string, start, end int64) ([]Trade, bool, error) {
	return c.FetchTrades(ctx, FetchTradesOptions{
		Market: market,
		Start:  start,
		End:    end,
	})
}

// FetchTrades pages Data API /v2/trades with a cursor.
// start/end are honored by the API only for a user-anchored feed. This call
// is condition-anchored, so rows outside [Start, End] are dropped locally.
// The feed is newest-first.
func (c *Client) FetchTrades(ctx context.Context, opt FetchTradesOptions) ([]Trade, bool, error) {
	if strings.TrimSpace(opt.Market) == "" {
		return nil, false, fmt.Errorf("market condition id required")
	}
	pageSize := opt.PageSize
	if pageSize <= 0 {
		pageSize = defaultTradePage
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	out := make([]Trade, 0, pageSize)
	seen := make(map[string]struct{}, pageSize)
	cursor := ""

	for page := 0; page < maxTradePages; page++ {
		q := url.Values{}
		q.Set("condition", opt.Market)
		if opt.IncludeMakers {
			q.Set("taker_only", "false")
		} else {
			q.Set("taker_only", "true")
		}
		q.Set("limit", strconv.Itoa(pageSize))
		if cursor != "" {
			q.Set("cursor", cursor)
		}

		var body struct {
			Data       []tradeWire `json:"data"`
			Pagination pagination  `json:"pagination"`
		}
		if err := c.getJSON(ctx, c.dataAPI()+"/v2/trades?"+q.Encode(), &body); err != nil {
			return out, false, err
		}
		if len(body.Data) == 0 {
			return out, false, nil
		}

		passedStart := false
		for _, row := range body.Data {
			t := row.trade()
			if opt.End > 0 && t.Timestamp > opt.End {
				continue
			}
			if opt.Start > 0 && t.Timestamp < opt.Start {
				passedStart = true
				continue
			}
			id := tradeKey(t)
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, t)
		}

		next := body.Pagination.next()
		last := body.Data[len(body.Data)-1].Timestamp
		if next == "" || passedStart || (opt.Start > 0 && last < opt.Start) {
			return out, false, nil
		}
		cursor = next
	}
	return out, true, nil
}

func tradeKey(t Trade) string {
	return t.TransactionHash + "|" + strings.ToLower(t.ProxyWallet) + "|" + t.Asset + "|" +
		strconv.FormatInt(t.Timestamp, 10) + "|" + strconv.FormatFloat(t.Size, 'f', -1, 64)
}
