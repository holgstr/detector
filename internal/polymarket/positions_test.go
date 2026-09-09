package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFetchPositionsRequiresUser(t *testing.T) {
	c := NewClient()
	_, err := c.FetchPositions(context.Background(), FetchPositionsOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchPositionsQuery(t *testing.T) {
	var got url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/positions" {
			t.Errorf("path=%s", r.URL.Path)
		}
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode([]Position{
			{ProxyWallet: "0xaaa", ConditionID: "0xabc", Outcome: "Yes", Size: 10},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	pos, err := c.FetchPositions(context.Background(), FetchPositionsOptions{
		User:   "0xAAA",
		Market: "0xabc",
		Limit:  20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != 1 {
		t.Fatalf("got %d", len(pos))
	}
	if got.Get("user") != "0xAAA" || got.Get("market") != "0xabc" ||
		got.Get("limit") != "20" || got.Get("sizeThreshold") != "0" {
		t.Fatalf("query=%s", got.Encode())
	}
}

func TestNetShares(t *testing.T) {
	cases := []struct {
		name string
		in   []Position
		size float64
		out  string
	}{
		{"yes only", []Position{{Outcome: "Yes", Size: 27500}}, 27500, "YES"},
		{"no only", []Position{{Outcome: "No", Size: 100}}, 100, "NO"},
		{"net yes", []Position{{Outcome: "Yes", Size: 100000}, {Outcome: "No", Size: 72500}}, 27500, "YES"},
		{"net no", []Position{{Outcome: "Yes", Size: 10}, {Outcome: "No", Size: 40}}, 30, "NO"},
		{"flat", []Position{{Outcome: "Yes", Size: 50}, {Outcome: "No", Size: 50}}, 0, ""},
		{"empty", nil, 0, ""},
		{"other", []Position{{Outcome: "Kamala", Size: 12}}, 12, "KAMALA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NetShares(tc.in)
			if !got.Known {
				t.Fatal("expected known")
			}
			if got.Size != tc.size || got.Outcome != tc.out {
				t.Fatalf("got size=%v outcome=%q want %v %q", got.Size, got.Outcome, tc.size, tc.out)
			}
		})
	}
}
