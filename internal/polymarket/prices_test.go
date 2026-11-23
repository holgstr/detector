package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchYesPriceHistory(t *testing.T) {
	gamma := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("condition_ids") != "0xabc" {
			t.Errorf("condition %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]gammaMarket{{
			ConditionID:  "0xabc",
			Question:     "Will it happen?",
			Outcomes:     `["Yes","No"]`,
			ClobTokenIDs: `["yes-token","no-token"]`,
			Active:       true,
		}})
	}))
	defer gamma.Close()

	var gotFidelity, gotMarket string
	clob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prices-history" {
			t.Errorf("path %s", r.URL.Path)
		}
		q := r.URL.Query()
		gotMarket = q.Get("market")
		gotFidelity = q.Get("fidelity")
		if q.Get("startTs") != "1000" || q.Get("endTs") != "5000" {
			t.Errorf("window %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"history": []map[string]any{
				{"t": 4000, "p": 0.42},
				{"t": 2000, "p": 0.40},
				{"t": 900, "p": 0.10},
				{"t": 3000, "p": 1.4},
			},
		})
	}))
	defer clob.Close()

	c := NewClient()
	c.GammaBase = gamma.URL
	c.ClobBase = clob.URL
	h, err := c.FetchYesPriceHistory(context.Background(), "0xabc", 1000, 5000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if gotMarket != "yes-token" || gotFidelity != "2" {
		t.Fatalf("query market=%s fidelity=%s", gotMarket, gotFidelity)
	}
	if h.Outcome != "Yes" || h.TokenID != "yes-token" {
		t.Fatalf("%+v", h)
	}
	if len(h.Points) != 2 || h.Points[0].Time != 2000 || h.Points[1].Price != 0.42 {
		t.Fatalf("%+v", h.Points)
	}
}
