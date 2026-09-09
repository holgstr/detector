package alert

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
)

type fakeSports map[string]bool

func (f fakeSports) EventIsSports(_ context.Context, slug string) (bool, error) {
	if v, ok := f[slug]; ok {
		return v, nil
	}
	return false, nil
}

type failSports struct{}

func (failSports) EventIsSports(context.Context, string) (bool, error) {
	return false, errors.New("gamma down")
}

func act(wallet, slug, side, outcome string, usd, price float64, ts int64, hash string) polymarket.Activity {
	return polymarket.Activity{
		ProxyWallet:     wallet,
		EventSlug:       slug,
		Slug:            slug,
		Title:           "Market " + slug,
		Side:            side,
		Outcome:         outcome,
		USDCSize:        usd,
		Size:            usd / price,
		Price:           price,
		Timestamp:       ts,
		TransactionHash: hash,
		Asset:           "tok",
		Name:            "n",
		ConditionID:     "0xabc",
	}
}

func TestFirstRunSeedsWithoutAlerts(t *testing.T) {
	s := &State{Seen: map[string]int64{}}
	acts := []polymarket.Activity{
		act("0x23d81ba9371e576015c1e562db09c689f56b0288", "election", "BUY", "Yes", 100, 0.4, 10, "h1"),
	}
	p, err := BuildPlan(context.Background(), fakeSports{}, s, acts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !p.FirstRun || len(p.Alerts) != 0 {
		t.Fatalf("plan=%+v", p)
	}
	if !s.Seeded || !s.known(polymarket.ActivityKey(acts[0])) {
		t.Fatal("expected seed")
	}

	p, err = BuildPlan(context.Background(), fakeSports{}, s, acts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.FirstRun || len(p.Alerts) != 0 {
		t.Fatalf("second poll should be quiet: %+v", p)
	}
}

func TestBuildPlanFiltersSportsAndDust(t *testing.T) {
	s := &State{Seeded: true, Seen: map[string]int64{}}
	api := fakeSports{"ucl": true, "election": false}
	acts := []polymarket.Activity{
		act("0xc8b9a30184244d427169cf62485dde6041b2b836", "ucl", "BUY", "Yes", 500, 0.5, 20, "sports"),
		act("0xc8b9a30184244d427169cf62485dde6041b2b836", "election", "BUY", "No", 2, 0.5, 21, "dust"),
		act("0xc8b9a30184244d427169cf62485dde6041b2b836", "election", "BUY", "Yes", 200, 0.4, 22, "keep"),
	}
	p, err := BuildPlan(context.Background(), api, s, acts, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Alerts) != 1 {
		t.Fatalf("alerts=%d", len(p.Alerts))
	}
	if p.Alerts[0].Name != "SnowLover7" || p.Alerts[0].USDC != 200 {
		t.Fatalf("alert=%+v", p.Alerts[0])
	}
	if len(p.DropKeys) != 1 {
		t.Fatalf("drop=%v (sports only; dust stays unseen until it aggregates over min)", p.DropKeys)
	}
	s.CommitDropped(p)
	if s.known(polymarket.ActivityKey(acts[1])) {
		t.Fatal("sub-min fill must remain unseen so later same-market fills can combine")
	}
}

func TestBuildPlanAggregatesThenAppliesMinUSD(t *testing.T) {
	s := &State{Seeded: true, Seen: map[string]int64{}}
	w := "0xc8b9a30184244d427169cf62485dde6041b2b836"
	acts := []polymarket.Activity{
		act(w, "election", "BUY", "No", 40, 0.32, 10, "a"),
		act(w, "election", "BUY", "No", 70, 0.32, 11, "b"),
		act(w, "election", "SELL", "No", 80, 0.4, 12, "c"),
	}
	p, err := BuildPlan(context.Background(), fakeSports{}, s, acts, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Alerts) != 1 {
		t.Fatalf("want one aggregated BUY: %+v", p.Alerts)
	}
	buy := p.Alerts[0]
	if buy.Side != "BUY" || buy.USDC != 110 || buy.Parts != 2 {
		t.Fatalf("buy=%+v", buy)
	}
	if s.known(polymarket.ActivityKey(acts[2])) {
		t.Fatal("sub-min SELL should not be dropped")
	}

	s.CommitSent(buy)
	later := []polymarket.Activity{acts[2], act(w, "election", "SELL", "No", 30, 0.4, 13, "d")}
	p2, err := BuildPlan(context.Background(), fakeSports{}, s, later, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Alerts) != 1 || p2.Alerts[0].Side != "SELL" || p2.Alerts[0].USDC != 110 {
		t.Fatalf("second poll should combine leftover SELL: %+v", p2.Alerts)
	}
}

func TestBuildPlanAggregatesSameMarket(t *testing.T) {
	s := &State{Seeded: true, Seen: map[string]int64{}}
	w := "0xc8b9a30184244d427169cf62485dde6041b2b836"
	acts := []polymarket.Activity{
		act(w, "election", "BUY", "Yes", 100, 0.4, 10, "a"),
		act(w, "election", "BUY", "Yes", 300, 0.6, 11, "b"),
		act(w, "election", "SELL", "Yes", 50, 0.5, 12, "c"),
	}
	p, err := BuildPlan(context.Background(), fakeSports{}, s, acts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Alerts) != 2 {
		t.Fatalf("alerts=%d", len(p.Alerts))
	}
	var buy Alert
	for _, a := range p.Alerts {
		if a.Side == "BUY" {
			buy = a
		}
	}
	if buy.Parts != 2 || buy.USDC != 400 {
		t.Fatalf("buy=%+v", buy)
	}
	// size-weighted: (100/0.4)*0.4 + (300/0.6)*0.6 all over sizes
	// size = usd/price → 250 + 500 = 750; price = (250*0.4 + 500*0.6)/750 = 0.533...
	if buy.Price < 0.53 || buy.Price > 0.54 {
		t.Fatalf("price=%v", buy.Price)
	}
}

func TestBuildPlanEmptyEventSlugIsNotSports(t *testing.T) {
	s := &State{Seeded: true, Seen: map[string]int64{}}
	a := act("0xc8b9a30184244d427169cf62485dde6041b2b836", "", "BUY", "Yes", 50, 0.5, 1, "h")
	p, err := BuildPlan(context.Background(), fakeSports{}, s, []polymarket.Activity{a}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Alerts) != 1 {
		t.Fatalf("alerts=%d", len(p.Alerts))
	}
}

func TestBuildPlanSkipsWhenSportsLookupFails(t *testing.T) {
	s := &State{Seeded: true, Seen: map[string]int64{}}
	acts := []polymarket.Activity{
		act("0x1", "election", "BUY", "Yes", 100, 0.5, 1, "h"),
	}
	p, err := BuildPlan(context.Background(), failSports{}, s, acts, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Alerts) != 0 || len(p.DropKeys) != 0 {
		t.Fatalf("should retry later: %+v", p)
	}
	if s.known(polymarket.ActivityKey(acts[0])) {
		t.Fatal("must not mark unseen on lookup failure")
	}
}

func TestCommitSentThenQuiet(t *testing.T) {
	s := &State{Seeded: true, Seen: map[string]int64{}}
	a := act("0xc8b9a30184244d427169cf62485dde6041b2b836", "election", "BUY", "Yes", 100, 0.5, 1, "h")
	p, _ := BuildPlan(context.Background(), fakeSports{}, s, []polymarket.Activity{a}, 0)
	s.CommitDropped(p)
	s.CommitSent(p.Alerts[0])
	p2, _ := BuildPlan(context.Background(), fakeSports{}, s, []polymarket.Activity{a}, 0)
	if len(p2.Alerts) != 0 {
		t.Fatal("already sent")
	}
}

func TestFormat(t *testing.T) {
	got := Format(Alert{
		Name:    "SnowLover7",
		Side:    "BUY",
		Outcome: "No",
		Size:    32000,
		Price:   0.32,
		Title:   "Fed decision in September?",
	})
	want := "SnowLover7 BUY NO 32k @ 32c\nFed decision in September?"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatIncludesNetPosition(t *testing.T) {
	got := Format(Alert{
		Name:            "SnowLover7",
		Side:            "BUY",
		Outcome:         "No",
		Size:            32000,
		Price:           0.32,
		Title:           "Fed decision in September?",
		HasPosition:     true,
		PositionSize:    27500,
		PositionOutcome: "YES",
	})
	want := "SnowLover7 BUY NO 32k @ 32c\nFed decision in September?\nPosition: 27.5k YES"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	flat := Format(Alert{
		Name:        "A",
		Side:        "SELL",
		Outcome:     "Yes",
		Size:        10,
		Price:       0.5,
		Title:       "M",
		HasPosition: true,
	})
	if !strings.HasSuffix(flat, "\nPosition: 0") {
		t.Fatalf("flat=%q", flat)
	}
}

type fakePositions map[string][]polymarket.Position

func (f fakePositions) FetchPositions(_ context.Context, opt polymarket.FetchPositionsOptions) ([]polymarket.Position, error) {
	key := strings.ToLower(opt.User) + "|" + opt.Market
	if pos, ok := f[key]; ok {
		return pos, nil
	}
	return nil, errors.New("missing")
}

func TestAttachNetPositions(t *testing.T) {
	alerts := []Alert{
		{Wallet: "0xAAA", ConditionID: "0xabc", Name: "A"},
		{Wallet: "0xaaa", ConditionID: "0xabc", Name: "A2"},
		{Wallet: "0xbbb", ConditionID: "0xdef", Name: "B"},
		{Wallet: "", ConditionID: "0xabc", Name: "skip"},
	}
	api := fakePositions{
		"0xaaa|0xabc": {
			{Outcome: "Yes", Size: 100000},
			{Outcome: "No", Size: 72500},
		},
	}
	AttachNetPositions(context.Background(), api, alerts)
	if !alerts[0].HasPosition || alerts[0].PositionSize != 27500 || alerts[0].PositionOutcome != "YES" {
		t.Fatalf("first=%+v", alerts[0])
	}
	if alerts[1].PositionSize != 27500 {
		t.Fatalf("cache miss on second: %+v", alerts[1])
	}
	if alerts[2].HasPosition {
		t.Fatal("failed lookup should omit Position")
	}
	if alerts[3].HasPosition {
		t.Fatal("empty wallet should skip")
	}
}

func TestFormatShares(t *testing.T) {
	cases := []struct {
		n    float64
		want string
	}{
		{32_000, "32k"},
		{1_400, "1.4k"},
		{1_450, "1.5k"},
		{1_000, "1k"},
		{999, "999"},
		{9.4, "9.4"},
		{50, "50"},
	}
	for _, tc := range cases {
		if got := formatShares(tc.n); got != tc.want {
			t.Errorf("formatShares(%v)=%q want %q", tc.n, got, tc.want)
		}
	}
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	min := 100.0
	s := &State{Seeded: true, ChatID: 42, MinUSD: &min, Seen: map[string]int64{"k": 1}}
	if err := SaveState(path, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Seeded || got.ChatID != 42 || got.Seen["k"] != 1 || got.EffectiveMinUSD(0) != 100 {
		t.Fatalf("%+v", got)
	}
	missing, err := LoadState(filepath.Join(dir, "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if missing.Seen == nil {
		t.Fatal("empty seen map")
	}
}
