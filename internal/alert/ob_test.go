package alert

import (
	"context"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
)

type fakeOBAPI struct {
	hit   polymarket.SearchMarket
	books []polymarket.OutcomeBook
	err   error
}

func (f fakeOBAPI) FindMarket(context.Context, string) (polymarket.SearchMarket, error) {
	return f.hit, f.err
}

func (f fakeOBAPI) FetchOutcomeBooks(context.Context, string) ([]polymarket.OutcomeBook, error) {
	return f.books, nil
}

func TestFormatOBReport(t *testing.T) {
	text := FormatOBReport(OBReport{
		Title: "Aliens?",
		Books: []polymarket.OutcomeBook{
			{
				Outcome: "Yes",
				Tick:    0.01,
				Bids:    []polymarket.BookLevel{{Price: 0.42, Size: 50}, {Price: 0.41, Size: 0}, {Price: 0.40, Size: 100}, {Price: 0.39, Size: 10}},
				Asks:    []polymarket.BookLevel{{Price: 0.43, Size: 10}, {Price: 0.44, Size: 20}, {Price: 0.45, Size: 0}, {Price: 0.46, Size: 5}},
			},
		},
	})
	if !strings.Contains(text, "Order book · Aliens?") {
		t.Fatalf("%q", text)
	}
	if !strings.Contains(text, "YES") || !strings.Contains(text, "Asks") || !strings.Contains(text, "Bids") {
		t.Fatalf("%q", text)
	}
	if !strings.Contains(text, "42¢ 50") || !strings.Contains(text, "41¢ —") || !strings.Contains(text, "43¢ 10") {
		t.Fatalf("%q", text)
	}
}

func TestFetchOBReport(t *testing.T) {
	api := fakeOBAPI{
		hit: polymarket.SearchMarket{Market: polymarket.Market{Question: "Aliens?", ConditionID: "0xabc", Slug: "aliens"}},
		books: []polymarket.OutcomeBook{
			{Outcome: "Yes", Tick: 0.01, Bids: []polymarket.BookLevel{{Price: 0.4, Size: 1}}},
		},
	}
	rep, err := FetchOBReport(context.Background(), api, "aliens")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Title != "Aliens?" || len(rep.Books) != 1 {
		t.Fatalf("%+v", rep)
	}
}
