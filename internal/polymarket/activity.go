package polymarket

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

const defaultActivityLimit = 100

// Activity is one Data API /activity row (trades, splits, etc.).
type Activity struct {
	ProxyWallet     string  `json:"proxyWallet"`
	Timestamp       int64   `json:"timestamp"`
	ConditionID     string  `json:"conditionId"`
	Type            string  `json:"type"`
	Size            float64 `json:"size"`
	USDCSize        float64 `json:"usdcSize"`
	TransactionHash string  `json:"transactionHash"`
	Price           float64 `json:"price"`
	Asset           string  `json:"asset"`
	Side            string  `json:"side"`
	OutcomeIndex    int     `json:"outcomeIndex"`
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	Icon            string  `json:"icon"`
	EventSlug       string  `json:"eventSlug"`
	Outcome         string  `json:"outcome"`
	Name            string  `json:"name"`
	Pseudonym       string  `json:"pseudonym"`
}

// FetchActivityOptions pages a wallet's activity feed.
type FetchActivityOptions struct {
	User   string
	Limit  int // default 100, max 500
	Offset int
	Type   string // e.g. "TRADE"; empty = all types
}

// ActivityKey uniquely identifies a fill so we can skip repeats.
func ActivityKey(a Activity) string {
	return a.TransactionHash + "|" + strings.ToLower(a.ProxyWallet) + "|" + a.Asset + "|" +
		strconv.FormatInt(a.Timestamp, 10) + "|" + strconv.FormatFloat(a.Size, 'f', -1, 64)
}

// FetchActivity returns one page of /activity for a wallet, newest first.
func (c *Client) FetchActivity(ctx context.Context, opt FetchActivityOptions) ([]Activity, error) {
	user := strings.TrimSpace(opt.User)
	if user == "" {
		return nil, fmt.Errorf("user wallet required")
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = defaultActivityLimit
	}
	if limit > 500 {
		limit = 500
	}

	q := url.Values{}
	q.Set("user", user)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("offset", strconv.Itoa(opt.Offset))
	if t := strings.TrimSpace(opt.Type); t != "" {
		q.Set("type", t)
	}

	var out []Activity
	if err := c.getJSON(ctx, dataBase+"/activity?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FetchActivityBatch fetches the same activity page for many wallets concurrently.
func (c *Client) FetchActivityBatch(ctx context.Context, users []string, opt FetchActivityOptions) ([]Activity, error) {
	if len(users) == 0 {
		return nil, nil
	}

	workers := c.Workers
	if workers <= 0 {
		workers = 16
	}
	if workers > len(users) {
		workers = len(users)
	}

	type res struct {
		acts []Activity
		err  error
	}
	jobs := make(chan string)
	results := make(chan res)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for user := range jobs {
				o := opt
				o.User = user
				acts, err := c.FetchActivity(ctx, o)
				results <- res{acts: acts, err: err}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()
	go func() {
		defer close(jobs)
		for _, user := range users {
			select {
			case <-ctx.Done():
				return
			case jobs <- user:
			}
		}
	}()

	out := make([]Activity, 0, len(users)*opt.Limit)
	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
			continue
		}
		out = append(out, r.acts...)
	}
	if firstErr != nil {
		return out, firstErr
	}
	return out, nil
}
