package alert

import (
	"context"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

func TestBuildPosReportSortsByOverallYes(t *testing.T) {
	m := polymarket.SearchMarket{Market: polymarket.Market{Question: "Will Magdalena Andersson win?", Slug: "magdalena"}}
	wallets := []sharps.Wallet{
		{Address: "0xaaa", Name: "Alice"},
		{Address: "0xbbb", Name: "Bob"},
		{Address: "0xccc", Name: "Cara"},
		{Address: "0xddd", Name: "Dee"},
	}
	by := map[string][]polymarket.Position{
		"0xaaa": {{Outcome: "Yes", Size: 10}, {Outcome: "No", Size: 2}},
		"0xbbb": {{Outcome: "No", Size: 50}},
		"0xccc": {{Outcome: "Yes", Size: 100}},
		"0xddd": {{Outcome: "Yes", Size: 0.1}}, // dust
	}
	r := BuildPosReport("Andersson", m, wallets, by, 0)
	if r.OverallSide != "YES" || r.OverallSize != 58 { // 8 + -50 + 100
		t.Fatalf("overall=%s %v", r.OverallSide, r.OverallSize)
	}
	if len(r.Holdings) != 3 {
		t.Fatalf("holdings=%d %+v", len(r.Holdings), r.Holdings)
	}
	if r.Holdings[0].Name != "Cara" || r.Holdings[0].Outcome != "YES" {
		t.Fatalf("want largest YES first: %+v", r.Holdings)
	}
	if r.Holdings[1].Name != "Alice" {
		t.Fatalf("want Alice next: %+v", r.Holdings)
	}
	if r.Holdings[2].Name != "Bob" || r.Holdings[2].Outcome != "NO" {
		t.Fatalf("want Bob last: %+v", r.Holdings)
	}
}

func TestBuildPosReportSortsByOverallNo(t *testing.T) {
	m := polymarket.SearchMarket{Market: polymarket.Market{Question: "Q"}}
	wallets := []sharps.Wallet{
		{Address: "0xaaa", Name: "Alice"},
		{Address: "0xbbb", Name: "Bob"},
		{Address: "0xccc", Name: "Cara"},
	}
	by := map[string][]polymarket.Position{
		"0xaaa": {{Outcome: "No", Size: 80}},
		"0xbbb": {{Outcome: "No", Size: 20}},
		"0xccc": {{Outcome: "Yes", Size: 10}},
	}
	r := BuildPosReport("q", m, wallets, by, 0)
	if r.OverallSide != "NO" {
		t.Fatalf("overall=%s %v", r.OverallSide, r.OverallSize)
	}
	if r.Holdings[0].Name != "Alice" || r.Holdings[0].Outcome != "NO" {
		t.Fatalf("want largest NO first: %+v", r.Holdings)
	}
	if r.Holdings[1].Name != "Bob" {
		t.Fatalf("want Bob next: %+v", r.Holdings)
	}
	if r.Holdings[2].Name != "Cara" || r.Holdings[2].Outcome != "YES" {
		t.Fatalf("want Cara last: %+v", r.Holdings)
	}
}

func TestFormatPosReport(t *testing.T) {
	chunks := FormatPosReport(PosReport{
		Title:       "Will Magdalena Andersson be the next Prime Minister of Sweden?",
		OverallSize: 90,
		OverallSide: "YES",
		Holdings: []PosHolding{
			{Name: "Cara", Size: 100, Outcome: "YES"},
			{Name: "Bob", Size: 10, Outcome: "NO"},
		},
	})
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	got := chunks[0]
	if !strings.Contains(got, "Holdings · Will Magdalena Andersson") {
		t.Fatalf("head: %s", got)
	}
	if !strings.Contains(got, "Tracked net 90 YES") {
		t.Fatalf("net: %s", got)
	}
	if !strings.Contains(got, "100 YES  Cara") || !strings.Contains(got, "10 NO  Bob") {
		t.Fatalf("lines: %s", got)
	}

	empty := FormatPosReport(PosReport{Title: "Q"})
	if len(empty) != 1 || !strings.Contains(empty[0], "No tracked holdings") {
		t.Fatalf("%v", empty)
	}
}

type fakePosAPI struct {
	market polymarket.SearchMarket
	pos    fakePositions
}

func (f fakePosAPI) FindMarket(_ context.Context, _ string) (polymarket.SearchMarket, error) {
	return f.market, nil
}

func (f fakePosAPI) FetchPositions(ctx context.Context, opt polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	return f.pos.FetchPositions(ctx, opt)
}

func TestFetchPosReport(t *testing.T) {
	api := fakePosAPI{
		market: polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xabc", Question: "Will Magdalena Andersson win?"}},
		pos: fakePositions{
			"0xaaa|0xabc": {{Outcome: "Yes", Size: 25}},
		},
	}
	r, err := FetchPosReport(context.Background(), api, "Andersson", []sharps.Wallet{{Address: "0xAAA", Name: "Alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Holdings) != 1 || r.Holdings[0].Size != 25 || r.Title != "Will Magdalena Andersson win?" {
		t.Fatalf("%+v", r)
	}
}
