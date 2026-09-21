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

func TestPickBestMarketFlavioPrefersPresidentialWinner(t *testing.T) {
	hits := []SearchMarket{
		{
			Market:         Market{ConditionID: "share", Question: "Will Flávio Bolsonaro win 39% or more of the valid vote in the first round of the 2026 Brazilian presidential election?", Slug: "brazil-first-round-vote-share-flavio-bolsonaro-39-plus"},
			GroupItemTitle: "39%+",
			EventTitle:     "Brazil Presidential Election First Round: Flávio Bolsonaro Vote Share?",
			Volume24hr:     1450,
			Volume:         24907,
			Active:         true,
		},
		{
			Market:         Market{ConditionID: "votes", Question: "Will Flavio Bolsonaro win the most votes in the first round of the 2026 Brazil presidential election?", Slug: "will-flavio-bolsonaro-win-the-most-votes-in-the-first-round"},
			GroupItemTitle: "Flavio Bolsonaro",
			EventTitle:     "Brazil Presidential Election First Round Winner",
			Volume24hr:     8240,
			Volume:         61293,
			Liquidity:      53812,
			Active:         true,
		},
		{
			Market:         Market{ConditionID: "second", Question: "Will Flávio Bolsonaro finish in second place in the first round of the 2026 Brazilian presidential election?", Slug: "will-flvio-bolsonaro-finish-in-second-place"},
			GroupItemTitle: "Flávio Bolsonaro",
			EventTitle:     "Brazil Presidential Election First Round: 2nd Place",
			Volume24hr:     45179,
			Volume:         1e6,
			Active:         true,
		},
		{
			Market:         Market{ConditionID: "win", Question: "Will Flávio Bolsonaro win the 2026 Brazilian presidential election?", Slug: "will-flvio-bolsonaro-win-the-2026-brazilian-presidential-election", EventSlug: "brazil-presidential-election"},
			GroupItemTitle: "Flávio Bolsonaro",
			EventTitle:     "Brazil Presidential Election",
			Volume24hr:     113184,
			Volume:         11e6,
			Liquidity:      342725,
			Active:         true,
		},
	}
	got, ok := PickBestMarket("Flavio", hits)
	if !ok || got.Market.ConditionID != "win" {
		t.Fatalf("name query should pick overall winner, got %+v ok=%v", got, ok)
	}
	got, ok = PickBestMarket("Flávio", hits)
	if !ok || got.Market.ConditionID != "win" {
		t.Fatalf("accented name: %+v ok=%v", got, ok)
	}
	got, ok = PickBestMarket("Flavio most votes", hits)
	if !ok || got.Market.ConditionID != "votes" {
		t.Fatalf("explicit side query: %+v ok=%v", got, ok)
	}
	top := TopRankMatches("Flavio", hits)
	if len(top) != 1 || top[0].Market.ConditionID != "win" {
		t.Fatalf("top should be only the winner market: %+v", top)
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

func TestLooksLikeSlug(t *testing.T) {
	if looksLikeSlug("Andersson") {
		t.Fatal("name is not a slug")
	}
	if !looksLikeSlug("will-magdalena-andersson-be-the-next-prime-minister-of-sweden") {
		t.Fatal("expected slug")
	}
}
