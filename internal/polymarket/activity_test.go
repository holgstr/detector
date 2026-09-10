package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFetchActivityRequiresUser(t *testing.T) {
	c := NewClient()
	_, err := c.FetchActivity(context.Background(), FetchActivityOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchActivityQuery(t *testing.T) {
	var got url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/activity" {
			t.Errorf("path=%s", r.URL.Path)
		}
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]Activity{
			{ProxyWallet: "0xaaa", TransactionHash: "h1", Size: 10, Timestamp: 9, Asset: "tok"},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	acts, err := c.FetchActivity(context.Background(), FetchActivityOptions{
		User:  "0xAAA",
		Limit: 50,
		Type:  "TRADE",
		Start: 1_700_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 {
		t.Fatalf("got %d", len(acts))
	}
	if got.Get("user") != "0xAAA" || got.Get("type") != "TRADE" || got.Get("limit") != "50" ||
		got.Get("start") != "1700000000" {
		t.Fatalf("query=%s", got.Encode())
	}
}

func TestActivityKeyWalletCase(t *testing.T) {
	a := Activity{TransactionHash: "h", ProxyWallet: "0xAA", Asset: "1", Timestamp: 9, Size: 1.5}
	b := a
	b.ProxyWallet = "0xaa"
	if ActivityKey(a) != ActivityKey(b) {
		t.Fatal("wallet case should not change key")
	}
}

func TestFetchActivityBatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		_ = json.NewEncoder(w).Encode([]Activity{
			{ProxyWallet: user, TransactionHash: "h-" + user, Size: 1, Timestamp: 1, Asset: "a"},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.Workers = 2
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	acts, err := c.FetchActivityBatch(context.Background(), []string{"0x1", "0x2"}, FetchActivityOptions{Type: "TRADE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 2 {
		t.Fatalf("got %d", len(acts))
	}
}

func TestFetchActivitySincePagesUntilWindow(t *testing.T) {
	var offsets []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offsets = append(offsets, r.URL.Query().Get("offset"))
		if r.URL.Query().Get("start") != "100" || r.URL.Query().Get("type") != "TRADE" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		off := r.URL.Query().Get("offset")
		if off == "0" {
			page := make([]Activity, 500)
			for i := range page {
				page[i] = Activity{ProxyWallet: "0x1", Timestamp: 200, Size: 1, TransactionHash: "a", Asset: "t"}
			}
			_ = json.NewEncoder(w).Encode(page)
			return
		}
		_ = json.NewEncoder(w).Encode([]Activity{
			{ProxyWallet: "0x1", Timestamp: 50, Size: 1, TransactionHash: "old", Asset: "t"},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	acts, trunc, err := c.FetchActivitySince(context.Background(), "0x1", 100, "TRADE")
	if err != nil {
		t.Fatal(err)
	}
	if trunc {
		t.Fatal("should finish at old page")
	}
	if len(acts) != 500 {
		t.Fatalf("kept %d (old row dropped)", len(acts))
	}
	if len(offsets) != 2 || offsets[1] != "500" {
		t.Fatalf("offsets=%v", offsets)
	}
}
