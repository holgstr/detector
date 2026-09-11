package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPickBestMarketAndersson(t *testing.T) {
	hits := []SearchMarket{
		{
			Market:         Market{ConditionID: "helene", Question: "Will Helene Andersson be the next Regional Board Chair of Region Halland?", Slug: "will-helene-andersson-be"},
			GroupItemTitle: "Helene Andersson",
			Volume:         1727,
		},
		{
			Market:         Market{ConditionID: "magdalena", Question: "Will Magdalena Andersson be the next Prime Minister of Sweden?", Slug: "will-magdalena-andersson-be-the-next-prime-minister-of-sweden"},
			GroupItemTitle: "Magdalena Andersson",
			Volume24hr:     149789,
			Volume:         743723,
		},
		{
			Market:         Market{ConditionID: "ulf", Question: "Will Ulf Kristersson be the next Prime Minister of Sweden?", Slug: "will-ulf-kristersson-be"},
			GroupItemTitle: "Ulf Kristersson",
			Volume24hr:     200000,
			Volume:         2e6,
		},
		{
			Market:     Market{ConditionID: "closed", Question: "Will Linus Andersson win?"},
			Closed:     true,
			Volume24hr: 9e9,
		},
	}
	got, ok := PickBestMarket("Andersson", hits)
	if !ok || got.Market.ConditionID != "magdalena" {
		t.Fatalf("got %+v ok=%v, want Magdalena", got, ok)
	}
	got, ok = PickBestMarket("Helene", hits)
	if !ok || got.Market.ConditionID != "helene" {
		t.Fatalf("Helene: %+v", got)
	}
	if _, ok := PickBestMarket("no-such", hits); ok {
		t.Fatal("expected miss")
	}
}

func TestSearchMarketsQuery(t *testing.T) {
	var path string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.URL.Query().Get("q") != "Andersson" {
			t.Errorf("q=%s", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("events_status") != "active" {
			t.Errorf("events_status=%s", r.URL.Query().Get("events_status"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]any{{
				"title":  "Next Prime Minister of Sweden",
				"slug":   "next-prime-minister-of-sweden",
				"active": true,
				"closed": false,
				"markets": []map[string]any{
					{
						"question":       "Will Magdalena Andersson be the next Prime Minister of Sweden?",
						"conditionId":    "0xabc",
						"slug":           "will-magdalena-andersson-be-the-next-prime-minister-of-sweden",
						"outcomes":       `["Yes", "No"]`,
						"groupItemTitle": "Magdalena Andersson",
						"volumeNum":      743723.0,
						"volume24hr":     149789.0,
						"active":         true,
						"closed":         false,
					},
				},
			}},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	hits, err := c.SearchMarkets(context.Background(), "Andersson")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "public-search") {
		t.Fatalf("path=%s", path)
	}
	if len(hits) != 1 || hits[0].Market.ConditionID != "0xabc" {
		t.Fatalf("%+v", hits)
	}
	best, err := c.FindMarket(context.Background(), "Andersson")
	if err != nil || best.Market.Question == "" {
		t.Fatalf("%+v %v", best, err)
	}
}

func TestLooksLikeSlug(t *testing.T) {
	if looksLikeSlug("Andersson") {
		t.Fatal("name is not a slug")
	}
	if !looksLikeSlug("will-magdalena-andersson-be-the-next-prime-minister-of-sweden") {
		t.Fatal("expected slug")
	}
}
