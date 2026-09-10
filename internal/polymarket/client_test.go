package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetJSONRetries429(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"Too Many Requests"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	c := NewClient()
	var dest map[string]bool
	if err := c.getJSON(context.Background(), srv.URL, &dest); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("calls=%d want 3", n)
	}
	if !dest["ok"] {
		t.Fatalf("dest=%v", dest)
	}
}

func TestExtractMarketSlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "https://polymarket.com/event/which-party-will-win-the-senate-in-2026/will-the-democratic-party-control-the-senate-after-the-2026-midterm-elections",
			want: "will-the-democratic-party-control-the-senate-after-the-2026-midterm-elections",
		},
		{
			in:   "will-the-democratic-party-control-the-senate-after-the-2026-midterm-elections",
			want: "will-the-democratic-party-control-the-senate-after-the-2026-midterm-elections",
		},
		{
			in:   "polymarket.com/market/foo-bar",
			want: "foo-bar",
		},
	}
	for _, tc := range cases {
		got, err := extractMarketSlug(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsConditionID(t *testing.T) {
	ok := "0x307a1ed89d60b61002dd5bbf00e1408c5ed2ab3fcdb056191ca7ef9bc34d38f3"
	if !isConditionID(ok) {
		t.Fatal("expected valid condition id")
	}
	if isConditionID("not-a-condition") {
		t.Fatal("expected invalid")
	}
}
