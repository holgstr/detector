package pascal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const dataBase = "https://data.pascal.trade"

// Client talks to Pascal's public read API.
type Client struct {
	HTTP *http.Client
	Base string
}

func NewClient() *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 20 * time.Second},
		Base: dataBase,
	}
}

// Market is one live (or resolved) Pascal outcome contract.
type Market struct {
	Symbol    string
	EventCode string
	Event     string
	Name      string
	TickMin   float64
	MarkPrice float64
	Volume24h float64
	Resolved  bool
}

// Level is one price on one side of a book.
type Level struct {
	Price float64
	Size  float64
}

// Book is one symbol's order book. Bids are highest-first, asks lowest-first.
type Book struct {
	Bids []Level
	Asks []Level
}

type marketMsg struct {
	Symbol     string          `json:"symbol"`
	TickMin    string          `json:"tick_size_min"`
	MarkPrice  string          `json:"mark_price"`
	Resolution json.RawMessage `json:"resolution"`
	Display    struct {
		Event string `json:"event_description"`
		Name  string `json:"market_description"`
	} `json:"display_attributes"`
	Stats struct {
		Last24h struct {
			Buy  string `json:"taker_buy_volume"`
			Sell string `json:"taker_sell_volume"`
		} `json:"last_24h"`
	} `json:"stats"`
}

type booksMsg struct {
	Books map[string]struct {
		Bids [][]flexNum `json:"bids"`
		Asks [][]flexNum `json:"asks"`
	} `json:"books"`
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
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		*f = flexNum(v)
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = flexNum(v)
	return nil
}

// TextSearch returns live markets whose symbol, event, name, or tag contains query.
func (c *Client) TextSearch(ctx context.Context, query string) ([]Market, error) {
	body := map[string]any{
		"query":    query,
		"status":   "live",
		"order_by": "volume_24h",
		"limit":    50,
	}
	var raw []marketMsg
	if err := c.post(ctx, "/api/v1/markets/text-search", body, &raw); err != nil {
		return nil, err
	}
	return marketsFrom(raw), nil
}

// MarketsBySymbols loads markets by exact symbol (includes resolved).
func (c *Client) MarketsBySymbols(ctx context.Context, symbols []string) ([]Market, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	var raw []marketMsg
	if err := c.post(ctx, "/api/v1/markets/search", map[string]any{"symbols": symbols}, &raw); err != nil {
		return nil, err
	}
	return marketsFrom(raw), nil
}

// MarketsByEvent loads every market in an event (includes resolved).
func (c *Client) MarketsByEvent(ctx context.Context, event string) ([]Market, error) {
	event = strings.TrimSpace(event)
	if event == "" {
		return nil, fmt.Errorf("empty event")
	}
	var raw []marketMsg
	if err := c.post(ctx, "/api/v1/markets/search", map[string]any{"events": []string{event}}, &raw); err != nil {
		return nil, err
	}
	return marketsFrom(raw), nil
}

// Books loads order books for up to 50 symbols.
func (c *Client) Books(ctx context.Context, symbols []string) (map[string]Book, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("no symbols")
	}
	if len(symbols) > 50 {
		symbols = symbols[:50]
	}
	q := url.Values{}
	q.Set("symbols", strings.Join(symbols, ","))
	var raw booksMsg
	if err := c.get(ctx, "/api/v1/books?"+q.Encode(), &raw); err != nil {
		return nil, err
	}
	out := make(map[string]Book, len(raw.Books))
	for sym, b := range raw.Books {
		out[sym] = Book{Bids: levelsFrom(b.Bids), Asks: levelsFrom(b.Asks)}
	}
	return out, nil
}

func marketsFrom(raw []marketMsg) []Market {
	out := make([]Market, 0, len(raw))
	for _, m := range raw {
		sym := strings.TrimSpace(m.Symbol)
		if sym == "" {
			continue
		}
		out = append(out, Market{
			Symbol:    sym,
			EventCode: eventCode(sym),
			Event:     strings.TrimSpace(m.Display.Event),
			Name:      strings.TrimSpace(m.Display.Name),
			TickMin:   parseDec(m.TickMin),
			MarkPrice: parseDec(m.MarkPrice),
			Volume24h: parseDec(m.Stats.Last24h.Buy) + parseDec(m.Stats.Last24h.Sell),
			Resolved:  resolved(m.Resolution),
		})
	}
	return out
}

func resolved(raw json.RawMessage) bool {
	s := bytes.TrimSpace(raw)
	return len(s) > 0 && string(s) != "null"
}

func levelsFrom(raw [][]flexNum) []Level {
	out := make([]Level, 0, len(raw))
	for _, row := range raw {
		if len(row) < 2 {
			continue
		}
		price, size := float64(row[0]), float64(row[1])
		if price <= 0 || size <= 0 {
			continue
		}
		out = append(out, Level{Price: price, Size: size})
	}
	return out
}

func eventCode(symbol string) string {
	if i := strings.IndexByte(symbol, '.'); i > 0 {
		return symbol[:i]
	}
	return symbol
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

type envelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
}

func (c *Client) post(ctx context.Context, path string, body any, dest any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "detector-activity-bot/1.0")
	return c.do(req, dest)
}

func (c *Client) get(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "detector-activity-bot/1.0")
	return c.do(req, dest)
}

func (c *Client) do(req *http.Request, dest any) error {
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pascal %s %s: %s", req.Method, req.URL.Path, truncate(string(b), 200))
	}
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return fmt.Errorf("pascal decode: %w", err)
	}
	if env.Status != "" && env.Status != "success" {
		return fmt.Errorf("pascal %s", env.Status)
	}
	if dest == nil || len(bytes.TrimSpace(env.Data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, dest); err != nil {
		return fmt.Errorf("pascal decode data: %w", err)
	}
	return nil
}

func (c *Client) base() string {
	if strings.TrimSpace(c.Base) != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return dataBase
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
