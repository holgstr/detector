package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestEventIsSports(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		slug := r.URL.Query().Get("slug")
		tags := []map[string]string{}
		switch slug {
		case "ucl-game":
			tags = []map[string]string{{"slug": "sports", "label": "Sports"}}
		case "nfl-mvp":
			tags = []map[string]string{{"slug": "nfl", "label": "NFL"}}
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"slug": slug, "title": "t", "tags": tags},
		})
	}))
	t.Cleanup(ts.Close)

	c := NewClient()
	c.HTTP = ts.Client()
	c.HTTP.Transport = rewriteHost(ts.URL)

	ok, err := c.EventIsSports(context.Background(), "ucl-game")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected sports")
	}
	ok, err = c.EventIsSports(context.Background(), "nfl-mvp")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected nfl league tag to count as sports")
	}
	ok, err = c.EventIsSports(context.Background(), "sweden-election")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected non-sports")
	}

	// Cached — no extra hits.
	_, _ = c.EventIsSports(context.Background(), "ucl-game")
	if hits.Load() != 3 {
		t.Fatalf("hits=%d want 3", hits.Load())
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
