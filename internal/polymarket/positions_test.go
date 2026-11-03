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
		if r.URL.Path != "/v2/positions" {
			t.Errorf("path=%s", r.URL.Path)
		}
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []positionWire{{
				ProxyWallet: "0xaaa", ConditionID: "0xabc", Outcome: "Yes", CurrentSize: 10,
			}},
			"pagination": pagination{HasMore: false},
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
	if got.Get("user") != "0xAAA" || got.Get("condition") != "0xabc" ||
		got.Get("limit") != "20" || got.Get("filter_type") != "TOKENS" || got.Get("filter_amount") != "0" ||
		got.Get("market") != "" || got.Get("offset") != "" {
		t.Fatalf("query=%s", got.Encode())
	}
	if pos[0].Size != 10 || pos[0].Outcome != "Yes" || pos[0].ConditionID != "0xabc" {
		t.Fatalf("%+v", pos[0])
	}
}

func TestFetchPositionsSortBy(t *testing.T) {
	var got url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":       []positionWire{},
			"pagination": pagination{},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	_, err := c.FetchPositions(context.Background(), FetchPositionsOptions{
		User:          "0xAAA",
		Limit:         500,
		SortBy:        "CURRENT",
		SortDirection: "DESC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("sort_by") != "CURRENT_VALUE" || got.Get("sort_direction") != "DESC" {
		t.Fatalf("query=%s", got.Encode())
	}
}

func TestFetchPositionsCursor(t *testing.T) {
	var got url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		next := "page-2"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":       []positionWire{},
			"pagination": pagination{HasMore: true, NextCursor: &next},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	var next string
	_, err := c.FetchPositions(context.Background(), FetchPositionsOptions{
		User:       "0xAAA",
		Limit:      500,
		Cursor:     "page-1",
		NextCursor: &next,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("cursor") != "page-1" || got.Get("offset") != "" || got.Get("limit") != "500" {
		t.Fatalf("query=%s", got.Encode())
	}
	if next != "page-2" {
		t.Fatalf("next=%q", next)
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
