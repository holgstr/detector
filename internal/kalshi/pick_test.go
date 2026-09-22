package kalshi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPickMarketsEventExpands(t *testing.T) {
	hits := []Market{
		{Ticker: "GOVPARTYFL-26-R", EventTicker: "GOVPARTYFL-26", Event: "Florida Governor winner?", Name: "Byron Donalds"},
		{Ticker: "GOVPARTYFL-26-D", EventTicker: "GOVPARTYFL-26", Event: "Florida Governor winner?", Name: "David Jolly"},
		{Ticker: "KXVOTECOUNTY-1", EventTicker: "KXVOTECOUNTY", Event: "Miami-Dade County, Florida: Byron Donalds vote percent", Name: "At least 50%"},
	}
	got, err := PickMarkets("florida governor", hits)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpandEvent || got.EventTicker != "GOVPARTYFL-26" || len(got.Markets) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestPickMarketsOutcomeStaysNarrow(t *testing.T) {
	hits := []Market{
		{Ticker: "KXVOTECOUNTY-1", EventTicker: "KXVOTECOUNTY", Event: "Miami-Dade County, Florida: Byron Donalds vote percent", Name: "At least 50%"},
		{Ticker: "GOVPARTYFL-26-R", EventTicker: "GOVPARTYFL-26", Event: "Florida Governor winner?", Name: "Byron Donalds"},
		{Ticker: "GOVPARTYFL-26-D", EventTicker: "GOVPARTYFL-26", Event: "Florida Governor winner?", Name: "David Jolly"},
		{Ticker: "KXVPRESNOMR-28-BD", EventTicker: "KXVPRESNOMR-28", Event: "2028 Republican VP nominee", Name: "Byron Donalds"},
	}
	got, err := PickMarkets("byron donalds", hits)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpandEvent || len(got.Markets) != 1 || got.Markets[0].Ticker != "GOVPARTYFL-26-R" {
		t.Fatalf("%+v", got)
	}
}

func TestClientSearchEventAndBook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/search/series"):
			_, _ = w.Write([]byte(`{"current_page":[{"event_ticker":"KXFEDDECISION-26OCT","event_title":"Fed decision in Oct 2026?","markets":[{"ticker":"KXFEDDECISION-26OCT-H0","yes_subtitle":"Fed maintains rate","last_price_dollars":"0.4700","volume":1411013},{"ticker":"KXFEDDECISION-26OCT-H25","yes_subtitle":"Hike 25bps","last_price_dollars":"0.5100","volume":10}]}]}`))
		case strings.Contains(r.URL.Path, "/events/"):
			_, _ = w.Write([]byte(`{"event":{"event_ticker":"KXFEDDECISION-26OCT","title":"Fed decision in Oct 2026?","markets":[{"ticker":"KXFEDDECISION-26OCT-H0","status":"active","yes_sub_title":"Fed maintains rate","last_price_dollars":"0.4700","volume_fp":"100","price_ranges":[{"step":"0.0100"}]},{"ticker":"KXFEDDECISION-26OCT-OLD","status":"settled","result":"no","yes_sub_title":"Old"}]}}`))
		case strings.Contains(r.URL.Path, "/markets/orderbooks"):
			_, _ = w.Write([]byte(`{"orderbooks":[{"ticker":"KXFEDDECISION-26OCT-H0","orderbook_fp":{"yes_dollars":[["0.4500","100.00"],["0.4600","200.00"]],"no_dollars":[["0.5200","10.00"],["0.5300","40.00"]]}}]}`))
		case strings.Contains(r.URL.Path, "/markets/"):
			_, _ = w.Write([]byte(`{"market":{"ticker":"KXFEDDECISION-26OCT-H0","event_ticker":"KXFEDDECISION-26OCT","status":"active","yes_sub_title":"Fed maintains rate","title":"Will the Fed maintain?"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient()
	c.Trade = srv.URL
	c.Search = srv.URL

	hits, err := c.TextSearch(context.Background(), "fed decision")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Name != "Fed maintains rate" || hits[0].Event != "Fed decision in Oct 2026?" || hits[1].Volume != 10 {
		t.Fatalf("%+v", hits)
	}

	ev, err := c.MarketsByEvent(context.Background(), "KXFEDDECISION-26OCT")
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 || !ev[1].Resolved || ev[0].Tick != 0.01 {
		t.Fatalf("%+v", ev)
	}

	books, err := c.Books(context.Background(), []string{"KXFEDDECISION-26OCT-H0"})
	if err != nil {
		t.Fatal(err)
	}
	b := books["KXFEDDECISION-26OCT-H0"]
	if len(b.Bids) != 2 || b.Bids[0].Price != 0.46 || b.Bids[0].Size != 200 {
		t.Fatalf("bids %+v", b.Bids)
	}
	if len(b.Asks) != 2 || b.Asks[0].Price != 0.47 || b.Asks[0].Size != 40 || b.Asks[1].Price != 0.48 {
		t.Fatalf("asks %+v", b.Asks)
	}

	m, err := c.Market(context.Background(), "kxfeddecision-26oct-h0")
	if err != nil || m.Ticker != "KXFEDDECISION-26OCT-H0" || m.Event != "Will the Fed maintain?" {
		t.Fatalf("%v %+v", err, m)
	}

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer missing.Close()
	c.Trade = missing.URL
	if _, err := c.Market(context.Background(), "NOPE-1"); err != ErrNotFound {
		t.Fatalf("not found: %v", err)
	}
	_ = io.Discard
}
