package polymarket

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultActivityLimit = 100
	maxActivityPages     = 20
)

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
	Limit  int    // default 100, max 1000
	Cursor string // pagination.next_cursor from the previous page
	Type   string // e.g. "TRADE"; empty = all types
	Start  int64  // epoch seconds, inclusive lower bound (0 = API default window)
	// NextCursor, when non-nil, receives the following page token (empty on the last page).
	NextCursor *string
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
	if limit > 1000 {
		limit = 1000
	}

	q := url.Values{}
	q.Set("user", user)
	q.Set("limit", strconv.Itoa(limit))
	if cursor := strings.TrimSpace(opt.Cursor); cursor != "" {
		q.Set("cursor", cursor)
	}
	if t := strings.TrimSpace(opt.Type); t != "" {
		q.Set("type", t)
	}
	if opt.Start > 0 {
		q.Set("start", strconv.FormatInt(opt.Start, 10))
	}

	var page struct {
		Data       []activityWire `json:"data"`
		Pagination pagination     `json:"pagination"`
	}
	if err := c.getJSON(ctx, c.dataAPI()+"/v2/activity?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	if opt.NextCursor != nil {
		*opt.NextCursor = page.Pagination.next()
	}
	out := make([]Activity, 0, len(page.Data))
	for _, row := range page.Data {
		out = append(out, row.activity())
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

// FetchActivitySince pages TRADE (or Type) rows at or after since, newest first.
// truncated is true if the page budget ran out before the window started.
func (c *Client) FetchActivitySince(ctx context.Context, user string, since int64, typ string) ([]Activity, bool, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil, false, fmt.Errorf("user wallet required")
	}
	typ = strings.TrimSpace(typ)
	if typ == "" {
		typ = "TRADE"
	}

	out := make([]Activity, 0, defaultActivityLimit)
	cursor := ""
	for page := 0; page < maxActivityPages; page++ {
		var next string
		acts, err := c.FetchActivity(ctx, FetchActivityOptions{
			User:       user,
			Limit:      500,
			Cursor:     cursor,
			Type:       typ,
			Start:      since,
			NextCursor: &next,
		})
		if err != nil {
			return out, false, err
		}
		if len(acts) == 0 {
			return out, false, nil
		}
		for _, a := range acts {
			if since > 0 && a.Timestamp < since {
				continue
			}
			out = append(out, a)
		}
		last := acts[len(acts)-1].Timestamp
		// Activity is newest-first. A row older than the window means the walk is done.
		if next == "" || (since > 0 && last < since) {
			return out, false, nil
		}
		cursor = next
	}
	return out, true, nil
}

// FetchActivitySinceBatch fetches each wallet's activity since the timestamp.
func (c *Client) FetchActivitySinceBatch(ctx context.Context, users []string, since int64, typ string) ([]Activity, bool, error) {
	if len(users) == 0 {
		return nil, false, nil
	}

	workers := c.Workers
	if workers <= 0 {
		workers = 16
	}
	if workers > len(users) {
		workers = len(users)
	}

	type res struct {
		acts      []Activity
		truncated bool
		err       error
	}
	jobs := make(chan string)
	results := make(chan res)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for user := range jobs {
				acts, trunc, err := c.FetchActivitySince(ctx, user, since, typ)
				results <- res{acts: acts, truncated: trunc, err: err}
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

	out := make([]Activity, 0)
	var firstErr error
	truncated := false
	for r := range results {
		if r.truncated {
			truncated = true
		}
		if r.err != nil && firstErr == nil {
			firstErr = r.err
			continue
		}
		out = append(out, r.acts...)
	}
	if firstErr != nil {
		return out, truncated, firstErr
	}
	return out, truncated, nil
}
