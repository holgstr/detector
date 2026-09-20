package alert

import (
	"context"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

type fakeOBAPI struct {
	fakePosAPI
	books []polymarket.OutcomeBook
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
		"46¢    5",
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
		fakePosAPI: fakePosAPI{
			market: polymarket.SearchMarket{Market: polymarket.Market{Question: "Aliens?", ConditionID: "0xabc", Slug: "aliens"}, Active: true},
		},
		books: []polymarket.OutcomeBook{
			{Outcome: "Yes", Tick: 0.01, Bids: []polymarket.BookLevel{{Price: 0.4, Size: 1}}},
		},
	}
	rep, err := FetchOBReport(context.Background(), api, "aliens", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Title != "Aliens?" || len(rep.Books) != 1 {
		t.Fatalf("%+v", rep)
	}
}

func TestFetchOBReportPrefersTrackedExposure(t *testing.T) {
	highVol := polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xhighvol", Question: "Will Flavio win Serie A?"}, Volume24hr: 500000, Active: true}
	held := polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xheld", Question: "Will Flavio be next PM of Italy?"}, Volume24hr: 1000, Active: true}
	api := fakeOBAPI{
		fakePosAPI: fakePosAPI{
			candidates: []polymarket.SearchMarket{highVol, held},
			allPositions: map[string][]polymarket.Position{
				"0xaaa": {{ConditionID: "0xheld", Outcome: "Yes", Size: 400}},
			},
		},
		books: []polymarket.OutcomeBook{
			{Outcome: "Yes", Tick: 0.01, Bids: []polymarket.BookLevel{{Price: 0.4, Size: 1}}},
		},
	}
	rep, err := FetchOBReport(context.Background(), api, "Flavio", []sharps.Wallet{{Address: "0xaaa", Name: "Alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Title != "Will Flavio be next PM of Italy?" {
		t.Fatalf("%+v", rep)
	}
}

func TestFetchOBReportFallsBackToVolume(t *testing.T) {
	highVol := polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xhighvol", Question: "Will Flavio win Serie A?"}, Volume24hr: 500000, Volume: 2e6, Active: true}
	thin := polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xthin", Question: "Will Flavio win a local race?"}, Volume24hr: 10, Volume: 20, Active: true}
	api := fakeOBAPI{
		fakePosAPI: fakePosAPI{
			candidates: []polymarket.SearchMarket{thin, highVol},
		},
		books: []polymarket.OutcomeBook{
			{Outcome: "Yes", Tick: 0.01, Bids: []polymarket.BookLevel{{Price: 0.4, Size: 1}}},
		},
	}
	rep, err := FetchOBReport(context.Background(), api, "Flavio", []sharps.Wallet{{Address: "0xaaa", Name: "Alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Title != "Will Flavio win Serie A?" {
		t.Fatalf("%+v", rep)
	}
}
