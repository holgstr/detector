package alert

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/kalshi"
)

type fakeKalshi struct {
	hits   []kalshi.Market
	event  []kalshi.Market
	market kalshi.Market
	miss   bool
	books  map[string]kalshi.Book
}

func (f fakeKalshi) TextSearch(context.Context, string) ([]kalshi.Market, error) {
	return f.hits, nil
}
func (f fakeKalshi) Market(context.Context, string) (kalshi.Market, error) {
	if f.miss || f.market.Ticker == "" {
		return kalshi.Market{}, kalshi.ErrNotFound
	}
	return f.market, nil
}
func (f fakeKalshi) MarketsByEvent(context.Context, string) ([]kalshi.Market, error) {
	if f.event == nil {
		return nil, kalshi.ErrNotFound
	}
	return f.event, nil
}
func (f fakeKalshi) Books(context.Context, []string) (map[string]kalshi.Book, error) {
	return f.books, nil
}

func TestFetchKalshiOBEvent(t *testing.T) {
	api := fakeKalshi{
		hits: []kalshi.Market{
			{Ticker: "GOVPARTYFL-26-R", EventTicker: "GOVPARTYFL-26", Event: "Florida Governor winner?", Name: "Byron Donalds", LastPrice: 0.77},
			{Ticker: "GOVPARTYFL-26-D", EventTicker: "GOVPARTYFL-26", Event: "Florida Governor winner?", Name: "David Jolly", LastPrice: 0.23},
		},
		miss: true,
		event: []kalshi.Market{
			{Ticker: "GOVPARTYFL-26-D", EventTicker: "GOVPARTYFL-26", Event: "Florida Governor winner?", Name: "David Jolly", LastPrice: 0.23, Tick: 0.001},
			{Ticker: "GOVPARTYFL-26-R", EventTicker: "GOVPARTYFL-26", Event: "Florida Governor winner?", Name: "Byron Donalds", LastPrice: 0.77, Tick: 0.001},
			{Ticker: "GOVPARTYFL-26-OLD", EventTicker: "GOVPARTYFL-26", Event: "Florida Governor winner?", Name: "Old", Resolved: true},
		},
		books: map[string]kalshi.Book{
			"GOVPARTYFL-26-R": {
				Asks: []kalshi.Level{{Price: 0.771, Size: 10}, {Price: 0.772, Size: 11}, {Price: 0.773, Size: 12}, {Price: 0.774, Size: 13}, {Price: 0.90, Size: 99}},
				Bids: []kalshi.Level{{Price: 0.76, Size: 20}},
			},
			"GOVPARTYFL-26-D": {
				Asks: []kalshi.Level{{Price: 0.24, Size: 5}},
				Bids: []kalshi.Level{{Price: 0.22, Size: 6}},
			},
		},
	}
	rep, err := FetchKalshiOB(context.Background(), api, "florida governor")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Title != "Florida Governor winner?" || len(rep.Books) != 2 {
		t.Fatalf("%+v", rep)
	}
	if rep.Books[0].Outcome != "Byron Donalds" || len(rep.Books[0].Asks) != 4 || rep.Books[0].Asks[3].Price != 0.774 {
		t.Fatalf("depth %+v", rep.Books[0])
	}
	text := FormatKalshiOB(rep)
	for _, bit := range []string{"Florida Governor winner?", "BYRON DONALDS", "DAVID JOLLY", "77.1¢", "76.0¢", "- - -"} {
		if !strings.Contains(text, bit) {
			t.Fatalf("missing %q in\n%s", bit, text)
		}
	}
	if strings.Contains(text, "90.0¢") || strings.Contains(text, "OLD") {
		t.Fatalf("extra level or resolved outcome leaked:\n%s", text)
	}
}

func TestFetchKalshiOBTicker(t *testing.T) {
	api := fakeKalshi{
		market: kalshi.Market{Ticker: "KXFEDDECISION-26OCT-H0", EventTicker: "KXFEDDECISION-26OCT", Name: "Fed maintains rate"},
		event: []kalshi.Market{
			{Ticker: "KXFEDDECISION-26OCT-H0", EventTicker: "KXFEDDECISION-26OCT", Event: "Fed decision in Oct 2026?", Name: "Fed maintains rate", Tick: 0.01},
			{Ticker: "KXFEDDECISION-26OCT-H25", EventTicker: "KXFEDDECISION-26OCT", Event: "Fed decision in Oct 2026?", Name: "Hike 25bps"},
		},
		books: map[string]kalshi.Book{
			"KXFEDDECISION-26OCT-H0": {Bids: []kalshi.Level{{Price: 0.46, Size: 200}}},
		},
	}
	rep, err := FetchKalshiOB(context.Background(), api, "kxfeddecision-26oct-h0")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Books) != 1 || rep.Books[0].Outcome != "Fed maintains rate" || rep.Title != "Fed decision in Oct 2026?" {
		t.Fatalf("%+v", rep)
	}
	if strings.Contains(FormatKalshiOB(rep), "HIKE") {
		t.Fatal("event sibling leaked")
	}
}

func TestFetchKalshiOBResolved(t *testing.T) {
	api := fakeKalshi{
		market: kalshi.Market{Ticker: "KXFED-25-H0", Resolved: true},
	}
	_, err := FetchKalshiOB(context.Background(), api, "KXFED-25-H0")
	if err == nil || !strings.Contains(err.Error(), "resolved") {
		t.Fatal(err)
	}
	if !errors.Is(kalshi.ErrNotFound, kalshi.ErrNotFound) {
		t.Fatal("sentinel")
	}
}
