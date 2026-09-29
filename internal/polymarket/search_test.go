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

func TestTopRankMatches(t *testing.T) {
	hits := []SearchMarket{
		{
			Market:         Market{ConditionID: "helene", Question: "Will Helene Andersson be the next Regional Board Chair?", Slug: "will-helene-andersson-be"},
			GroupItemTitle: "Helene Andersson",
			Volume24hr:     5000,
			Active:         true,
		},
		{
			Market:         Market{ConditionID: "magdalena", Question: "Will Magdalena Andersson be the next Prime Minister of Sweden?", Slug: "will-magdalena-andersson-be"},
			GroupItemTitle: "Magdalena Andersson",
			Volume24hr:     149789,
			Active:         true,
		},
		{
			Market:     Market{ConditionID: "event-only", Question: "Will someone else win?", Slug: "someone-else"},
			EventTitle: "Andersson series",
			Volume24hr: 8e9,
			Active:     true,
		},
	}
	top := TopRankMatches("Andersson", hits)
	if len(top) != 2 {
		t.Fatalf("want 2 top-rank hits, got %d %+v", len(top), top)
	}
	if top[0].Market.ConditionID != "magdalena" || top[1].Market.ConditionID != "helene" {
		t.Fatalf("volume order: %+v", top)
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

func TestPickBestMarketLiquidityBreaksVolumeTie(t *testing.T) {
	hits := []SearchMarket{
		{
			Market:     Market{ConditionID: "thin", Question: "Will Alice win?"},
			Volume24hr: 100,
			Volume:     100,
			Liquidity:  10,
			Active:     true,
		},
		{
			Market:     Market{ConditionID: "deep", Question: "Will Alice be president?"},
			Volume24hr: 100,
			Volume:     100,
			Liquidity:  50000,
			Active:     true,
		},
	}
	got, ok := PickBestMarket("Alice", hits)
	if !ok || got.Market.ConditionID != "deep" {
		t.Fatalf("liquidity: %+v ok=%v", got, ok)
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
							"liquidityNum":   12000.0,
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
	if hits[0].Liquidity != 12000 {
		t.Fatalf("liquidity=%v", hits[0].Liquidity)
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

func TestPickBestMarketNameTypo(t *testing.T) {
	hits := []SearchMarket{
		{
			Market:         Market{ConditionID: "dem", Question: "Will the Democratic Party win the FL-25 House seat?", Slug: "will-the-democratic-party-win-the-fl-25-house-seat"},
			GroupItemTitle: "Jared Moskowitz (D)",
			EventTitle:     "FL-25 House Election Winner",
			Volume24hr:     112,
			Active:         true,
		},
		{
			Market:         Market{ConditionID: "gop", Question: "Will the Republican Party win the FL-25 House seat?", Slug: "will-the-republican-party-win-the-fl-25-house-seat"},
			GroupItemTitle: "Scott Singer (R)",
			EventTitle:     "FL-25 House Election Winner",
			Volume24hr:     40,
			Active:         true,
		},
		{
			Market:         Market{ConditionID: "margin", Question: "Will the Republican Party candidate win the 2026 FL-25 House election by 12% or more?", Slug: "fl-25-margin"},
			GroupItemTitle: "Republican 12%+",
			EventTitle:     "FL-25 House Election Margin of Victory",
			Volume24hr:     9000,
			Active:         true,
		},
	}
	for _, q := range []string{"Moskowitz", "Mowkowitz"} {
		got, ok := PickBestMarket(q, hits)
		if !ok || got.Market.ConditionID != "dem" {
			t.Fatalf("%s: got %+v ok=%v", q, got.Market.ConditionID, ok)
		}
	}
}

func TestSearchMarketsByOptionTitle(t *testing.T) {
	var sawExecute bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/search/execute" {
			sawExecute = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			params, _ := body["params"].(map[string]any)
			if params["query"] != "Moskowitz" {
				t.Fatalf("outcome query %#v", params["query"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{
						"title":  "FL-25 Democratic Primary Winner",
						"slug":   "fl-25-democratic-primary-winner",
						"closed": true,
						"markets": []map[string]any{{
							"slug":           "jared-moskowitz-primary",
							"question":       "Will Jared Moskowitz be the FL-25 Democratic nominee?",
							"groupItemTitle": "Jared Moskowitz",
							"outcomes":       []string{"Yes", "No"},
							"active":         true,
							"closed":         true,
						}},
					},
					{
						"title":      "FL-25 House Election Winner",
						"slug":       "fl-25-house-election-winner",
						"closed":     false,
						"volume24hr": 122,
						"markets": []map[string]any{
							{
								"slug":           "fl-25-gop",
								"question":       "Will the Republican Party win the FL-25 House seat?",
								"groupItemTitle": "Scott Singer (R)",
								"outcomes":       []string{"Yes", "No"},
								"active":         true,
								"closed":         false,
							},
							{
								"slug":           "fl-25-dem",
								"question":       "Will the Democratic Party win the FL-25 House seat?",
								"groupItemTitle": "Jared Moskowitz (D)",
								"outcomes":       []string{"Yes", "No"},
								"active":         true,
								"closed":         false,
							},
						},
					},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "public-search") {
			_ = json.NewEncoder(w).Encode(map[string]any{"events": []any{}})
			return
		}
		if slug := r.URL.Query().Get("slug"); slug == "fl-25-dem" {
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"conditionId": "0xmoskowitz",
				"slug":        "fl-25-dem",
				"question":    "Will the Democratic Party win the FL-25 House seat?",
				"outcomes":    `["Yes", "No"]`,
				"active":      true,
				"closed":      false,
				"events":      []map[string]any{{"slug": "fl-25-house-election-winner"}},
			}})
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.String())
		http.Error(w, "no", http.StatusNotFound)
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	best, err := c.FindMarket(context.Background(), "Moskowitz")
	if err != nil {
		t.Fatal(err)
	}
	if !sawExecute {
		t.Fatal("expected outcome search")
	}
	if best.Market.ConditionID != "0xmoskowitz" || best.GroupItemTitle != "Jared Moskowitz (D)" {
		t.Fatalf("got %s %q", best.Market.ConditionID, best.GroupItemTitle)
	}
	if best.EventTitle != "FL-25 House Election Winner" {
		t.Fatalf("event %q", best.EventTitle)
	}
}

func TestPickBestMarketNamedOutcome(t *testing.T) {
	hits := []SearchMarket{
		{
			Market: Market{
				ConditionID: "match",
				Question:    "M15 Fayetteville",
				Outcomes:    []string{"Jared Horwood", "Volodymyr Gurenko"},
			},
			Active: true,
		},
		{
			Market: Market{
				ConditionID: "sets",
				Question:    "Total sets",
				Outcomes:    []string{"Over 2.5", "Under 2.5"},
			},
			Volume24hr: 9000,
			Active:     true,
		},
	}
	got, ok := PickBestMarket("Horwood", hits)
	if !ok || got.Market.ConditionID != "match" {
		t.Fatalf("got %+v ok=%v", got.Market.ConditionID, ok)
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
