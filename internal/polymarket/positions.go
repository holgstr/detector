package polymarket

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

const defaultPositionLimit = 50

// Position is one Data API /positions row (shares held of one outcome).
type Position struct {
	ProxyWallet string  `json:"proxyWallet"`
	Asset       string  `json:"asset"`
	ConditionID string  `json:"conditionId"`
	Size        float64 `json:"size"`
	AvgPrice    float64 `json:"avgPrice"`
	CurPrice    float64 `json:"curPrice"`
	Title       string  `json:"title"`
	Slug        string  `json:"slug"`
	EventSlug   string  `json:"eventSlug"`
	Outcome     string  `json:"outcome"`
}

// FetchPositionsOptions filters a wallet's open positions.
type FetchPositionsOptions struct {
	User          string
	Market        string // condition ID; empty = all markets
	Limit         int    // default 50, max 1000
	Cursor        string // pagination.next_cursor from the previous page
	SortBy        string // CURRENT (mapped to CURRENT_VALUE) or a v2 sort; empty = API default
	SortDirection string // ASC or DESC
	// NextCursor, when non-nil, receives the following page token (empty on the last page).
	NextCursor *string
}

// FetchPositions returns one page of open positions for a wallet from /v2/positions.
func (c *Client) FetchPositions(ctx context.Context, opt FetchPositionsOptions) ([]Position, error) {
	user := strings.TrimSpace(opt.User)
	if user == "" {
		return nil, fmt.Errorf("user wallet required")
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = defaultPositionLimit
	}
	if limit > 1000 {
		limit = 1000
	}

	q := url.Values{}
	q.Set("user", user)
	q.Set("limit", strconv.Itoa(limit))
	// v1 sizeThreshold=0. v2's default floor is 0.1 shares; 0 keeps dust rows.
	q.Set("filter_type", "TOKENS")
	q.Set("filter_amount", "0")
	if cursor := strings.TrimSpace(opt.Cursor); cursor != "" {
		q.Set("cursor", cursor)
	}
	if m := strings.TrimSpace(opt.Market); m != "" {
		q.Set("condition", m)
	}
	if s := v2PositionSort(opt.SortBy); s != "" {
		q.Set("sort_by", s)
	}
	if s := strings.TrimSpace(opt.SortDirection); s != "" {
		q.Set("sort_direction", s)
	}

	var page struct {
		Data       []positionWire `json:"data"`
		Pagination pagination     `json:"pagination"`
	}
	if err := c.getJSON(ctx, c.dataAPI()+"/v2/positions?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	if opt.NextCursor != nil {
		*opt.NextCursor = page.Pagination.next()
	}
	out := make([]Position, 0, len(page.Data))
	for _, row := range page.Data {
		out = append(out, row.position())
	}
	return out, nil
}

// FetchPositionsBatch fetches the same market (or all markets) for many wallets.
func (c *Client) FetchPositionsBatch(ctx context.Context, users []string, opt FetchPositionsOptions) (map[string][]Position, error) {
	out := make(map[string][]Position, len(users))
	if len(users) == 0 {
		return out, nil
	}

	workers := c.Workers
	if workers <= 0 {
		workers = 16
	}
	if workers > len(users) {
		workers = len(users)
	}

	type res struct {
		user string
		pos  []Position
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
				pos, err := c.FetchPositions(ctx, o)
				results <- res{user: strings.ToLower(user), pos: pos, err: err}
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

	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
			continue
		}
		out[r.user] = r.pos
	}
	return out, firstErr
}

// NetPosition is YES minus NO shares (or a single non-binary outcome).
type NetPosition struct {
	Size    float64
	Outcome string
	Known   bool
}

// NetShares collapses Yes/No legs into one signed net, matching the
// Activity tab: long Yes if yesSize > noSize, else long No.
func NetShares(positions []Position) NetPosition {
	var yes, no float64
	otherSize := 0.0
	otherOut := ""
	for _, p := range positions {
		switch parseOutcomeSide(p.Outcome) {
		case "yes":
			yes += p.Size
		case "no":
			no += p.Size
		default:
			if p.Size > otherSize {
				otherSize = p.Size
				otherOut = strings.TrimSpace(p.Outcome)
			}
		}
	}
	net := yes - no
	if net > 0 {
		return NetPosition{Size: net, Outcome: "YES", Known: true}
	}
	if net < 0 {
		return NetPosition{Size: -net, Outcome: "NO", Known: true}
	}
	if otherSize > 0 && otherOut != "" {
		return NetPosition{Size: otherSize, Outcome: strings.ToUpper(otherOut), Known: true}
	}
	return NetPosition{Known: true}
}

func parseOutcomeSide(outcome string) string {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "yes":
		return "yes"
	case "no":
		return "no"
	default:
		return ""
	}
}
