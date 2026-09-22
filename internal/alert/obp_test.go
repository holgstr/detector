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
}

func (f fakePascal) TextSearch(context.Context, string) ([]pascal.Market, error) {
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
