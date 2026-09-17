package alert

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

func TestBuildLastTradesReportNewestFirst(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeActPx(w.Address, "mA", "alpha", "BUY", "Yes", 10, 0.40, 100, "1"),
		tradeActPx(w.Address, "mB", "beta", "SELL", "No", 20, 0.55, 200, "2"),
	}
	r := BuildLastTradesReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{w}, time.Hour, 0, false, "", "", "", "", "")
	if len(r.Trades) != 2 {
		t.Fatalf("%+v", r.Trades)
	}
	if r.Trades[0].Slug != "beta" || r.Trades[1].Slug != "alpha" {
		t.Fatalf("want newest first: %+v", r.Trades)
	}
	if r.Trades[0].Side != "SELL" || r.Trades[0].Outcome != "NO" || r.Trades[0].Price != 0.55 {
		t.Fatalf("%+v", r.Trades[0])
	}
}

func TestBuildLastTradesReportDropsSportsUnlessMarketSet(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeAct(w.Address, "mA", "ucl", "BUY", "Yes", 50, 1, "1"),
		tradeAct(w.Address, "mB", "election", "BUY", "Yes", 8, 2, "2"),
	}
	r := BuildLastTradesReport(context.Background(), fakeSports{"ucl": true}, acts, []sharps.Wallet{w}, time.Hour, 0, false, "", "", "", "", "")
	if len(r.Trades) != 1 || r.Trades[0].Slug != "election" || !r.DroppedSport {
		t.Fatalf("%+v", r)
	}

	r = BuildLastTradesReport(context.Background(), fakeSports{"ucl": true}, acts, []sharps.Wallet{w}, time.Hour, 0, false, "mA", "ucl", "ucl", "UCL", "")
	if len(r.Trades) != 1 || r.Trades[0].Slug != "ucl" {
		t.Fatalf("explicit market should keep sports: %+v", r.Trades)
	}
}

func TestBuildLastTradesReportFiltersWalletAndMarket(t *testing.T) {
	alice := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	bob := sharps.Wallet{Address: "0xbbb", Name: "Bob"}
	acts := []polymarket.Activity{
		tradeAct(alice.Address, "mA", "alpha", "BUY", "Yes", 10, 10, "1"),
		tradeAct(bob.Address, "mA", "alpha", "SELL", "No", 5, 11, "2"),
		tradeAct(alice.Address, "mB", "beta", "BUY", "Yes", 7, 12, "3"),
	}
	r := BuildLastTradesReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{alice}, time.Hour, 0, false, "mA", "alpha", "alpha", "Alpha", "Alice")
	if len(r.Trades) != 1 || r.Trades[0].Name != "Alice" || r.Trades[0].Slug != "alpha" {
		t.Fatalf("%+v", r.Trades)
	}

	r = BuildLastTradesReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{alice, bob}, time.Hour, 0, false, "", "", "beta", "", "")
	if len(r.Trades) != 1 || r.Trades[0].Slug != "beta" {
		t.Fatalf("substring market: %+v", r.Trades)
	}
}

func TestBuildLastTradesReportRespectsSince(t *testing.T) {
	w := sharps.Wallet{Address: "0xaaa", Name: "Alice"}
	acts := []polymarket.Activity{
		tradeAct(w.Address, "mA", "alpha", "BUY", "Yes", 10, 50, "1"),
		tradeAct(w.Address, "mA", "alpha", "BUY", "Yes", 10, 150, "2"),
	}
	r := BuildLastTradesReport(context.Background(), fakeSports{}, acts, []sharps.Wallet{w}, time.Hour, 100, false, "", "", "", "", "")
	if len(r.Trades) != 1 || r.Trades[0].Timestamp != 150 {
		t.Fatalf("%+v", r.Trades)
	}
}

func TestFormatLastTradesReportEmpty(t *testing.T) {
	got := FormatLastTradesReport(LastTradesReport{Window: 24 * time.Hour})
	if len(got) != 1 || !strings.Contains(got[0], "No fills") || !strings.Contains(got[0], "last 1d") {
		t.Fatalf("%q", got)
	}
}

func TestFormatLastTradesReportLines(t *testing.T) {
	now := time.Unix(200, 0)
	r := LastTradesReport{
		Window: time.Hour,
		Now:    now,
		Trades: []LastTrade{
			{Name: "Alice", Side: "BUY", Outcome: "YES", Title: "Market alpha", Size: 10, Price: 0.4, Timestamp: 200 - 600},
			{Name: "Bob", Side: "SELL", Outcome: "NO", Title: "Market beta", Size: 20, Price: 0.55, Timestamp: 200 - 3600},
		},
	}
	got := strings.Join(FormatLastTradesReport(r), "\n")
	if !strings.Contains(got, "Alice BUY YES 10 @ 40c") {
		t.Fatalf("alice line: %s", got)
	}
	if !strings.Contains(got, "Bob SELL NO 20 @ 55c") {
		t.Fatalf("bob line: %s", got)
	}
	if !strings.Contains(got, "Market alpha") || !strings.Contains(got, "Market beta") {
		t.Fatalf("titles: %s", got)
	}
}

func TestSplitTraderMarket(t *testing.T) {
	trader, market := splitTraderMarket(nil)
	if trader != "" || market != "" {
		t.Fatalf("%q %q", trader, market)
	}
	trader, market = splitTraderMarket([]string{"all", "Andersson"})
	if trader != "" || market != "Andersson" {
		t.Fatalf("%q %q", trader, market)
	}
	trader, market = splitTraderMarket([]string{"Flip", "Magdalena", "Andersson"})
	if trader != "Flip" || market != "Magdalena Andersson" {
		t.Fatalf("%q %q", trader, market)
	}
	trader, market = splitTraderMarket([]string{"Magdalena", "Andersson"})
	if trader != "" || market != "Magdalena Andersson" {
		t.Fatalf("%q %q", trader, market)
	}
}
