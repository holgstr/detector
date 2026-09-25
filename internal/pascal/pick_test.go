package pascal

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
		{Symbol: "FL_GOV_2026.REP", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Republicans", Volume24h: 100},
		{Symbol: "FL_GOV_2026.DEM", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Democrats", Volume24h: 40},
		{Symbol: "OTHER.REP", EventCode: "OTHER", Event: "Some other governor race", Name: "Republicans", Volume24h: 1},
	}
	got, err := PickMarkets("florida governor", hits)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpandEvent || got.EventCode != "FL_GOV_2026" || len(got.Markets) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestPickMarketsOutcomeStaysNarrow(t *testing.T) {
	hits := []Market{
		{Symbol: "FL_GOV_2026.REP", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Republicans"},
		{Symbol: "FL_GOV_2026.DEM", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Democrats"},
	}
	got, err := PickMarkets("florida governor republicans", hits)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpandEvent || len(got.Markets) != 1 || got.Markets[0].Symbol != "FL_GOV_2026.REP" {
		t.Fatalf("%+v", got)
	}
}

func TestPickMarketsPrefersWinnerOverPollMargin(t *testing.T) {
	hits := []Market{
		{Symbol: "EMERSON_MISEN_26SEP21_ABOVE.1P5", EventCode: "EMERSON_MISEN_26SEP21_ABOVE", Event: "Emerson Michigan Senate poll margin", Name: "El-Sayed above 1.5 points", Volume24h: 488},
		{Symbol: "MI_SEN_2026.DEM", EventCode: "MI_SEN_2026", Event: "Michigan Senate Winner", Name: "Democrats", Volume24h: 243},
		{Symbol: "MI_SEN_2026.REP", EventCode: "MI_SEN_2026", Event: "Michigan Senate Winner", Name: "Republicans", Volume24h: 40},
		{Symbol: "EMERSON_MISEN_26SEP21_ABOVE.0P5", EventCode: "EMERSON_MISEN_26SEP21_ABOVE", Event: "Emerson Michigan Senate poll margin", Name: "El-Sayed above 0.5 points"},
	}
	got, err := PickMarkets("michigan senate", hits)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ExpandEvent || got.EventCode != "MI_SEN_2026" || len(got.Markets) != 2 {
		t.Fatalf("%+v", got)
	}

	poll, err := PickMarkets("michigan senate poll", hits)
	if err != nil {
		t.Fatal(err)
	}
	if !poll.ExpandEvent || poll.EventCode != "EMERSON_MISEN_26SEP21_ABOVE" || len(poll.Markets) != 2 {
		t.Fatalf("%+v", poll)
	}
}

func TestPickMarketsExactSymbol(t *testing.T) {
	hits := []Market{
		{Symbol: "FL_GOV_2026.REP", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Republicans"},
		{Symbol: "FL_GOV_2026.DEM", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Democrats"},
	}
	got, err := PickMarkets("fl_gov_2026.dem", hits)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpandEvent || len(got.Markets) != 1 || got.Markets[0].Name != "Democrats" {
		t.Fatalf("%+v", got)
	}
}

func TestClientBooksAndSearch(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "text-search"):
			_, _ = w.Write([]byte(`{"status":"success","data":[{"symbol":"FL_GOV_2026.REP","tick_size_min":"0.001000","mark_price":"0.770000","display_attributes":{"event_description":"Florida Governor Winner","market_description":"Republicans"},"stats":{"last_24h":{"taker_buy_volume":"10","taker_sell_volume":"5"}}}]}`))
		case strings.Contains(r.URL.Path, "/books"):
			_, _ = w.Write([]byte(`{"status":"success","data":{"books":{"FL_GOV_2026.REP":{"asks":[["0.780000","100"]],"bids":[["0.760000","40"]]}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient()
	c.Base = srv.URL

	markets, err := c.TextSearch(context.Background(), "florida governor")
	if err != nil {
		t.Fatal(err)
	}
	if len(markets) != 1 || markets[0].Name != "Republicans" || markets[0].Volume24h != 15 || markets[0].TickMin != 0.001 || markets[0].EventCode != "FL_GOV_2026" {
		t.Fatalf("%+v", markets)
	}
	if !strings.Contains(gotBody, `"status":"live"`) {
		t.Fatalf("body %s", gotBody)
	}

	books, err := c.Books(context.Background(), []string{"FL_GOV_2026.REP"})
	if err != nil {
		t.Fatal(err)
	}
	b := books["FL_GOV_2026.REP"]
	if len(b.Asks) != 1 || b.Asks[0].Price != 0.78 || b.Asks[0].Size != 100 || b.Bids[0].Size != 40 {
		t.Fatalf("%+v path %s", b, gotPath)
	}
}
