package kalshi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	tradeBase  = "https://external-api.kalshi.com/trade-api/v2"
	searchBase = "https://api.elections.kalshi.com/v1"
)

// ErrNotFound is a missing market or event ticker.
var ErrNotFound = errors.New("kalshi: not found")

// Client talks to Kalshi's public market-data API.
type Client struct {
	HTTP   *http.Client
	Trade  string
	Search string
}

func NewClient() *Client {
	return &Client{
		HTTP:   &http.Client{Timeout: 20 * time.Second},
		Trade:  tradeBase,
		Search: searchBase,
	}
}

// Market is one Kalshi binary contract (one outcome inside an event).
type Market struct {
	Ticker      string
	EventTicker string
	Event       string
	Name        string
	Tick        float64
	LastPrice   float64
	Volume      float64
	Resolved    bool
}

// Level is one price on one side of a book.
type Level struct {
	Price float64
	Size  float64
}

// Book is one ticker's Yes order book. Bids are highest-first, asks lowest-first.
// Asks are the complement of No bids (a No bid at P is a Yes ask at 1-P).
type Book struct {
	Bids []Level
	Asks []Level
}

// TextSearch returns open markets whose event or outcome matches query.
func (c *Client) TextSearch(ctx context.Context, query string) ([]Market, error) {
	q := url.Values{}
	q.Set("query", query)
	q.Set("order_by", "querymatch")
	q.Set("page_size", "50")
	q.Set("fuzzy_threshold", "4")
	q.Set("status", "open")
	var raw searchMsg
	if err := c.get(ctx, c.searchBase()+"/search/series?"+q.Encode(), &raw); err != nil {
		return nil, err
	}
	var out []Market
	for _, ev := range raw.CurrentPage {
		out = append(out, marketsFromSearch(ev)...)
	}
	return out, nil
}

// Market loads one contract by ticker.
func (c *Client) Market(ctx context.Context, ticker string) (Market, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" {
		return Market{}, fmt.Errorf("empty ticker")
	}
	var raw struct {
		Market rawMarket `json:"market"`
	}
	if err := c.get(ctx, c.tradeBase()+"/markets/"+url.PathEscape(ticker), &raw); err != nil {
		return Market{}, err
	}
	m := marketFrom(raw.Market, "", "")
	if m.Ticker == "" {
		return Market{}, ErrNotFound
	}
	return m, nil
}

// MarketsByEvent loads every contract in an event.
func (c *Client) MarketsByEvent(ctx context.Context, event string) ([]Market, error) {
	event = strings.ToUpper(strings.TrimSpace(event))
	if event == "" {
		return nil, fmt.Errorf("empty event")
	}
	var raw struct {
		Event struct {
			EventTicker string      `json:"event_ticker"`
			Title       string      `json:"title"`
			Markets     []rawMarket `json:"markets"`
		} `json:"event"`
	}
	path := c.tradeBase() + "/events/" + url.PathEscape(event) + "?with_nested_markets=true"
	if err := c.get(ctx, path, &raw); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(raw.Event.Title)
	code := strings.TrimSpace(raw.Event.EventTicker)
	if code == "" {
		code = event
	}
	out := make([]Market, 0, len(raw.Event.Markets))
	for _, m := range raw.Event.Markets {
		out = append(out, marketFrom(m, code, title))
	}
	return out, nil
}

// Books loads Yes books for up to 100 tickers.
func (c *Client) Books(ctx context.Context, tickers []string) (map[string]Book, error) {
	if len(tickers) == 0 {
		return nil, fmt.Errorf("no tickers")
	}
	if len(tickers) > 100 {
		tickers = tickers[:100]
	}
	q := url.Values{}
	for _, t := range tickers {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		q.Add("tickers", t)
	}
	var raw struct {
		Orderbooks []struct {
			Ticker string `json:"ticker"`
			FP     struct {
				Yes [][]string `json:"yes_dollars"`
				No  [][]string `json:"no_dollars"`
			} `json:"orderbook_fp"`
		} `json:"orderbooks"`
	}
	if err := c.get(ctx, c.tradeBase()+"/markets/orderbooks?"+q.Encode(), &raw); err != nil {
		return nil, err
	}
	out := make(map[string]Book, len(raw.Orderbooks))
	for _, ob := range raw.Orderbooks {
		if ob.Ticker == "" {
			continue
		}
		out[ob.Ticker] = bookFrom(levelsFrom(ob.FP.Yes), levelsFrom(ob.FP.No))
	}
	return out, nil
}

type searchMsg struct {
	CurrentPage []searchEvent `json:"current_page"`
}

type searchEvent struct {
	EventTicker string      `json:"event_ticker"`
	EventTitle  string      `json:"event_title"`
	Markets     []rawMarket `json:"markets"`
}

type rawMarket struct {
	Ticker           string      `json:"ticker"`
	EventTicker      string      `json:"event_ticker"`
	Title            string      `json:"title"`
	YesSubTitle      string      `json:"yes_sub_title"`
	YesSubtitle      string      `json:"yes_subtitle"`
	Subtitle         string      `json:"subtitle"`
	Status           string      `json:"status"`
	Result           string      `json:"result"`
	LastPriceDollars string      `json:"last_price_dollars"`
	VolumeFP         string      `json:"volume_fp"`
	Volume           flexNum     `json:"volume"`
	PriceRanges      []priceStep `json:"price_ranges"`
}

type priceStep struct {
	Step string `json:"step"`
}

type flexNum float64

func (f *flexNum) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexNum(parseDec(s))
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = flexNum(v)
	return nil
}

func marketsFromSearch(ev searchEvent) []Market {
	title := strings.TrimSpace(ev.EventTitle)
	code := strings.TrimSpace(ev.EventTicker)
	out := make([]Market, 0, len(ev.Markets))
	for _, m := range ev.Markets {
		got := marketFrom(m, code, title)
		if got.Ticker == "" || got.Resolved {
			continue
		}
		out = append(out, got)
	}
	return out
}

func marketFrom(m rawMarket, eventTicker, eventTitle string) Market {
	ticker := strings.TrimSpace(m.Ticker)
	name := strings.TrimSpace(m.YesSubTitle)
	if name == "" {
		name = strings.TrimSpace(m.YesSubtitle)
	}
	if name == "" {
		name = strings.TrimSpace(m.Subtitle)
	}
	if name == "" {
		name = "Yes"
	}
	code := strings.TrimSpace(m.EventTicker)
	if code == "" {
		code = eventTicker
	}
	if strings.TrimSpace(eventTitle) == "" {
		eventTitle = strings.TrimSpace(m.Title)
	}
	vol := parseDec(m.VolumeFP)
	if vol == 0 {
		vol = float64(m.Volume)
	}
	return Market{
		Ticker:      ticker,
		EventTicker: code,
		Event:       eventTitle,
		Name:        name,
		Tick:        tickFrom(m.PriceRanges),
		LastPrice:   parseDec(m.LastPriceDollars),
		Volume:      vol,
		Resolved:    resolvedStatus(m.Status, m.Result),
	}
}

func resolvedStatus(status, result string) bool {
	if strings.TrimSpace(result) != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "closed", "settled", "determined", "finalized", "inactive":
		return true
	default:
		return false
	}
}

func tickFrom(ranges []priceStep) float64 {
	tick := 0.0
	for _, r := range ranges {
		step := parseDec(r.Step)
		if step <= 0 {
			continue
		}
		if tick == 0 || step < tick {
			tick = step
		}
	}
	if tick <= 0 {
		return 0.01
	}
	return tick
}

func levelsFrom(raw [][]string) []Level {
	out := make([]Level, 0, len(raw))
	for _, row := range raw {
		if len(row) < 2 {
			continue
		}
		price, size := parseDec(row[0]), parseDec(row[1])
		if price <= 0 || size <= 0 {
			continue
		}
		out = append(out, Level{Price: price, Size: size})
	}
	return out
}

func bookFrom(yes, no []Level) Book {
	sortDesc(yes)
	sortDesc(no)
	asks := make([]Level, 0, len(no))
	for _, lv := range no {
		price := math.Round((1-lv.Price)*10000) / 10000
		if price <= 0 || price >= 1 || lv.Size <= 0 {
			continue
		}
		asks = append(asks, Level{Price: price, Size: lv.Size})
	}
	return Book{Bids: yes, Asks: asks}
}

func sortDesc(levels []Level) {
	for i := 1; i < len(levels); i++ {
		j := i
		for j > 0 && levels[j].Price > levels[j-1].Price {
			levels[j], levels[j-1] = levels[j-1], levels[j]
			j--
		}
	}
}

func parseDec(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func (c *Client) get(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "detector-activity-bot/1.0")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("kalshi %s %s: %s", req.Method, req.URL.Path, truncate(string(b), 200))
	}
	if dest == nil || len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, dest); err != nil {
		return fmt.Errorf("kalshi decode: %w", err)
	}
	return nil
}

func (c *Client) tradeBase() string {
	if strings.TrimSpace(c.Trade) != "" {
		return strings.TrimRight(c.Trade, "/")
	}
	return tradeBase
}

func (c *Client) searchBase() string {
	if strings.TrimSpace(c.Search) != "" {
		return strings.TrimRight(c.Search, "/")
	}
	return searchBase
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
