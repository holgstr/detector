package alert

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
		"0xaaa": {{Outcome: "Yes", Size: 10, AvgPrice: 0.60, CurPrice: 0.64}, {Outcome: "No", Size: 2, AvgPrice: 0.40, CurPrice: 0.36}},
		"0xbbb": {{Outcome: "No", Size: 50, AvgPrice: 0.21, CurPrice: 0.20}},
		"0xccc": {{Outcome: "Yes", Size: 100, AvgPrice: 0.61, CurPrice: 0.64}},
		"0xddd": {{Outcome: "Yes", Size: 0.1}}, // dust
	}
	r := BuildPosReport("Andersson", m, wallets, by, 0)
	if r.OverallSide != "YES" || r.OverallSize != 58 || !r.HasCur || r.CurPrice != 0.64 { // 8 + -50 + 100
		t.Fatalf("overall=%s %v cur=%v", r.OverallSide, r.OverallSize, r.CurPrice)
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
	cara := r.Holdings[0]
	if !cara.HasAvg || !cara.HasCur || cara.AvgPrice != 0.61 || cara.CurPrice != 0.64 {
		t.Fatalf("Cara prices %+v", cara)
	}
	alice := r.Holdings[1]
	if !alice.HasAvg || !alice.HasCur || alice.CurPrice != 0.64 {
		t.Fatalf("Alice prices %+v", alice)
	}
	if abs(alice.AvgPrice-0.60) > 1e-9 {
		t.Fatalf("Alice avg %v want 0.60", alice.AvgPrice)
	}
	bob := r.Holdings[2]
	if !bob.HasAvg || !bob.HasCur || bob.AvgPrice != 0.21 || bob.CurPrice != 0.20 {
		t.Fatalf("Bob prices %+v", bob)
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

func TestFormatPosReport(t *testing.T) {
	chunks := FormatPosReport(PosReport{
		Title:       "Will Magdalena Andersson be the next Prime Minister of Sweden?",
		OverallSize: 90,
		OverallSide: "YES",
		CurPrice:    0.64,
		HasCur:      true,
		Holdings: []PosHolding{
			{Name: "Cara", Size: 100, Outcome: "YES", AvgPrice: 0.61, HasAvg: true, CurPrice: 0.64, HasCur: true},
			{Name: "Alice", Size: 8, Outcome: "YES", AvgPrice: 0.60, HasAvg: true, CurPrice: 0.64, HasCur: true},
			{Name: "Bob", Size: 50, Outcome: "NO", AvgPrice: 0.21, HasAvg: true, CurPrice: 0.20, HasCur: true},
		},
	})
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	got := chunks[0]
	if strings.Contains(got, "Holdings") {
		t.Fatalf("should start with market name, not Holdings: %s", got)
	}
	if !strings.HasPrefix(got, "Will Magdalena Andersson") {
		t.Fatalf("head: %s", got)
	}
	if strings.Contains(got, "Tracked net") {
		t.Fatalf("no tracked-net line: %s", got)
	}
	want := strings.Join([]string{
		"Will Magdalena Andersson be the next Prime Minister of Sweden?",
		"",
		"YES @ 64c",
		"Cara 100 @ 61c",
		"Alice 8.0 @ 60c",
		"",
		"NO @ 20c",
		"Bob 50 @ 21c",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	empty := FormatPosReport(PosReport{Title: "Q"})
	if len(empty) != 1 || !strings.Contains(empty[0], "No tracked holdings") {
		t.Fatalf("%v", empty)
	}
}

type fakePosAPI struct {
	market       polymarket.SearchMarket
	candidates   []polymarket.SearchMarket
	pos          fakePositions
	allPositions map[string][]polymarket.Position // wallet -> all positions (disambiguation)
}

func (f fakePosAPI) FindMarket(_ context.Context, _ string) (polymarket.SearchMarket, error) {
	return f.market, nil
}

func (f fakePosAPI) SearchMarkets(_ context.Context, _ string) ([]polymarket.SearchMarket, error) {
	if len(f.candidates) > 0 {
		return f.candidates, nil
	}
	return []polymarket.SearchMarket{f.market}, nil
}

func (f fakePosAPI) FetchPositions(ctx context.Context, opt polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	addr := strings.ToLower(opt.User)
	markets := splitPosMarkets(opt.Market)
	if len(markets) == 0 {
		if f.allPositions != nil {
			if pos, ok := f.allPositions[addr]; ok {
				return pos, nil
			}
			return nil, nil
		}
		return f.pos.FetchPositions(ctx, opt)
	}
	want := make(map[string]struct{}, len(markets))
	for _, id := range markets {
		want[strings.ToLower(id)] = struct{}{}
	}
	if f.allPositions != nil {
		var out []polymarket.Position
		for _, p := range f.allPositions[addr] {
			if _, ok := want[strings.ToLower(strings.TrimSpace(p.ConditionID))]; ok {
				out = append(out, p)
			}
		}
		return out, nil
	}
	if len(markets) == 1 {
		return f.pos.FetchPositions(ctx, opt)
	}
	var out []polymarket.Position
	for _, id := range markets {
		pos, err := f.pos.FetchPositions(ctx, polymarket.FetchPositionsOptions{User: opt.User, Market: id})
		if err != nil {
			continue
		}
		out = append(out, pos...)
	}
	return out, nil
}

func splitPosMarkets(market string) []string {
	var out []string
	for _, part := range strings.Split(market, ",") {
		id := strings.TrimSpace(part)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func TestPickMarketByTrackedPositions(t *testing.T) {
	candidates := []polymarket.SearchMarket{
		{Market: polymarket.Market{ConditionID: "0xhighvol", Question: "Will Flavio win Serie A?", Slug: "flavio-serie-a"}, Volume24hr: 500000, Active: true},
		{Market: polymarket.Market{ConditionID: "0xheld", Question: "Will Flavio be next PM of Italy?", Slug: "flavio-pm"}, Volume24hr: 1000, Active: true},
	}
	api := fakePosAPI{
		allPositions: map[string][]polymarket.Position{
			"0xaaa": {{ConditionID: "0xheld", Outcome: "Yes", Size: 50}},
			"0xbbb": {{ConditionID: "0xheld", Outcome: "No", Size: 20}},
		},
	}
	got, err := pickMarketByTrackedPositions(context.Background(), api, candidates, []sharps.Wallet{
		{Address: "0xaaa", Name: "Alice"},
		{Address: "0xbbb", Name: "Bob"},
	})
	if err != nil || got.Market.ConditionID != "0xheld" {
		t.Fatalf("want held market, got %+v err=%v", got, err)
	}
}

func TestFilterPositionsByMarket(t *testing.T) {
	got := filterPositionsByMarket(map[string][]polymarket.Position{
		"0xaaa": {
			{ConditionID: "0xheld", Outcome: "Yes", Size: 10},
			{ConditionID: "0xhighvol", Outcome: "No", Size: 3},
		},
		"0xbbb": {{ConditionID: "0xhighvol", Outcome: "Yes", Size: 1}},
	}, "0xHELD")
	if len(got) != 1 || len(got["0xaaa"]) != 1 || got["0xaaa"][0].Size != 10 {
		t.Fatalf("%+v", got)
	}
}

func TestFetchPosReportPrefersTrackedMarket(t *testing.T) {
	highVol := polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xhighvol", Question: "Will Flavio win Serie A?"}, Volume24hr: 500000, Active: true}
	held := polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xheld", Question: "Will Flavio be next PM of Italy?"}, Volume24hr: 1000, Active: true}
	api := fakePosAPI{
		candidates: []polymarket.SearchMarket{highVol, held},
		allPositions: map[string][]polymarket.Position{
			"0xaaa": {{ConditionID: "0xheld", Outcome: "Yes", Size: 25}},
		},
		pos: fakePositions{
			"0xaaa|0xheld": {{Outcome: "Yes", Size: 25}},
		},
	}
	r, err := FetchPosReport(context.Background(), api, "Flavio", []sharps.Wallet{{Address: "0xAAA", Name: "Alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Will Flavio be next PM of Italy?" || len(r.Holdings) != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestFetchPosReport(t *testing.T) {
	api := fakePosAPI{
		market: polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xabc", Question: "Will Magdalena Andersson win?"}, Active: true},
		pos: fakePositions{
			"0xaaa|0xabc": {{Outcome: "Yes", Size: 25, AvgPrice: 0.40, CurPrice: 0.50}},
		},
	}
	r, err := FetchPosReport(context.Background(), api, "Andersson", []sharps.Wallet{{Address: "0xAAA", Name: "Alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Holdings) != 1 || r.Holdings[0].Size != 25 || r.Title != "Will Magdalena Andersson win?" {
		t.Fatalf("%+v", r)
	}
	h := r.Holdings[0]
	if !h.HasAvg || !h.HasCur || h.AvgPrice != 0.40 || h.CurPrice != 0.50 {
		t.Fatalf("prices %+v", h)
	}
	if !r.HasCur || r.CurPrice != 0.50 {
		t.Fatalf("market px %+v", r)
	}
}

type countingPosAPI struct {
	fakePosAPI
	calls    atomic.Int32
	inflight atomic.Int32
	max      atomic.Int32
	gate     <-chan struct{}
}

func (c *countingPosAPI) FetchPositions(ctx context.Context, opt polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	c.calls.Add(1)
	n := c.inflight.Add(1)
	for {
		old := c.max.Load()
		if n <= old || c.max.CompareAndSwap(old, n) {
			break
		}
	}
	defer c.inflight.Add(-1)
	if c.gate != nil {
		select {
		case <-c.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.fakePosAPI.FetchPositions(ctx, opt)
}

func TestFetchPosReportOneRoundTripForAmbiguousMarket(t *testing.T) {
	highVol := polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xhighvol", Question: "Will Flavio win Serie A?"}, Volume24hr: 500000, Active: true}
	held := polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xheld", Question: "Will Flavio be next PM of Italy?"}, Volume24hr: 1000, Active: true}
	wallets := []sharps.Wallet{
		{Address: "0xaaa", Name: "Alice"},
		{Address: "0xbbb", Name: "Bob"},
		{Address: "0xccc", Name: "Cara"},
	}
	api := &countingPosAPI{fakePosAPI: fakePosAPI{
		candidates: []polymarket.SearchMarket{highVol, held},
		allPositions: map[string][]polymarket.Position{
			"0xaaa": {{ConditionID: "0xheld", Outcome: "Yes", Size: 25}},
			"0xbbb": {{ConditionID: "0xheld", Outcome: "No", Size: 10}},
			"0xccc": {{ConditionID: "0xhighvol", Outcome: "Yes", Size: 1}},
		},
	}}
	r, err := FetchPosReport(context.Background(), api, "Flavio", wallets)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Will Flavio be next PM of Italy?" || len(r.Holdings) != 2 {
		t.Fatalf("%+v", r)
	}
	if got := api.calls.Load(); got != int32(len(wallets)) {
		t.Fatalf("position fetches=%d want %d (one round, not resolve+report)", got, len(wallets))
	}
}

func TestFetchPosReportFetchesWalletsInParallel(t *testing.T) {
	const n = 20
	gate := make(chan struct{})
	inner := fakePosAPI{
		market:       polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xabc", Question: "Will Magdalena Andersson win?"}, Active: true},
		allPositions: make(map[string][]polymarket.Position, n),
	}
	wallets := make([]sharps.Wallet, n)
	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("0x%040x", i+1)
		wallets[i] = sharps.Wallet{Address: addr, Name: fmt.Sprintf("W%d", i)}
		inner.allPositions[addr] = []polymarket.Position{{ConditionID: "0xabc", Outcome: "Yes", Size: float64(i + 10)}}
	}
	api := &countingPosAPI{fakePosAPI: inner, gate: gate}

	done := make(chan struct{})
	var (
		rep PosReport
		err error
	)
	go func() {
		rep, err = FetchPosReport(context.Background(), api, "Andersson", wallets)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for api.max.Load() < int32(posFetchWorkers) {
		if time.Now().After(deadline) {
			close(gate)
			<-done
			t.Fatalf("max inflight=%d want %d", api.max.Load(), posFetchWorkers)
		}
		time.Sleep(time.Millisecond)
	}
	close(gate)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if api.calls.Load() != n {
		t.Fatalf("calls=%d want %d", api.calls.Load(), n)
	}
	if api.max.Load() != int32(posFetchWorkers) {
		t.Fatalf("max inflight=%d want %d", api.max.Load(), posFetchWorkers)
	}
	if len(rep.Holdings) != n {
		t.Fatalf("holdings=%d", len(rep.Holdings))
	}
}
