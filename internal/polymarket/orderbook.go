package polymarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

const OrderBookDepth = 4

// BookLevel is one price on one side of a CLOB book.
type BookLevel struct {
	Price float64
	Size  float64
}

// OutcomeBook is the inside of one outcome token's book.
type OutcomeBook struct {
	Outcome string
	TokenID string
	Tick    float64
	Bids    []BookLevel // closest bid first
	Asks    []BookLevel // closest ask first
}

type clobBookResponse struct {
	Bids     []clobLevel `json:"bids"`
	Asks     []clobLevel `json:"asks"`
	TickSize flexNumber  `json:"tick_size"`
}

type clobLevel struct {
	Price flexNumber `json:"price"`
	Size  flexNumber `json:"size"`
}

type flexNumber struct {
	V float64
}

func (f flexNumber) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.V)
}

func (f *flexNumber) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		f.V = v
		return nil
	}
	return json.Unmarshal(b, &f.V)
}

// fetchOrderBook loads the CLOB summary for one token.
func (c *Client) fetchOrderBook(ctx context.Context, tokenID string) (clobBookResponse, error) {
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return clobBookResponse{}, fmt.Errorf("empty token id")
	}
	u := c.clobAPI() + "/book?token_id=" + url.QueryEscape(tokenID)
	var book clobBookResponse
	if err := c.getJSON(ctx, u, &book); err != nil {
		return clobBookResponse{}, err
	}
	return book, nil
}

// FetchOutcomeBooks loads up to 4 closest ticks with size on each side for every outcome.
func (c *Client) FetchOutcomeBooks(ctx context.Context, conditionID string) ([]OutcomeBook, error) {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return nil, fmt.Errorf("empty condition id")
	}
	g, err := c.fetchGammaByCondition(ctx, conditionID)
	if err != nil {
		return nil, err
	}
	m := toMarket(g)
	if len(m.TokenIDs) == 0 {
		return nil, fmt.Errorf("no CLOB tokens for %s", conditionID)
	}

	out := make([]OutcomeBook, len(m.TokenIDs))
	var wg sync.WaitGroup
	errCh := make(chan error, len(m.TokenIDs))
	for i, tok := range m.TokenIDs {
		i, tok := i, tok
		outcome := ""
		if i < len(m.Outcomes) {
			outcome = m.Outcomes[i]
		}
		if outcome == "" {
			outcome = fmt.Sprintf("Outcome %d", i+1)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			book, err := c.fetchOrderBook(ctx, tok)
			if err != nil {
				errCh <- fmt.Errorf("%s: %w", outcome, err)
				return
			}
			out[i] = outcomeBookFromClob(outcome, tok, book, OrderBookDepth)
		}()
	}
	wg.Wait()
	close(errCh)
	var first error
	for e := range errCh {
		if first == nil {
			first = e
		}
	}
	if first != nil {
		return out, first
	}
	return out, nil
}

// FetchOutcomeBook loads the full CLOB for one outcome (Yes, No, or a named side).
func (c *Client) FetchOutcomeBook(ctx context.Context, conditionID, outcome string) (OutcomeBook, error) {
	conditionID = strings.TrimSpace(conditionID)
	outcome = strings.TrimSpace(outcome)
	if conditionID == "" {
		return OutcomeBook{}, fmt.Errorf("empty condition id")
	}
	if outcome == "" {
		return OutcomeBook{}, fmt.Errorf("empty outcome")
	}
	g, err := c.fetchGammaByCondition(ctx, conditionID)
	if err != nil {
		return OutcomeBook{}, err
	}
	m := toMarket(g)
	idx := -1
	for i, name := range m.Outcomes {
		if strings.EqualFold(strings.TrimSpace(name), outcome) {
			idx = i
			break
		}
	}
	if idx < 0 || idx >= len(m.TokenIDs) {
		return OutcomeBook{}, fmt.Errorf("no %s outcome", outcome)
	}
	name := strings.TrimSpace(m.Outcomes[idx])
	if name == "" {
		name = outcome
	}
	book, err := c.fetchOrderBook(ctx, m.TokenIDs[idx])
	if err != nil {
		return OutcomeBook{}, err
	}
	return outcomeBookFromClob(name, m.TokenIDs[idx], book, 0), nil
}

// FetchYesBook loads the full CLOB for the Yes token (or the first outcome).
func (c *Client) FetchYesBook(ctx context.Context, conditionID string) (OutcomeBook, error) {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return OutcomeBook{}, fmt.Errorf("empty condition id")
	}
	g, err := c.fetchGammaByCondition(ctx, conditionID)
	if err != nil {
		return OutcomeBook{}, err
	}
	m := toMarket(g)
	if len(m.TokenIDs) == 0 {
		return OutcomeBook{}, fmt.Errorf("no CLOB tokens for %s", conditionID)
	}
	idx := 0
	for i, name := range m.Outcomes {
		if strings.EqualFold(strings.TrimSpace(name), "yes") {
			idx = i
			break
		}
	}
	if idx >= len(m.TokenIDs) {
		idx = 0
	}
	outcome := "Yes"
	if idx < len(m.Outcomes) && strings.TrimSpace(m.Outcomes[idx]) != "" {
		outcome = m.Outcomes[idx]
	}
	book, err := c.fetchOrderBook(ctx, m.TokenIDs[idx])
	if err != nil {
		return OutcomeBook{}, err
	}
	return outcomeBookFromClob(outcome, m.TokenIDs[idx], book, 0), nil
}

func outcomeBookFromClob(outcome, tokenID string, book clobBookResponse, depth int) OutcomeBook {
	tick := book.TickSize.V
	if tick <= 0 {
		tick = 0.01
	}
	bids := levelsFromClob(book.Bids)
	asks := levelsFromClob(book.Asks)
	if depth > 0 {
		bids = ClosestTicks(bids, tick, depth, true)
		asks = ClosestTicks(asks, tick, depth, false)
	}
	return OutcomeBook{
		Outcome: outcome,
		TokenID: tokenID,
		Tick:    tick,
		Bids:    bids,
		Asks:    asks,
	}
}

// SizeAtOrBelow is total size at maxPrice and every cheaper tick.
func SizeAtOrBelow(levels []BookLevel, maxPrice float64) float64 {
	var n float64
	for _, lv := range levels {
		if lv.Size > 0 && lv.Price <= maxPrice+1e-12 {
			n += lv.Size
		}
	}
	return n
}

func levelsFromClob(in []clobLevel) []BookLevel {
	out := make([]BookLevel, 0, len(in))
	for _, lv := range in {
		if lv.Price.V <= 0 || lv.Size.V <= 0 {
			continue
		}
		out = append(out, BookLevel{Price: lv.Price.V, Size: lv.Size.V})
	}
	return out
}

// ClosestTicks returns up to n ticks with size from the inside of the book.
// Empty price levels are skipped. bids=true walks down from the best bid;
// otherwise walks up from the best ask, staying within n ticks of the inside.
func ClosestTicks(levels []BookLevel, tick float64, n int, bids bool) []BookLevel {
	if n <= 0 {
		return nil
	}
	if tick <= 0 {
		tick = 0.01
	}
	sizes := make(map[int64]float64, len(levels))
	var inside int64
	have := false
	for _, lv := range levels {
		if lv.Price <= 0 || lv.Size <= 0 {
			continue
		}
		k := int64(math.Round(lv.Price / tick))
		if k <= 0 {
			continue
		}
		sizes[k] += lv.Size
		if !have {
			inside, have = k, true
			continue
		}
		if bids {
			if k > inside {
				inside = k
			}
		} else if k < inside {
			inside = k
		}
	}
	if !have {
		return nil
	}
	out := make([]BookLevel, 0, n)
	for i := 0; i < n; i++ {
		var k int64
		if bids {
			k = inside - int64(i)
		} else {
			k = inside + int64(i)
		}
		if k <= 0 {
			break
		}
		p := float64(k) * tick
		if p >= 1 {
			break
		}
		if sz := sizes[k]; sz > 0 {
			out = append(out, BookLevel{Price: p, Size: sz})
		}
	}
	return out
}
