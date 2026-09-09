package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	gammaBase = "https://gamma-api.polymarket.com"
	dataBase  = "https://data-api.polymarket.com"
)

// Client talks to Polymarket's public Gamma + Data APIs.
type Client struct {
	HTTP    *http.Client
	Workers int

	eventMu    sync.Mutex
	eventCache map[string]EventMeta
}

func NewClient() *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        64,
				MaxIdleConnsPerHost: 32,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		Workers:    16,
		eventCache: make(map[string]EventMeta),
	}
}

// ResolveMarket accepts a condition ID, market slug, or Polymarket URL.
func (c *Client) ResolveMarket(ctx context.Context, input string) (Market, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Market{}, fmt.Errorf("empty market input")
	}

	if isConditionID(input) {
		m, err := c.fetchMarketByCondition(ctx, input)
		if err != nil {
			// Fall back to a minimal market if Gamma lookup by condition fails.
			return Market{ConditionID: input, Outcomes: []string{"Yes", "No"}}, nil
		}
		return m, nil
	}

	slug, err := extractMarketSlug(input)
	if err != nil {
		return Market{}, err
	}
	return c.fetchMarketBySlug(ctx, slug)
}

func (c *Client) fetchMarketBySlug(ctx context.Context, slug string) (Market, error) {
	u := gammaBase + "/markets?slug=" + url.QueryEscape(slug)
	var markets []gammaMarket
	if err := c.getJSON(ctx, u, &markets); err != nil {
		return Market{}, err
	}
	if len(markets) == 0 {
		return Market{}, fmt.Errorf("no market found for slug %q", slug)
	}
	return toMarket(markets[0]), nil
}

func (c *Client) fetchMarketByCondition(ctx context.Context, conditionID string) (Market, error) {
	u := gammaBase + "/markets?condition_ids=" + url.QueryEscape(conditionID)
	var markets []gammaMarket
	if err := c.getJSON(ctx, u, &markets); err != nil {
		return Market{}, err
	}
	if len(markets) == 0 {
		return Market{}, fmt.Errorf("no market found for condition %s", conditionID)
	}
	return toMarket(markets[0]), nil
}

func toMarket(g gammaMarket) Market {
	outcomes := parseJSONStringArray(g.Outcomes)
	if len(outcomes) == 0 {
		outcomes = []string{"Yes", "No"}
	}
	m := Market{
		ConditionID: g.ConditionID,
		Slug:        g.Slug,
		Question:    g.Question,
		Outcomes:    outcomes,
	}
	if len(g.Events) > 0 {
		m.EventSlug = g.Events[0].Slug
		m.URL = "https://polymarket.com/event/" + g.Events[0].Slug + "/" + g.Slug
	} else if g.Slug != "" {
		m.URL = "https://polymarket.com/market/" + g.Slug
	}
	return m
}

// FetchHolders returns top holders grouped by outcome token.
func (c *Client) FetchHolders(ctx context.Context, conditionID string, limit int) ([]holdersResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 20 {
		limit = 20
	}
	u := fmt.Sprintf("%s/holders?market=%s&limit=%d", dataBase, url.QueryEscape(conditionID), limit)
	var out []holdersResponse
	if err := c.getJSON(ctx, u, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FetchLifetimePnL returns all-time PnL for a proxy wallet from the leaderboard API.
// ok=false means the wallet has no leaderboard entry.
func (c *Client) FetchLifetimePnL(ctx context.Context, wallet string) (pnl float64, ok bool, err error) {
	u := fmt.Sprintf(
		"%s/v1/leaderboard?user=%s&timePeriod=ALL&orderBy=PNL&limit=1&category=OVERALL",
		dataBase,
		url.QueryEscape(wallet),
	)
	var entries []leaderboardEntry
	if err := c.getJSON(ctx, u, &entries); err != nil {
		return 0, false, err
	}
	if len(entries) == 0 {
		return 0, false, nil
	}
	return entries[0].PnL, true, nil
}

// FetchLifetimePnLBatch fetches lifetime PnL for many wallets concurrently.
func (c *Client) FetchLifetimePnLBatch(ctx context.Context, wallets []string) (map[string]*float64, error) {
	out := make(map[string]*float64, len(wallets))
	if len(wallets) == 0 {
		return out, nil
	}

	workers := c.Workers
	if workers <= 0 {
		workers = 16
	}
	if workers > len(wallets) {
		workers = len(wallets)
	}

	type job struct{ wallet string }
	type res struct {
		wallet string
		pnl    *float64
		err    error
	}

	jobs := make(chan job)
	results := make(chan res)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				pnl, ok, err := c.FetchLifetimePnL(ctx, j.wallet)
				if err != nil {
					results <- res{wallet: j.wallet, err: err}
					continue
				}
				var p *float64
				if ok {
					v := pnl
					p = &v
				}
				results <- res{wallet: j.wallet, pnl: p}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	go func() {
		defer close(jobs)
		for _, w := range wallets {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{wallet: w}:
			}
		}
	}()

	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("lifetime pnl for %s: %w", r.wallet, r.err)
		}
		out[r.wallet] = r.pnl
	}
	if firstErr != nil {
		return out, firstErr
	}
	return out, nil
}

// BuildResult pulls holders + lifetime PnL into the intermediate artifact.
func (c *Client) BuildResult(ctx context.Context, marketInput string, limit int) (*Result, error) {
	market, err := c.ResolveMarket(ctx, marketInput)
	if err != nil {
		return nil, err
	}

	groups, err := c.FetchHolders(ctx, market.ConditionID, limit)
	if err != nil {
		return nil, fmt.Errorf("holders: %w", err)
	}

	yesName, noName := "Yes", "No"
	if len(market.Outcomes) > 0 {
		yesName = market.Outcomes[0]
	}
	if len(market.Outcomes) > 1 {
		noName = market.Outcomes[1]
	}

	yes := HolderSide{Outcome: yesName}
	no := HolderSide{Outcome: noName}

	unique := make(map[string]struct{})
	indexed := false
	for _, g := range groups {
		for _, h := range g.Holders {
			if h.OutcomeIndex == 0 || h.OutcomeIndex == 1 {
				indexed = true
			}
		}
	}

	for i, g := range groups {
		var side *HolderSide
		if indexed {
			// Prefer outcomeIndex from the API; set token from first holder on that side.
			for _, h := range g.Holders {
				rec := HolderRecord{
					Holder: strings.ToLower(h.ProxyWallet),
					Name:   h.Name,
					Size:   h.Amount,
				}
				unique[rec.Holder] = struct{}{}
				switch h.OutcomeIndex {
				case 1:
					side = &no
				default:
					side = &yes
				}
				if side.TokenID == "" {
					side.TokenID = g.Token
				}
				side.Holders = append(side.Holders, rec)
			}
			continue
		}

		// Fallback: first token group = Yes, second = No.
		side = &yes
		if i > 0 {
			side = &no
		}
		side.TokenID = g.Token
		for _, h := range g.Holders {
			rec := HolderRecord{
				Holder: strings.ToLower(h.ProxyWallet),
				Name:   h.Name,
				Size:   h.Amount,
			}
			unique[rec.Holder] = struct{}{}
			side.Holders = append(side.Holders, rec)
		}
	}

	wallets := make([]string, 0, len(unique))
	for w := range unique {
		wallets = append(wallets, w)
	}

	pnls, err := c.FetchLifetimePnLBatch(ctx, wallets)
	if err != nil {
		return nil, err
	}

	attach := func(side *HolderSide) {
		for i := range side.Holders {
			side.Holders[i].LifetimePnL = pnls[side.Holders[i].Holder]
		}
	}
	attach(&yes)
	attach(&no)

	return &Result{
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Market:    market,
		Yes:       yes,
		No:        no,
	}, nil
}

func (c *Client) getJSON(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "detector-market-holders/1.0")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: %s: %s", rawURL, resp.Status, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

func isConditionID(s string) bool {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return false
	}
	for _, r := range s[2:] {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !ok {
			return false
		}
	}
	return true
}

func extractMarketSlug(input string) (string, error) {
	if strings.Contains(input, "://") || strings.HasPrefix(input, "polymarket.com") {
		if !strings.Contains(input, "://") {
			input = "https://" + input
		}
		u, err := url.Parse(input)
		if err != nil {
			return "", fmt.Errorf("parse url: %w", err)
		}
		slug := path.Base(strings.TrimSuffix(u.Path, "/"))
		if slug == "" || slug == "." || slug == "event" || slug == "market" {
			return "", fmt.Errorf("could not extract market slug from url %q", input)
		}
		return slug, nil
	}
	return strings.Trim(input, "/"), nil
}

func parseJSONStringArray(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
