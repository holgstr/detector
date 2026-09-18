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
			{
				Outcome: "No",
				Tick:    0.01,
				Bids:    []polymarket.BookLevel{{Price: 0.57, Size: 9}},
				Asks:    []polymarket.BookLevel{{Price: 0.58, Size: 8}},
			},
		},
	})
	want := strings.Join([]string{
		"Aliens?",
		"",
		"46¢  5.0",
		"44¢   20",
		"43¢   10",
		"- - -",
		"42¢   50",
		"40¢  100",
		"39¢   10",
	}, "\n")
	if text != want {
		t.Fatalf("got:\n%s\nwant:\n%s", text, want)
	}
	if strings.Contains(text, "Asks") || strings.Contains(text, "Bids") || strings.Contains(text, "YES") || strings.Contains(text, "NO") || strings.Contains(text, "57¢") || strings.Contains(text, "45¢") || strings.Contains(text, "41¢") {
		t.Fatalf("labels, no-side, or empty ticks leaked: %q", text)
	}
}

func TestFormatOBReportNonBinaryKeepsNames(t *testing.T) {
	text := FormatOBReport(OBReport{
		Title: "Who wins?",
		Books: []polymarket.OutcomeBook{
			{Outcome: "Alice", Tick: 0.01, Bids: []polymarket.BookLevel{{Price: 0.20, Size: 1}}},
			{Outcome: "Bob", Tick: 0.01, Asks: []polymarket.BookLevel{{Price: 0.30, Size: 2}}},
		},
	})
	if !strings.Contains(text, "\nALICE\n") || !strings.Contains(text, "\nBOB\n") {
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
