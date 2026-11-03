package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFetchTradesRequiresMarket(t *testing.T) {
	c := NewClient()
	_, _, err := c.FetchTrades(context.Background(), FetchTradesOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchTradesPagesUntilShort(t *testing.T) {
	var calls []url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		calls = append(calls, q)
		cursor := q.Get("cursor")
		var page []tradeWire
		var next *string
		if cursor == "" {
			page = []tradeWire{
				{ProxyWallet: "0xaaa", Side: "BUY", TokenID: "1", Size: 10, Price: 0.5, Timestamp: 200, TransactionHash: "h1"},
				{ProxyWallet: "0xbbb", Side: "BUY", TokenID: "1", Size: 8, Price: 0.5, Timestamp: 150, TransactionHash: "h2"},
			}
			n := "page-2"
			next = &n
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":       page,
			"pagination": pagination{HasMore: next != nil, NextCursor: next},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	trades, truncated, err := c.FetchTrades(context.Background(), FetchTradesOptions{
		Market:   "0x" + strings.Repeat("ab", 32),
		Start:    100,
		PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("not truncated")
	}
	if len(trades) != 2 {
		t.Fatalf("got %d trades", len(trades))
	}
	if len(calls) != 2 {
		t.Fatalf("calls=%d", len(calls))
	}
	if calls[0].Get("taker_only") != "true" || calls[0].Get("condition") == "" || calls[0].Get("market") != "" || calls[0].Get("offset") != "" {
		t.Fatalf("query=%s", calls[0].Encode())
	}
	if calls[1].Get("cursor") != "page-2" {
		t.Fatalf("second cursor=%s", calls[1].Get("cursor"))
	}
	if trades[0].Asset != "1" || trades[0].TransactionHash != "h1" {
		t.Fatalf("%+v", trades[0])
	}
}

func TestTradeKey(t *testing.T) {
	a := Trade{TransactionHash: "h", ProxyWallet: "0xAA", Asset: "1", Timestamp: 9, Size: 1.5}
	b := a
	b.ProxyWallet = "0xaa"
	if tradeKey(a) != tradeKey(b) {
		t.Fatal("wallet case should not change key")
	}
}

type rewriteRoundTripper struct {
	base   string
	parent http.RoundTripper
}

func rewriteHost(base string) http.RoundTripper {
	return rewriteRoundTripper{base: base, parent: http.DefaultTransport}
}

func (r rewriteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(r.base)
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	req.Host = u.Host
	return r.parent.RoundTrip(req)
}

func TestFetchTradesTimeoutClient(t *testing.T) {
	c := NewClient()
	if c.HTTP.Timeout < time.Second {
		t.Fatal("expected http timeout")
	}
}
