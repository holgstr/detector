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
			Active:         true,
		},
		{
			Market:         Market{ConditionID: "magdalena", Question: "Will Magdalena Andersson be the next Prime Minister of Sweden?", Slug: "will-magdalena-andersson-be-the-next-prime-minister-of-sweden"},
			GroupItemTitle: "Magdalena Andersson",
			Volume24hr:     149789,
			Volume:         743723,
			Active:         true,
		},
		{
			Market:         Market{ConditionID: "ulf", Question: "Will Ulf Kristersson be the next Prime Minister of Sweden?", Slug: "will-ulf-kristersson-be"},
			GroupItemTitle: "Ulf Kristersson",
			Volume24hr:     200000,
			Volume:         2e6,
			Active:         true,
		},
		{
			Market:     Market{ConditionID: "closed", Question: "Will Linus Andersson win?"},
			Closed:     true,
			Active:     true,
			Volume24hr: 9e9,
		},
		{
			Market:     Market{ConditionID: "event-only", Question: "Will someone else win?", Slug: "someone-else"},
			EventTitle: "Andersson series",
			Volume24hr: 8e9,
			Active:     true,
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
	if _, ok := PickBestMarket("Linus", hits); ok {
		t.Fatal("resolved market must not match")
	}
}

func TestPickBestMarketEventTitleAndAnyTopic(t *testing.T) {
	hits := []SearchMarket{
		{
			Market:         Market{ConditionID: "50bps", Question: "Will the Fed decrease interest rates by 50+ bps after the September 2026 meeting?", Slug: "fed-50", EventSlug: "fed-decision-in-september"},
			GroupItemTitle: "50+ bps decrease",
			EventTitle:     "Fed Decision in September?",
			Volume24hr:     10,
			Volume:         100,
			Active:         true,
		},
		{
			Market:         Market{ConditionID: "25bps", Question: "Will the Fed decrease interest rates by 25 bps after the September 2026 meeting?", Slug: "fed-25", EventSlug: "fed-decision-in-september"},
			GroupItemTitle: "25 bps decrease",
			EventTitle:     "Fed Decision in September?",
			Volume24hr:     80,
			Volume:         800,
			Active:         true,
		},
		{
			Market:     Market{ConditionID: "lakers", Question: "Vaxjo Lakers vs. Tappara Tampere", Slug: "vaxjo-lakers-vs-tappara-tampere"},
			EventTitle: "Vaxjo Lakers vs. Tappara Tampere",
			Volume24hr: 5,
			Active:     true,
		},
	}
	got, ok := PickBestMarket("Fed Decision", hits)
	if !ok || got.Market.ConditionID != "25bps" {
		t.Fatalf("event title should pick highest-volume child: %+v ok=%v", got, ok)
	}
	got, ok = PickBestMarket("Lakers", hits)
	if !ok || got.Market.ConditionID != "lakers" {
		t.Fatalf("sports: %+v", got)
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
		if r.URL.Query().Get("sort") != "volume24hr" {
			t.Errorf("sort=%s", r.URL.Query().Get("sort"))
		}
		if r.URL.Query().Get("keep_closed_markets") != "0" {
			t.Errorf("keep_closed_markets=%s", r.URL.Query().Get("keep_closed_markets"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]any{
				{
					"title":  "ITF: Linus Andersson (resolved)",
					"slug":   "itf-andersson",
					"active": true,
					"closed": true,
					"markets": []map[string]any{{
						"question":    "Will Linus Andersson win?",
						"conditionId": "0xclosed",
						"slug":        "linus-andersson",
						"active":      true,
						"closed":      true,
						"volume24hr":  9e9,
					}},
				},
				{
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
						{
							"question":    "Will Linus Andersson win the closed child?",
							"conditionId": "0xchildclosed",
							"slug":        "linus-child",
							"active":      true,
							"closed":      true,
							"volume24hr":  8e9,
						},
					},
				},
			},
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

func TestFindMarketRejectsResolvedSlug(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"conditionId": "0xdead",
			"slug":        "old-resolved-market",
			"question":    "Did this already resolve?",
			"outcomes":    `["Yes", "No"]`,
			"active":      true,
			"closed":      true,
		}})
	}))
	t.Cleanup(ts.Close)
	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)
	_, err := c.FindMarket(context.Background(), "old-resolved-market")
	if err == nil || !strings.Contains(err.Error(), "resolved") {
		t.Fatalf("err=%v", err)
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
