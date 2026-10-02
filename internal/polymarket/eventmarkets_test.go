package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchEventMarketsYesPrice(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("slug") != "senate-race" {
			t.Errorf("slug=%s", r.URL.Query().Get("slug"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"slug":  "senate-race",
				"title": "Senate race",
				"markets": []map[string]any{
					{
						"conditionId":    "0xaaa",
						"slug":           "cand-a",
						"question":       "Will A win?",
						"groupItemTitle": "Candidate A",
						"outcomes":       `["Yes","No"]`,
						"outcomePrices":  `["0.62","0.38"]`,
						"active":         true,
						"volume24hr":     10,
					},
					{
						"conditionId":    "0xbbb",
						"slug":           "cand-b",
						"question":       "Will B win?",
						"groupItemTitle": "Candidate B",
						"outcomes":       []string{"Yes", "No"},
						"outcomePrices":  []string{"0.31", "0.69"},
						"active":         true,
					},
					{
						"conditionId":   "0xclosed",
						"slug":          "old",
						"question":      "Old",
						"active":        true,
						"closed":        true,
						"outcomePrices": `["0.99","0.01"]`,
					},
				},
			},
		})
	}))
	t.Cleanup(ts.Close)
	c := NewClient()
	c.GammaBase = ts.URL
	c.HTTP = ts.Client()
	got, err := c.FetchEventMarkets(context.Background(), "senate-race")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].Market.ConditionID != "0xaaa" || got[0].YesPrice != 0.62 || got[0].GroupItemTitle != "Candidate A" {
		t.Fatalf("a %+v", got[0])
	}
	if got[0].Market.URL != "https://polymarket.com/event/senate-race/cand-a" {
		t.Fatalf("url %s", got[0].Market.URL)
	}
	if got[1].YesPrice != 0.31 {
		t.Fatalf("b price %v", got[1].YesPrice)
	}
}
