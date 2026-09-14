package alert

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

func tradeAct(wallet, market, slug, side, outcome string, size float64, ts int64, hash string) polymarket.Activity {
	return tradeActPx(wallet, market, slug, side, outcome, size, 0, ts, hash)
}

func tradeActPx(wallet, market, slug, side, outcome string, size, price float64, ts int64, hash string) polymarket.Activity {
	return polymarket.Activity{
		ProxyWallet:     wallet,
		ConditionID:     market,
		EventSlug:       slug,
		Slug:            slug,
		Title:           "Market " + slug,
		Side:            side,
		Outcome:         outcome,
		Size:            size,
		Price:           price,
		USDCSize:        size * price,
		Timestamp:       ts,
		TransactionHash: hash,
		Asset:           "tok",
		Name:            "n",
	}
}

func TestBuildNetReportNetsBuysAndSells(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeAct(w.Address, "mA", "alpha", "BUY", "Yes", 30, 100, "1"),
		tradeAct(w.Address, "mA", "alpha", "SELL", "Yes", 20, 101, "2"),
		tradeAct(w.Address, "mB", "beta", "BUY", "Yes", 10, 102, "3"),
		tradeAct(w.Address, "mB", "beta", "SELL", "Yes", 10, 103, "4"),
		tradeAct(w.Address, "mC", "gamma", "BUY", "No", 30, 104, "5"),
		tradeAct(w.Address, "mC", "gamma", "SELL", "No", 20, 105, "6"),
	}
	r := BuildNetReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{w}, time.Hour, 0, false)
	if len(r.Traders) != 1 {
		t.Fatalf("traders=%d", len(r.Traders))
	}
	got := map[string]MarketDelta{}
	for _, m := range r.Traders[0].Markets {
		got[m.Title] = m
	}
	if len(got) != 2 {
		t.Fatalf("want 2 markets (flat beta omitted): %+v", r.Traders[0].Markets)
	}
	a := got["Market alpha"]
	if a.Outcome != "YES" || a.Size != 10 {
		t.Fatalf("alpha=%+v want +10 YES", a)
	}
	c := got["Market gamma"]
	if c.Outcome != "NO" || c.Size != 10 {
		t.Fatalf("gamma=%+v want +10 NO", c)
	}
}

func TestBuildNetReportHedgeIsFlat(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeAct(w.Address, "mA", "alpha", "BUY", "Yes", 30, 1, "1"),
		tradeAct(w.Address, "mA", "alpha", "BUY", "No", 30, 2, "2"),
	}
	r := BuildNetReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{w}, time.Hour, 0, false)
	if len(r.Traders) != 0 {
		t.Fatalf("hedged market should be omitted: %+v", r.Traders)
	}
}

func TestBuildNetReportDropsSports(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeAct(w.Address, "mA", "ucl", "BUY", "Yes", 50, 1, "1"),
		tradeAct(w.Address, "mB", "election", "BUY", "Yes", 8, 2, "2"),
	}
	r := BuildNetReport(context.Background(), fakeSports{"ucl": true}, acts, []sharps.Wallet{w}, time.Hour, 0, false)
	if len(r.Traders) != 1 || len(r.Traders[0].Markets) != 1 || r.Traders[0].Markets[0].Title != "Market election" {
		t.Fatalf("%+v", r.Traders)
	}
}

func TestBuildNetReportHidesWhenSportsLookupFails(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeAct(w.Address, "mA", "mystery-event", "BUY", "Yes", 50, 1, "1"),
	}
	r := BuildNetReport(context.Background(), failSports{}, acts, []sharps.Wallet{w}, time.Hour, 0, false)
	if len(r.Traders) != 0 {
		t.Fatalf("unknown events must not leak when Gamma is down: %+v", r.Traders)
	}
}

func TestBuildNetReportEffectiveAvgYesOnly(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeActPx(w.Address, "mA", "alpha", "BUY", "Yes", 10, 0.40, 1, "1"),
		tradeActPx(w.Address, "mA", "alpha", "BUY", "Yes", 10, 0.60, 2, "2"),
	}
	r := BuildNetReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{w}, time.Hour, 0, false)
	if len(r.Traders) != 1 || len(r.Traders[0].Markets) != 1 {
		t.Fatalf("%+v", r.Traders)
	}
	m := r.Traders[0].Markets[0]
	if m.Outcome != "YES" || m.Size != 20 || !m.HasAvg || m.AvgPrice != 0.50 {
		t.Fatalf("got %+v want +20 YES @ 0.50", m)
	}
}

func TestBuildNetReportEffectiveAvgHedge(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	// 100 YES @ 0.55 and 40 NO @ 0.40 → leftover 60 YES at (55+16-40)/60 = 0.5166…
	acts := []polymarket.Activity{
		tradeActPx(w.Address, "mA", "alpha", "BUY", "Yes", 100, 0.55, 1, "1"),
		tradeActPx(w.Address, "mA", "alpha", "BUY", "No", 40, 0.40, 2, "2"),
	}
	r := BuildNetReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{w}, time.Hour, 0, false)
	if len(r.Traders) != 1 || len(r.Traders[0].Markets) != 1 {
		t.Fatalf("%+v", r.Traders)
	}
	m := r.Traders[0].Markets[0]
	want := (55.0 + 16.0 - 40.0) / 60.0
	if m.Outcome != "YES" || m.Size != 60 || !m.HasAvg || abs(m.AvgPrice-want) > 1e-9 {
		t.Fatalf("got %+v want +60 YES @ %v", m, want)
	}
}

func TestBuildNetReportEffectiveAvgNetNo(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeActPx(w.Address, "mA", "alpha", "BUY", "No", 80, 0.30, 1, "1"),
		tradeActPx(w.Address, "mA", "alpha", "BUY", "Yes", 20, 0.80, 2, "2"),
	}
	r := BuildNetReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{w}, time.Hour, 0, false)
	if len(r.Traders) != 1 || len(r.Traders[0].Markets) != 1 {
		t.Fatalf("%+v", r.Traders)
	}
	m := r.Traders[0].Markets[0]
	// leftover 60 NO at (24+16-20)/60 = 0.333…
	want := (24.0 + 16.0 - 20.0) / 60.0
	if m.Outcome != "NO" || m.Size != 60 || !m.HasAvg || abs(m.AvgPrice-want) > 1e-9 {
		t.Fatalf("got %+v want +60 NO @ %v", m, want)
	}
}

func TestBuildNetReportOmitsAvgWithoutPrices(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeAct(w.Address, "mA", "alpha", "BUY", "Yes", 10, 1, "1"),
	}
	r := BuildNetReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{w}, time.Hour, 0, false)
	m := r.Traders[0].Markets[0]
	if m.HasAvg {
		t.Fatalf("no fill prices → no avg: %+v", m)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestFormatNetReport(t *testing.T) {
	chunks := FormatNetReport(NetReport{
		Window: 6 * time.Hour,
		Traders: []TraderDelta{{
			Name: "SnowLover7",
			Markets: []MarketDelta{
				{Title: "Fed decision?", Size: 10, Outcome: "YES"},
			},
		}},
	}, "SnowLover7")
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	got := chunks[0]
	if !strings.Contains(got, "Net change · last 6h · SnowLover7") {
		t.Fatalf("head: %s", got)
	}
	if !strings.Contains(got, "+10 YES  Fed decision?") {
		t.Fatalf("line: %s", got)
	}

	priced := FormatNetReport(NetReport{
		Window: 6 * time.Hour,
		Traders: []TraderDelta{{
			Name: "SnowLover7",
			Markets: []MarketDelta{
				{Title: "Fed decision?", Size: 10, Outcome: "YES", AvgPrice: 0.42, HasAvg: true},
			},
		}},
	}, "")
	if !strings.Contains(priced[0], "+10 YES @ 42c  Fed decision?") {
		t.Fatalf("priced line: %s", priced[0])
	}

	empty := FormatNetReport(NetReport{Window: 24 * time.Hour}, "")
	if len(empty) != 1 || !strings.Contains(empty[0], "No net position changes") {
		t.Fatalf("%v", empty)
	}
}

func TestResolveNetWallets(t *testing.T) {
	all, err := ResolveNetWallets("")
	if err != "" || len(all) != len(sharps.Tracked) {
		t.Fatalf("all %d err=%s", len(all), err)
	}
	one, err := ResolveNetWallets("Flip")
	if err != "" || len(one) != 1 || one[0].Name != "Flipadelphia" {
		t.Fatalf("%+v %s", one, err)
	}
	if _, err := ResolveNetWallets("w"); err == "" || !strings.Contains(err, "Several") {
		t.Fatalf("ambiguous w: %q", err)
	}
	if _, err := ResolveNetWallets("no-such-trader"); err == "" {
		t.Fatal("expected miss")
	}
}
