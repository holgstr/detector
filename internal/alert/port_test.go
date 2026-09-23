package alert

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

func TestResolvePortWallet(t *testing.T) {
	api := fakeUsers{
		byName: map[string][]polymarket.UserProfile{
			"newsharp": {{Address: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "NewSharp"}},
		},
	}
	if _, msg := ResolvePortWallet(context.Background(), api, ""); msg != PortUsage {
		t.Fatalf("empty: %q", msg)
	}
	if _, msg := ResolvePortWallet(context.Background(), api, "all"); msg != PortUsage {
		t.Fatalf("all: %q", msg)
	}
	w, msg := ResolvePortWallet(context.Background(), api, "Flip")
	if msg != "" || w.Name != "Flipadelphia" {
		t.Fatalf("Flip: %+v %q", w, msg)
	}
	w, msg = ResolvePortWallet(context.Background(), api, "NewSharp")
	if msg != "" || w.Name != "NewSharp" || w.Address != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("untracked: %+v %q", w, msg)
	}
	if _, msg := ResolvePortWallet(context.Background(), api, "no-such-trader"); !strings.Contains(msg, "No Polymarket user") {
		t.Fatalf("missing: %q", msg)
	}
}

func TestNetPortHoldingsMergeAvgAndValue(t *testing.T) {
	// 100 Yes @ 0.60 + 40 No @ 0.40 → leftover 60 Yes; merge avg (cash-no)/net = 0.60; cur 0.64 → +4c; value 38.4
	got := NetPortHoldings([]polymarket.Position{
		{ConditionID: "a", Title: "Market A", Outcome: "Yes", Size: 100, AvgPrice: 0.60, CurPrice: 0.64},
		{ConditionID: "a", Outcome: "No", Size: 40, AvgPrice: 0.40, CurPrice: 0.36},
		{ConditionID: "b", Title: "Market B", Outcome: "No", Size: 17000, AvgPrice: 0.21, CurPrice: 0.20},
		{ConditionID: "flat", Title: "Flat", Outcome: "Yes", Size: 10, AvgPrice: 0.5, CurPrice: 0.5},
		{ConditionID: "flat", Outcome: "No", Size: 10, AvgPrice: 0.5, CurPrice: 0.5},
		{ConditionID: "dust", Title: "Dust", Outcome: "Yes", Size: 0.2, AvgPrice: 0.9, CurPrice: 0.9},
	})
	byTitle := map[string]PortHolding{}
	for _, h := range got {
		byTitle[h.Title] = h
	}
	if _, ok := byTitle["Flat"]; ok {
		t.Fatalf("flat should drop: %+v", got)
	}
	if _, ok := byTitle["Dust"]; ok {
		t.Fatalf("dust should drop: %+v", got)
	}
	a := byTitle["Market A"]
	if a.Outcome != "YES" || a.Size != 60 || !a.HasAvg || !a.HasCur {
		t.Fatalf("A=%+v", a)
	}
	if abs(a.AvgPrice-0.60) > 1e-9 || a.CurPrice != 0.64 || abs(a.MarketValue-38.4) > 1e-9 {
		t.Fatalf("A prices %+v", a)
	}
	b := byTitle["Market B"]
	if b.Outcome != "NO" || b.Size != 17000 || b.CurPrice != 0.20 {
		t.Fatalf("B=%+v", b)
	}
}

func TestBuildPortReportSortsByMarketValueDropsSports(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	pos := []polymarket.Position{
		{ConditionID: "a", Title: "Market A", Outcome: "Yes", Size: 8600, AvgPrice: 0.61, CurPrice: 0.64},
		{ConditionID: "b", Title: "Market B", EventSlug: "nfl-atl-pit-2026-09-13", Outcome: "Yes", Size: 99999, AvgPrice: 0.5, CurPrice: 0.9},
		{ConditionID: "c", Title: "Market C", Outcome: "No", Size: 3400, AvgPrice: 0.21, CurPrice: 0.20},
	}
	r := BuildPortReport(context.Background(), fakeSports{}, w, pos, false)
	if r.Name != "Alice" || len(r.Holdings) != 2 {
		t.Fatalf("%+v", r)
	}
	if r.Holdings[0].Title != "Market A" || r.Holdings[1].Title != "Market C" {
		t.Fatalf("sort %+v", r.Holdings)
	}
}

func TestBuildPortReportDropsSub100Value(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	pos := []polymarket.Position{
		{ConditionID: "keep", Title: "Keep", Outcome: "Yes", Size: 200, AvgPrice: 0.50, CurPrice: 0.50},   // $100
		{ConditionID: "drop", Title: "Drop", Outcome: "Yes", Size: 199.8, AvgPrice: 0.50, CurPrice: 0.50}, // $99.90
		{ConditionID: "nopx", Title: "NoPx", Outcome: "Yes", Size: 5000, AvgPrice: 0.40},                  // no live px
	}
	r := BuildPortReport(context.Background(), fakeSports{}, w, pos, false)
	if len(r.Holdings) != 1 || r.Holdings[0].Title != "Keep" {
		t.Fatalf("want only $100+ Keep, got %+v", r.Holdings)
	}
}

func TestBuildPortReportHidesWhenSportsLookupFails(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	pos := []polymarket.Position{
		{ConditionID: "a", Title: "Mystery", EventSlug: "mystery-event", Outcome: "Yes", Size: 50, AvgPrice: 0.5, CurPrice: 0.5},
	}
	r := BuildPortReport(context.Background(), failSports{}, w, pos, false)
	if len(r.Holdings) != 0 {
		t.Fatalf("unknown events must not leak when Gamma is down: %+v", r.Holdings)
	}
}

func TestFormatPortReport(t *testing.T) {
	chunks := FormatPortReport(PortReport{
		Name: "Alice",
		Holdings: []PortHolding{
			{Title: "Market A", Size: 8600, Outcome: "YES", CurPrice: 0.64, AvgPrice: 0.61, HasCur: true, HasAvg: true},
			{Title: "Market B", Size: 3400, Outcome: "NO", CurPrice: 0.20, AvgPrice: 0.21, HasCur: true, HasAvg: true},
			{Title: "Market C", Size: 200, Outcome: "YES", AvgPrice: 0.40, HasAvg: true},
			{Title: "Market D", Size: 300, Outcome: "NO", CurPrice: 0.55, HasCur: true},
		},
	})
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	got := chunks[0]
	if !strings.HasPrefix(got, "Alice\n") || strings.Contains(got, "Portfolio") {
		t.Fatalf("head: %s", got)
	}
	if !strings.Contains(got, "8.6k YES  Market A | 61c → 64c") {
		t.Fatalf("A: %s", got)
	}
	if !strings.Contains(got, "3.4k NO  Market B | 21c → 20c") {
		t.Fatalf("B: %s", got)
	}
	if !strings.Contains(got, "200 YES  Market C | 40c") {
		t.Fatalf("C: %s", got)
	}
	if !strings.Contains(got, "300 NO  Market D | 55c") {
		t.Fatalf("D: %s", got)
	}

	empty := FormatPortReport(PortReport{Name: "Alice"})
	if len(empty) != 1 || !strings.Contains(empty[0], "No open non-sports holdings of $100+") {
		t.Fatalf("%v", empty)
	}

	top := FormatPortReport(PortReport{
		Name:  "Alice",
		Limit: 2,
		Holdings: []PortHolding{
			{Title: "Market A", Size: 8600, Outcome: "YES", CurPrice: 0.64, AvgPrice: 0.61, HasCur: true, HasAvg: true},
			{Title: "Market B", Size: 3400, Outcome: "NO", CurPrice: 0.20, AvgPrice: 0.21, HasCur: true, HasAvg: true},
			{Title: "Market C", Size: 200, Outcome: "YES", AvgPrice: 0.40, HasAvg: true},
		},
	})
	if len(top) != 1 {
		t.Fatalf("top chunks=%d", len(top))
	}
	if !strings.Contains(top[0], "(top 2 of 3 by market value)") {
		t.Fatalf("top note: %s", top[0])
	}
	if !strings.Contains(top[0], "Market A") || !strings.Contains(top[0], "Market B") || strings.Contains(top[0], "Market C") {
		t.Fatalf("top rows: %s", top[0])
	}
}

type fakePortAPI struct {
	pos    map[string][]polymarket.Position
	sports map[string]bool
	err    error
}

func (f fakePortAPI) FetchPositions(_ context.Context, opt polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	if f.err != nil {
		return nil, f.err
	}
	rows := f.pos[strings.ToLower(opt.User)]
	start := opt.Offset
	if start >= len(rows) {
		return nil, nil
	}
	end := start + opt.Limit
	if opt.Limit <= 0 || end > len(rows) {
		end = len(rows)
	}
	return rows[start:end], nil
}

func (f fakePortAPI) EventIsSports(_ context.Context, slug string) (bool, error) {
	return f.sports[slug], nil
}

func TestFetchPortReport(t *testing.T) {
	api := fakePortAPI{
		pos: map[string][]polymarket.Position{
			"0xaaa": {
				{ConditionID: "a", Title: "Market A", Outcome: "Yes", Size: 250, AvgPrice: 0.40, CurPrice: 0.50},
				{ConditionID: "s", Title: "Game", EventSlug: "nba", Slug: "nba-foo", Outcome: "Yes", Size: 100, AvgPrice: 0.5, CurPrice: 0.5},
			},
		},
	}
	r, err := FetchPortReport(context.Background(), api, sharps.Wallet{Address: "0xAAA", Name: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Holdings) != 1 || r.Holdings[0].Title != "Market A" || r.Holdings[0].Size != 250 {
		t.Fatalf("%+v", r)
	}
}

type pagingPortAPI struct {
	mu      sync.Mutex
	rows    []polymarket.Position
	offsets []int
	sortBy  string
	sortDir string
}

func (p *pagingPortAPI) FetchPositions(_ context.Context, opt polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	p.mu.Lock()
	p.offsets = append(p.offsets, opt.Offset)
	p.sortBy = opt.SortBy
	p.sortDir = opt.SortDirection
	p.mu.Unlock()
	start := opt.Offset
	if start >= len(p.rows) {
		return nil, nil
	}
	end := start + opt.Limit
	if opt.Limit <= 0 || end > len(p.rows) {
		end = len(p.rows)
	}
	out := make([]polymarket.Position, end-start)
	copy(out, p.rows[start:end])
	return out, nil
}

func (p *pagingPortAPI) EventIsSports(context.Context, string) (bool, error) {
	return false, nil
}

func TestFetchPortReportOnePage(t *testing.T) {
	api := &pagingPortAPI{
		rows: []polymarket.Position{
			{ConditionID: "a", Title: "A", Outcome: "Yes", Size: 400, CurPrice: 0.5},
		},
	}
	r, err := FetchPortReport(context.Background(), api, sharps.Wallet{Address: "0xaaa", Name: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Holdings) != 1 {
		t.Fatalf("%+v", r)
	}
	if len(api.offsets) != 1 || api.offsets[0] != 0 {
		t.Fatalf("offsets=%v (small books should be one request)", api.offsets)
	}
	if api.sortBy != "CURRENT" || api.sortDir != "DESC" {
		t.Fatalf("sort %s %s", api.sortBy, api.sortDir)
	}
}

func TestFetchPortReportPagesInParallel(t *testing.T) {
	rows := make([]polymarket.Position, portPageSize+150)
	for i := range rows {
		rows[i] = polymarket.Position{
			ConditionID: fmt.Sprintf("c%d", i),
			Title:       fmt.Sprintf("M%d", i),
			Outcome:     "Yes",
			Size:        400,
			CurPrice:    0.5,
		}
	}
	api := &pagingPortAPI{rows: rows}
	r, err := FetchPortReport(context.Background(), api, sharps.Wallet{Address: "0xaaa", Name: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Holdings) != len(rows) {
		t.Fatalf("holdings=%d want %d", len(r.Holdings), len(rows))
	}
	if r.Truncated {
		t.Fatal("short last page should not truncate")
	}
	got := map[int]int{}
	for _, off := range api.offsets {
		got[off]++
	}
	if got[0] != 1 {
		t.Fatalf("first page calls=%d offsets=%v", got[0], api.offsets)
	}
	for off := portPageSize; off < portMaxOffset; off += portPageSize {
		if got[off] != 1 {
			t.Fatalf("missing parallel page %d: %v", off, api.offsets)
		}
	}
	if len(api.offsets) != portMaxOffset/portPageSize {
		t.Fatalf("want %d page fetches, got %d %v", portMaxOffset/portPageSize, len(api.offsets), api.offsets)
	}
}

type fullBookPortAPI struct {
	calls atomic.Int32
}

func (f *fullBookPortAPI) FetchPositions(_ context.Context, opt polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	f.calls.Add(1)
	out := make([]polymarket.Position, portPageSize)
	for i := range out {
		out[i] = polymarket.Position{
			ConditionID: fmt.Sprintf("%d-%d", opt.Offset, i),
			Title:       "M",
			Outcome:     "Yes",
			Size:        400,
			CurPrice:    0.4,
		}
	}
	return out, nil
}

func (f *fullBookPortAPI) EventIsSports(context.Context, string) (bool, error) {
	return false, nil
}

func TestFetchPortReportTruncated(t *testing.T) {
	api := &fullBookPortAPI{}
	r, err := FetchPortReport(context.Background(), api, sharps.Wallet{Address: "0xaaa", Name: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated {
		t.Fatal("expected truncated")
	}
	if len(r.Holdings) != portMaxOffset {
		t.Fatalf("holdings=%d", len(r.Holdings))
	}
	if api.calls.Load() != int32(portMaxOffset/portPageSize) {
		t.Fatalf("calls=%d", api.calls.Load())
	}
}

type countingSportsAPI struct {
	lookups atomic.Int32
}

func (c *countingSportsAPI) FetchPositions(context.Context, polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	return []polymarket.Position{
		{ConditionID: "a", Title: "A", EventSlug: "event-a", Outcome: "Yes", Size: 400, CurPrice: 0.5},
		{ConditionID: "b", Title: "B", EventSlug: "event-b", Outcome: "Yes", Size: 400, CurPrice: 0.5},
		{ConditionID: "c", Title: "C", EventSlug: "event-a", Outcome: "Yes", Size: 400, CurPrice: 0.5},
		{ConditionID: "d", Title: "D", EventSlug: "nfl-atl-pit-2026-09-13", Outcome: "Yes", Size: 400, CurPrice: 0.5},
	}, nil
}

func (c *countingSportsAPI) EventIsSports(_ context.Context, slug string) (bool, error) {
	c.lookups.Add(1)
	return slug == "event-b", nil
}

func TestFetchPortReportSportsLookupsDeduped(t *testing.T) {
	api := &countingSportsAPI{}
	r, err := FetchPortReport(context.Background(), api, sharps.Wallet{Address: "0xaaa", Name: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if api.lookups.Load() != 2 {
		t.Fatalf("lookups=%d want 2 unique non-sports-looking slugs", api.lookups.Load())
	}
	if len(r.Holdings) != 2 {
		t.Fatalf("holdings=%v", r.Holdings)
	}
}
