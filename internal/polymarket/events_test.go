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

	// Cached — no extra event hits. League-prefixed slugs (ucl, nfl, lol)
	// are sports without a Gamma event fetch.
	_, _ = c.EventIsSports(context.Background(), "ucl-game")
	if eventHits.Load() != 3 {
		t.Fatalf("event hits=%d want 3", eventHits.Load())
	}
}

func TestTagsAreSports(t *testing.T) {
	if !tagsAreSports([]eventTag{{Slug: "ATP"}}, nil, nil) {
		t.Fatal("atp slug")
	}
	if !tagsAreSports([]eventTag{{ID: json.RawMessage(`"101232"`), Slug: "mystery-league"}}, map[int]struct{}{101232: {}}, nil) {
		t.Fatal("catalog tag id")
	}
	if tagsAreSports([]eventTag{{Slug: "games", ID: json.RawMessage(`100639`)}}, map[int]struct{}{100639: {}}, nil) {
		t.Fatal("games tag must not count as sports")
	}
	if tagsAreSports([]eventTag{{Slug: "politics"}}, map[int]struct{}{101232: {}}, nil) {
		t.Fatal("politics")
	}
	if !tagsAreSports([]eventTag{{Slug: "japan-j2-league"}}, nil, map[string]struct{}{"japan-j2-league": {}}) {
		t.Fatal("catalog sport code as tag slug")
	}
}

func TestLooksLikeSportsSlug(t *testing.T) {
	if !LooksLikeSportsSlug("nfl-atl-pit-2026-09-13") {
		t.Fatal("nfl game")
	}
	if !LooksLikeSportsSlug("epl-mun-mac-2026-09-13-more-markets") {
		t.Fatal("epl more-markets")
	}
	if !LooksLikeSportsSlug("bun-elv-bay-2026-09-13") {
		t.Fatal("bundesliga prefix")
	}
	if LooksLikeSportsSlug("sweden-parliamentary-election-v-over-under-7-percent-20260812172023903") {
		t.Fatal("election slug is not sports")
	}
	if !LooksLikeSportsSlug("pro-football-2026-27-passing-touchdowns-leader-20260715212611250") {
		t.Fatal("pro-football season market")
	}
	if LooksLikeSportsSlug("fed-decision-in-september") {
		t.Fatal("fed")
	}
	if !looksLikeSportsSlug(map[string]struct{}{"xyz": {}}, "xyz-foo-bar-2026-09-13") {
		t.Fatal("catalog sport code + match date")
	}
	if looksLikeSportsSlug(map[string]struct{}{"xyz": {}}, "xyz-without-a-date") {
		t.Fatal("catalog code without match date is not enough")
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
				{"slug": "obscure-final", "tags": []map[string]any{{"id": 102123, "slug": "obscure-wta-event"}}},
			})
		}
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	ok, err := c.EventIsSports(context.Background(), "obscure-final")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("primary WTA tag from /sports should count")
	}
}

func TestSportsCatalogUsesTagsCSV(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sports":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"sport": "xyz", "primaryTagId": 9, "tags": "1,100639,88881"},
			})
		case strings.Contains(r.URL.Path, "related-tags"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"slug": "obscure-cup", "tags": []map[string]any{{"id": 88881, "slug": "obscure-cup-tag"}}},
			})
		}
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	ok, err := c.EventIsSports(context.Background(), "obscure-cup")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("tag id from /sports tags CSV should count")
	}
}

func TestEmptyTagsAreNotCachedAsNonSports(t *testing.T) {
	var n atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/sports") || strings.Contains(r.URL.Path, "related-tags") {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		n.Add(1)
		tags := []map[string]any{}
		if n.Load() > 1 {
			tags = []map[string]any{{"slug": "soccer"}}
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"slug": "new-match", "tags": tags},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	ok, err := c.EventIsSports(context.Background(), "new-match")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("empty tags should not yet be sports")
	}
	ok, err = c.EventIsSports(context.Background(), "new-match")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("later soccer tag should count; empty-tag miss must not be cached")
	}
}
