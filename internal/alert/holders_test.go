package alert

import (
	"context"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

func TestBuildHoldersReportNetsBothSides(t *testing.T) {
	m := polymarket.SearchMarket{Market: polymarket.Market{Question: "Will it happen?", Slug: "will-it"}}
	raw := []polymarket.OutcomeHolder{
		{Wallet: "0xaaa", Name: "holder4", Size: 10000, Outcome: "YES"},
		{Wallet: "0xaaa", Name: "holder4", Size: 3600, Outcome: "NO"},
		{Wallet: "0xbbb", Name: "other", Size: 2000, Outcome: "NO"},
	}
	by := map[string][]polymarket.Position{
		"0xaaa": {
			{Outcome: "Yes", Size: 10000, AvgPrice: 0.40, CurPrice: 0.64},
			{Outcome: "No", Size: 3600, AvgPrice: 0.60, CurPrice: 0.36},
		},
		"0xbbb": {{Outcome: "No", Size: 2000, AvgPrice: 0.21, CurPrice: 0.36}},
	}
	r := BuildHoldersReport("q", m, raw, by, nil)
	if len(r.Holdings) != 2 {
		t.Fatalf("holdings=%d %+v", len(r.Holdings), r.Holdings)
	}
	h := r.Holdings[0]
	if h.Name != "holder4" || h.Outcome != "YES" || h.Size != 6400 {
		t.Fatalf("net %+v", h)
	}
	if !h.HasAvg || abs(h.AvgPrice-0.40) > 1e-9 {
		t.Fatalf("avg %v", h.AvgPrice)
	}
	if r.Holdings[1].Name != "other" || r.Holdings[1].Outcome != "NO" || r.Holdings[1].Size != 2000 {
		t.Fatalf("no side %+v", r.Holdings[1])
	}
}

func TestBuildHoldersReportUsesPositionLegMissingFromList(t *testing.T) {
	m := polymarket.SearchMarket{Market: polymarket.Market{Question: "Q"}}
	raw := []polymarket.OutcomeHolder{
		{Wallet: "0xaaa", Name: "big", Size: 10000, Outcome: "YES"},
	}
	by := map[string][]polymarket.Position{
		"0xaaa": {
			{Outcome: "Yes", Size: 10000, AvgPrice: 0.50, CurPrice: 0.60},
			{Outcome: "No", Size: 9000, AvgPrice: 0.40, CurPrice: 0.40},
		},
	}
	r := BuildHoldersReport("q", m, raw, by, nil)
	if len(r.Holdings) != 1 || r.Holdings[0].Outcome != "YES" || r.Holdings[0].Size != 1000 {
		t.Fatalf("%+v", r.Holdings)
	}
}

func TestBuildHoldersReportTop10(t *testing.T) {
	m := polymarket.SearchMarket{Market: polymarket.Market{Question: "Q"}}
	var raw []polymarket.OutcomeHolder
	by := map[string][]polymarket.Position{}
	for i := 0; i < 12; i++ {
		addr := "0x" + string(rune('a'+i))
		raw = append(raw, polymarket.OutcomeHolder{Wallet: addr, Name: "h", Size: float64(100 - i), Outcome: "YES"})
		by[addr] = []polymarket.Position{{Outcome: "Yes", Size: float64(100 - i), AvgPrice: 0.32, CurPrice: 0.40}}
	}
	r := BuildHoldersReport("q", m, raw, by, nil)
	if len(r.Holdings) != holdersPerSide {
		t.Fatalf("len=%d", len(r.Holdings))
	}
	if r.Holdings[0].Size != 100 || r.Holdings[len(r.Holdings)-1].Size != 91 {
		t.Fatalf("%+v", r.Holdings)
	}
}

func TestBuildHoldersReportFallbackWithoutPrice(t *testing.T) {
	m := polymarket.SearchMarket{Market: polymarket.Market{Question: "Q"}}
	raw := []polymarket.OutcomeHolder{
		{Wallet: "0xaaa", Name: "", Size: 5000, Outcome: "YES"},
		{Wallet: "0xaaa", Size: 1000, Outcome: "NO"},
	}
	r := BuildHoldersReport("q", m, raw, nil, map[string]struct{}{"0xaaa": {}})
	if r.FailedWallets != 1 || len(r.Holdings) != 1 {
		t.Fatalf("%+v", r)
	}
	h := r.Holdings[0]
	if h.Outcome != "YES" || h.Size != 4000 || h.HasAvg || h.Name == "" {
		t.Fatalf("%+v", h)
	}
}

func TestBuildHoldersReportDropsFlat(t *testing.T) {
	m := polymarket.SearchMarket{Market: polymarket.Market{Question: "Q"}}
	raw := []polymarket.OutcomeHolder{
		{Wallet: "0xaaa", Name: "flat", Size: 10, Outcome: "YES"},
		{Wallet: "0xaaa", Name: "flat", Size: 10, Outcome: "NO"},
	}
	by := map[string][]polymarket.Position{
		"0xaaa": {
			{Outcome: "Yes", Size: 10, AvgPrice: 0.5, CurPrice: 0.5},
			{Outcome: "No", Size: 10, AvgPrice: 0.5, CurPrice: 0.5},
		},
	}
	r := BuildHoldersReport("q", m, raw, by, nil)
	if len(r.Holdings) != 0 {
		t.Fatalf("%+v", r.Holdings)
	}
}

func TestFormatHoldersReport(t *testing.T) {
	chunks := FormatHoldersReport(HoldersReport{
		Title: "Will it happen?",
		Holdings: []PosHolding{
			{Name: "holder4", Size: 6400, Outcome: "YES", AvgPrice: 0.32, HasAvg: true, CurPrice: 0.64, HasCur: true},
			{Name: "nope", Size: 1200, Outcome: "NO", AvgPrice: 0.21, HasAvg: true, CurPrice: 0.36, HasCur: true},
		},
	})
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	want := strings.Join([]string{
		"Will it happen?",
		"",
		"YES @ 64c",
		"holder4 6.4k @ 32c",
		"",
		"NO @ 36c",
		"nope 1.2k @ 21c",
	}, "\n")
	if chunks[0] != want {
		t.Fatalf("got:\n%s\nwant:\n%s", chunks[0], want)
	}
}

type fakeHoldersAPI struct {
	fakePosAPI
	holders []polymarket.OutcomeHolder
	err     error
}

func (f fakeHoldersAPI) ListHolders(context.Context, string, int) ([]polymarket.OutcomeHolder, error) {
	return f.holders, f.err
}

func TestFetchHoldersReport(t *testing.T) {
	api := fakeHoldersAPI{
		fakePosAPI: fakePosAPI{
			market: polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xabc", Question: "Will Magdalena Andersson win?"}, Active: true},
			pos: fakePositions{
				"0xaaa|0xabc": {
					{Outcome: "Yes", Size: 10000, AvgPrice: 0.32, CurPrice: 0.64},
					{Outcome: "No", Size: 3600, AvgPrice: 0.40, CurPrice: 0.36},
				},
			},
		},
		holders: []polymarket.OutcomeHolder{
			{Wallet: "0xAAA", Name: "holder4", Size: 10000, Outcome: "YES"},
			{Wallet: "0xaaa", Name: "holder4", Size: 3600, Outcome: "NO"},
		},
	}
	r, err := FetchHoldersReport(context.Background(), api, "Andersson", []sharps.Wallet{{Address: "0xtracked", Name: "Tracked"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Will Magdalena Andersson win?" || len(r.Holdings) != 1 {
		t.Fatalf("%+v", r)
	}
	h := r.Holdings[0]
	if h.Name != "holder4" || h.Outcome != "YES" || h.Size != 6400 || !h.HasAvg {
		t.Fatalf("%+v", h)
	}
}
