package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEventIsSports(t *testing.T) {
	var eventHits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/sports") && !strings.Contains(r.URL.Path, "tags") {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		if strings.Contains(r.URL.Path, "related-tags") {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		eventHits.Add(1)
		slug := r.URL.Query().Get("slug")
		tags := []map[string]any{}
		switch slug {
		case "ucl-game":
			tags = []map[string]any{{"slug": "sports", "label": "Sports"}}
		case "nfl-mvp":
			tags = []map[string]any{{"slug": "nfl", "label": "NFL"}}
		case "miami-open":
			tags = []map[string]any{{"id": "864", "slug": "tennis", "label": "Tennis"}, {"slug": "atp"}}
		case "lol-match":
			tags = []map[string]any{{"slug": "esports"}, {"slug": "games"}}
		case "gta-price":
			tags = []map[string]any{{"slug": "games"}, {"slug": "pop-culture"}}
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"slug": slug, "title": "t", "tags": tags},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	mustSports := func(slug string) {
		t.Helper()
		ok, err := c.EventIsSports(context.Background(), slug)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("expected sports for %s", slug)
		}
	}
	mustNot := func(slug string) {
		t.Helper()
		ok, err := c.EventIsSports(context.Background(), slug)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("expected non-sports for %s", slug)
		}
	}

	mustSports("ucl-game")
	mustSports("nfl-mvp")
	mustSports("miami-open")
	mustSports("lol-match")
	mustNot("sweden-election")
	mustNot("gta-price")

	// Cached — no extra event hits.
	_, _ = c.EventIsSports(context.Background(), "ucl-game")
	if eventHits.Load() != 6 {
		t.Fatalf("event hits=%d want 6", eventHits.Load())
	}
}

func TestTagsAreSports(t *testing.T) {
	if !tagsAreSports([]eventTag{{Slug: "ATP"}}, nil) {
		t.Fatal("atp slug")
	}
	if !tagsAreSports([]eventTag{{ID: json.RawMessage(`"101232"`), Slug: "mystery-league"}}, map[int]struct{}{101232: {}}) {
		t.Fatal("catalog tag id")
	}
	if tagsAreSports([]eventTag{{Slug: "games", ID: json.RawMessage(`100639`)}}, map[int]struct{}{100639: {}}) {
		t.Fatal("games tag must not count as sports")
	}
	if tagsAreSports([]eventTag{{Slug: "politics"}}, map[int]struct{}{101232: {}}) {
		t.Fatal("politics")
	}
}

func TestEventIsSportsEmptySlug(t *testing.T) {
	c := NewClient()
	ok, err := c.EventIsSports(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("empty slug is not sports")
	}
}

func TestEventIsSportsMissingEvent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	ok, err := c.EventIsSports(context.Background(), "no-such-event")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("missing event is not sports")
	}
}

func TestSportsCatalogUsesPrimaryTag(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sports":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"primaryTagId": 102123}})
		case strings.Contains(r.URL.Path, "related-tags"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"relatedTagID": 100639}})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"slug": "wta-final", "tags": []map[string]any{{"id": 102123, "slug": "obscure-wta-event"}}},
			})
		}
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	ok, err := c.EventIsSports(context.Background(), "wta-final")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("primary WTA tag from /sports should count")
	}
}
