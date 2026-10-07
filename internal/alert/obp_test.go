package alert

import (
	"context"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/pascal"
)

type fakePascal struct {
	hits   []pascal.Market
	event  []pascal.Market
	symbol []pascal.Market
	books  map[string]pascal.Book
	only   string
}

func (f fakePascal) TextSearch(_ context.Context, q string) ([]pascal.Market, error) {
	if f.only != "" && q != f.only {
		return nil, nil
	}
	return f.hits, nil
}
func (f fakePascal) MarketsBySymbols(context.Context, []string) ([]pascal.Market, error) {
	return f.symbol, nil
}
func (f fakePascal) MarketsByEvent(context.Context, string) ([]pascal.Market, error) {
	return f.event, nil
}
func (f fakePascal) Books(context.Context, []string) (map[string]pascal.Book, error) {
	return f.books, nil
}

func TestFetchPascalOBEvent(t *testing.T) {
	api := fakePascal{
		hits: []pascal.Market{
			{Symbol: "FL_GOV_2026.REP", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Republicans", MarkPrice: 0.77, TickMin: 0.001},
			{Symbol: "FL_GOV_2026.DEM", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Democrats", MarkPrice: 0.23, TickMin: 0.001},
		},
		event: []pascal.Market{
			{Symbol: "FL_GOV_2026.DEM", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Democrats", MarkPrice: 0.23, TickMin: 0.001},
			{Symbol: "FL_GOV_2026.REP", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Republicans", MarkPrice: 0.77, TickMin: 0.001},
			{Symbol: "FL_GOV_2026.OLD", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Old", Resolved: true},
		},
		books: map[string]pascal.Book{
			"FL_GOV_2026.REP": {
				Asks: []pascal.Level{{Price: 0.78, Size: 10}, {Price: 0.79, Size: 11}, {Price: 0.80, Size: 12}, {Price: 0.81, Size: 13}, {Price: 0.90, Size: 99}},
				Bids: []pascal.Level{{Price: 0.76, Size: 20}},
			},
			"FL_GOV_2026.DEM": {
				Asks: []pascal.Level{{Price: 0.24, Size: 5}},
				Bids: []pascal.Level{{Price: 0.22, Size: 6}},
			},
		},
	}
	rep, err := FetchPascalOB(context.Background(), api, "florida governor")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Title != "Florida Governor Winner" || len(rep.Books) != 2 {
		t.Fatalf("%+v", rep)
	}
	if rep.Books[0].Outcome != "Republicans" || len(rep.Books[0].Asks) != 4 || rep.Books[0].Asks[3].Price != 0.81 {
		t.Fatalf("depth %+v", rep.Books[0])
	}
	text := FormatPascalOB(rep)
	wantBits := []string{"Florida Governor Winner", "REPUBLICANS", "DEMOCRATS", "78.0¢", "76.0¢", "- - -"}
	for _, bit := range wantBits {
		if !strings.Contains(text, bit) {
			t.Fatalf("missing %q in\n%s", bit, text)
		}
	}
	if strings.Contains(text, "90.0¢") || strings.Contains(text, "OLD") {
		t.Fatalf("extra level or resolved outcome leaked:\n%s", text)
	}
}

func TestFetchPascalOBPolymarketQuestion(t *testing.T) {
	api := fakePascal{
		only: "nevada governor",
		hits: []pascal.Market{
			{Symbol: "NV_GOV_2026.DEM", EventCode: "NV_GOV_2026", Event: "Nevada Governor Winner", Name: "Democrats"},
			{Symbol: "NV_GOV_2026.REP", EventCode: "NV_GOV_2026", Event: "Nevada Governor Winner", Name: "Republicans"},
		},
		books: map[string]pascal.Book{
			"NV_GOV_2026.REP": {Bids: []pascal.Level{{Price: 0.55, Size: 10}}},
		},
	}
	rep, err := FetchPascalOB(context.Background(), api, "Will the Republicans win the Nevada governor race")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Books) != 1 || rep.Books[0].Outcome != "Republicans" || rep.Title != "Nevada Governor Winner" {
		t.Fatalf("%+v", rep)
	}
}

func TestFetchPascalOBPolymarketURL(t *testing.T) {
	api := fakePascal{
		only: "south carolina senate",
		hits: []pascal.Market{
			{Symbol: "SC_SENATE_2026.DEM", EventCode: "SC_SENATE_2026", Event: "South Carolina Senate Winner", Name: "Democrats"},
			{Symbol: "SC_SENATE_2026.REP", EventCode: "SC_SENATE_2026", Event: "South Carolina Senate Winner", Name: "Republicans"},
		},
		event: []pascal.Market{
			{Symbol: "SC_SENATE_2026.DEM", EventCode: "SC_SENATE_2026", Event: "South Carolina Senate Winner", Name: "Democrats"},
			{Symbol: "SC_SENATE_2026.REP", EventCode: "SC_SENATE_2026", Event: "South Carolina Senate Winner", Name: "Republicans"},
		},
		books: map[string]pascal.Book{
			"SC_SENATE_2026.DEM": {Bids: []pascal.Level{{Price: 0.40, Size: 10}}},
			"SC_SENATE_2026.REP": {Bids: []pascal.Level{{Price: 0.60, Size: 12}}},
		},
	}
	url := "https://polymarket.com/event/south-carolina-senate-2026/will-the-democrats-win-the-south-carolina-senate-race-in-2026"
	rep, err := FetchPascalOB(context.Background(), api, url)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Title != "South Carolina Senate Winner" || len(rep.Books) != 2 {
		t.Fatalf("%+v", rep)
	}
}

func TestFetchPascalOBSymbol(t *testing.T) {
	api := fakePascal{
		symbol: []pascal.Market{
			{Symbol: "FL_GOV_2026.DEM", EventCode: "FL_GOV_2026", Event: "Florida Governor Winner", Name: "Democrats", TickMin: 0.001},
		},
		books: map[string]pascal.Book{
			"FL_GOV_2026.DEM": {Bids: []pascal.Level{{Price: 0.22, Size: 6}}},
		},
	}
	rep, err := FetchPascalOB(context.Background(), api, "fl_gov_2026.dem")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Books) != 1 || rep.Books[0].Outcome != "Democrats" {
		t.Fatalf("%+v", rep)
	}
}
