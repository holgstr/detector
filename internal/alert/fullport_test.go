package alert

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

func TestBuildFullPortReportNetsAndCountsSide(t *testing.T) {
	by := map[string][]polymarket.Position{
		"0xalice": {
			{ConditionID: "m", Title: "Market A", Outcome: "Yes", Size: 200, AvgPrice: 0.40, CurPrice: 0.50},
			{ConditionID: "s", Title: "Game", EventSlug: "nfl-atl-pit-2026-09-13", Outcome: "Yes", Size: 5000, AvgPrice: 0.5, CurPrice: 0.5},
		},
		"0xbob": {
			{ConditionID: "m", Title: "Market A", Outcome: "Yes", Size: 200, AvgPrice: 0.60, CurPrice: 0.50},
		},
		"0xcarol": {
			{ConditionID: "m", Title: "Market A", Outcome: "No", Size: 100, AvgPrice: 0.30, CurPrice: 0.50},
		},
		"0xflat": {
			{ConditionID: "m", Title: "Market A", Outcome: "Yes", Size: 50, AvgPrice: 0.50, CurPrice: 0.50},
			{ConditionID: "m", Title: "Market A", Outcome: "No", Size: 50, AvgPrice: 0.50, CurPrice: 0.50},
		},
		"0xtiny": {
			{ConditionID: "tiny", Title: "Tiny", Outcome: "Yes", Size: 100, AvgPrice: 0.40, CurPrice: 0.40},
		},
		"0xe": {{ConditionID: "f", Title: "Market F", Outcome: "No", Size: 1000, AvgPrice: 0.20, CurPrice: 0.20}},
		"0xf": {{ConditionID: "f", Title: "Market F", Outcome: "No", Size: 1000, AvgPrice: 0.20, CurPrice: 0.20}},
		"0xg": {{ConditionID: "f", Title: "Market F", Outcome: "No", Size: 1000, AvgPrice: 0.20, CurPrice: 0.20}},
	}
	r := BuildFullPortReport(context.Background(), fakeSports{}, by, false, 1, 0)
	if r.Failed != 1 || len(r.Holdings) != 2 {
		t.Fatalf("%+v", r)
	}
	f := r.Holdings[0]
	if f.Title != "Market F" || f.Outcome != "NO" || f.Size != 3000 || f.Wallets != 3 {
		t.Fatalf("F %+v", f)
	}
	a := r.Holdings[1]
	if a.Title != "Market A" || a.Outcome != "YES" || a.Size != 300 || a.Wallets != 2 {
		t.Fatalf("A %+v", a)
	}
	if !a.HasAvg || abs(a.AvgPrice-130.0/300) > 1e-9 || a.CurPrice != 0.50 || abs(a.MarketValue-150) > 1e-9 {
		t.Fatalf("A px %+v", a)
	}

	chunks := FormatFullPortReport(r)
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	got := chunks[0]
	if !strings.HasPrefix(got, "All tracked\n") {
		t.Fatalf("head: %s", got)
	}
	if !strings.Contains(got, "3k NO  Market F (3) | 20c → 20c") {
		t.Fatalf("F line: %s", got)
	}
	if !strings.Contains(got, "300 YES  Market A (2) | 43c → 50c") {
		t.Fatalf("A line: %s", got)
	}
	if strings.Contains(got, "alice") || strings.Contains(got, "bob") || strings.Contains(got, "carol") || strings.Contains(got, "Game") || strings.Contains(got, "Tiny") {
		t.Fatalf("leaked: %s", got)
	}
	if !strings.Contains(got, "(1 wallet lookups failed.)") {
		t.Fatalf("failed note: %s", got)
	}
}

func TestFormatFullPortReportEmpty(t *testing.T) {
	got := FormatFullPortReport(FullPortReport{})
	if len(got) != 1 || !strings.Contains(got[0], "No open non-sports holdings of $100+") {
		t.Fatalf("%v", got)
	}
}

func TestFetchFullPortReport(t *testing.T) {
	api := fakePortAPI{
		pos: map[string][]polymarket.Position{
			"0xaaa": {{ConditionID: "a", Title: "Market A", Outcome: "Yes", Size: 400, AvgPrice: 0.40, CurPrice: 0.50}},
			"0xbbb": {{ConditionID: "a", Title: "Market A", Outcome: "Yes", Size: 200, AvgPrice: 0.50, CurPrice: 0.50}},
		},
	}
	wallets := []sharps.Wallet{
		{Address: "0xAAA", Name: "Alice"},
		{Address: "0xBBB", Name: "Bob"},
	}
	r, err := FetchFullPortReport(context.Background(), api, wallets, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Holdings) != 1 || r.Holdings[0].Size != 600 || r.Holdings[0].Wallets != 2 || r.Holdings[0].Title != "Market A" {
		t.Fatalf("%+v", r)
	}
}

type errOnePortAPI struct {
	fakePortAPI
	bad string
}

func (f errOnePortAPI) FetchPositions(ctx context.Context, opt polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	if strings.EqualFold(opt.User, f.bad) {
		return nil, errors.New("positions down")
	}
	return f.fakePortAPI.FetchPositions(ctx, opt)
}

func TestFetchFullPortReportSkipsFailedWallet(t *testing.T) {
	api := errOnePortAPI{
		bad: "0xbbb",
		fakePortAPI: fakePortAPI{
			pos: map[string][]polymarket.Position{
				"0xaaa": {{ConditionID: "a", Title: "Market A", Outcome: "Yes", Size: 400, CurPrice: 0.50}},
				"0xbbb": {{ConditionID: "a", Title: "Market A", Outcome: "Yes", Size: 9999, CurPrice: 0.50}},
			},
		},
	}
	r, err := FetchFullPortReport(context.Background(), api, []sharps.Wallet{
		{Address: "0xaaa", Name: "Alice"},
		{Address: "0xbbb", Name: "Bob"},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Failed != 1 || len(r.Holdings) != 1 || r.Holdings[0].Wallets != 1 || r.Holdings[0].Size != 400 {
		t.Fatalf("%+v", r)
	}
}

type countingSports struct {
	fakeSports
	calls int
}

func (c *countingSports) EventIsSports(ctx context.Context, slug string) (bool, error) {
	c.calls++
	return c.fakeSports.EventIsSports(ctx, slug)
}

func TestFullPortLimitSkipsTailLookups(t *testing.T) {
	by := make(map[string][]polymarket.Position)
	var rows []polymarket.Position
	for i := 0; i < 40; i++ {
		rows = append(rows, polymarket.Position{
			ConditionID: fmt.Sprintf("m%d", i),
			Title:       fmt.Sprintf("Market %02d", i),
			EventSlug:   fmt.Sprintf("election-%02d", i),
			Outcome:     "Yes",
			Size:        float64(1000 - i*10),
			CurPrice:    0.50,
		})
	}
	// Obvious sports sit at the top and must not burn a lookup or take a slot.
	rows = append(rows, polymarket.Position{
		ConditionID: "nfl",
		Title:       "Game",
		EventSlug:   "nfl-atl-pit-2026-09-13",
		Outcome:     "Yes",
		Size:        50000,
		CurPrice:    0.50,
	})
	by["0xaaa"] = rows
	api := &countingSports{}
	r := BuildFullPortReport(context.Background(), api, by, false, 0, 5)
	if len(r.Holdings) != 5 || !r.More || r.Holdings[0].Title != "Market 00" {
		t.Fatalf("%+v calls=%d", r, api.calls)
	}
	if api.calls > 5+fullPortSportsSlack || api.calls >= 40 {
		t.Fatalf("calls=%d", api.calls)
	}
	got := FormatFullPortReport(r)
	if len(got) != 1 || !strings.Contains(got[0], "(top 5 by market value)") || strings.Contains(got[0], "Game") || strings.Contains(got[0], "Market 10") {
		t.Fatalf("calls=%d\n%s", api.calls, got)
	}

	short := BuildFullPortReport(context.Background(), fakeSports{}, map[string][]polymarket.Position{
		"0xaaa": {{ConditionID: "a", Title: "Only", EventSlug: "election-a", Outcome: "Yes", Size: 400, CurPrice: 0.5}},
	}, false, 0, FullPortDefaultLimit)
	if short.More || len(short.Holdings) != 1 {
		t.Fatalf("%+v", short)
	}
	if strings.Contains(FormatFullPortReport(short)[0], "top ") {
		t.Fatalf("%s", FormatFullPortReport(short)[0])
	}
}
