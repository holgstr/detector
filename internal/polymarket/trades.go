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
	maxTradeOffset   = 10000
	maxTradePages    = 40
)

// Trade is one Data API fill. With takerOnly=true (the API default),
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

// FetchTrades pages the Data API /trades endpoint, sliding the start/end
// window when offset would pass the API's 10k cap.
func (c *Client) FetchTrades(ctx context.Context, opt FetchTradesOptions) ([]Trade, bool, error) {
	if strings.TrimSpace(opt.Market) == "" {
		return nil, false, fmt.Errorf("market condition id required")
	}
	pageSize := opt.PageSize
	if pageSize <= 0 {
		pageSize = defaultTradePage
	}
	if pageSize > defaultTradePage {
		pageSize = defaultTradePage
	}

	out := make([]Trade, 0, pageSize)
	seen := make(map[string]struct{}, pageSize)
	end := opt.End
	offset := 0

	for page := 0; page < maxTradePages; page++ {
		q := url.Values{}
		q.Set("market", opt.Market)
		if opt.IncludeMakers {
			q.Set("takerOnly", "false")
		} else {
			q.Set("takerOnly", "true")
		}
		q.Set("limit", strconv.Itoa(pageSize))
		q.Set("offset", strconv.Itoa(offset))
		if opt.Start > 0 {
			q.Set("start", strconv.FormatInt(opt.Start, 10))
		}
		if end > 0 {
			q.Set("end", strconv.FormatInt(end, 10))
		}

		var pageTrades []Trade
		if err := c.getJSON(ctx, dataBase+"/trades?"+q.Encode(), &pageTrades); err != nil {
			return out, false, err
		}
		if len(pageTrades) == 0 {
			return out, false, nil
		}

		added := 0
		for _, t := range pageTrades {
			if opt.Start > 0 && t.Timestamp < opt.Start {
				continue
			}
			id := tradeKey(t)
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, t)
			added++
		}

		last := pageTrades[len(pageTrades)-1].Timestamp
		if len(pageTrades) < pageSize || (opt.Start > 0 && last < opt.Start) {
			return out, false, nil
		}

		if added == 0 {
			// Window slide landed on an already-seen second; nudge back 1s.
			if last <= 1 || (opt.Start > 0 && last-1 < opt.Start) {
				return out, false, nil
			}
			end = last - 1
			offset = 0
			continue
		}

		offset += pageSize
		if offset+pageSize > maxTradeOffset {
			if opt.Start > 0 && last <= opt.Start {
				return out, false, nil
			}
			end = last
			offset = 0
		}
	}
	return out, true, nil
}

func tradeKey(t Trade) string {
	return t.TransactionHash + "|" + strings.ToLower(t.ProxyWallet) + "|" + t.Asset + "|" +
		strconv.FormatInt(t.Timestamp, 10) + "|" + strconv.FormatFloat(t.Size, 'f', -1, 64)
}
