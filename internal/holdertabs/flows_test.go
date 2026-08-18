package holdertabs

import (
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
)

func TestYesPressure(t *testing.T) {
	cases := []struct {
		side, outcome string
		size, price   float64
		want          float64
	}{
		{"BUY", "Yes", 100, 0.4, 40},
		{"SELL", "Yes", 100, 0.4, -40},
		{"BUY", "No", 50, 0.2, -10},
		{"SELL", "No", 50, 0.2, 10},
		{"buy", "YES", 10, 1, 10},
	}
	for _, tc := range cases {
		got := YesPressure(trade(tc.side, tc.outcome, "0x1", tc.size, tc.price))
		if got != tc.want {
			t.Fatalf("%s %s: got %v want %v", tc.side, tc.outcome, got, tc.want)
		}
	}
}

func TestAggregateTakerFlows(t *testing.T) {
	raw := []polymarket.Trade{
		trade("BUY", "Yes", "0xaaa", 100, 0.5),   // +$50 yes
		trade("BUY", "No", "0xbbb", 80, 0.5),     // -$40 no
		trade("SELL", "No", "0xaaa", 20, 0.5),    // +$10 yes-pressure
		trade("BUY", "Yes", "0xsharp", 200, 0.5), // skipped
		trade("BUY", "Yes", "0xccc", 1, 0.5),     // $0.50 dust, skipped by min
	}
	skip := map[string]struct{}{"0xsharp": {}}
	s := AggregateTakerFlows(raw, FlowOptions{SkipWallets: skip, MinUSD: 1})

	if s.Orders != 3 {
		t.Fatalf("orders=%d", s.Orders)
	}
	if s.TakerUSD != 50+40+10 {
		t.Fatalf("taker_usd=%v", s.TakerUSD)
	}
	if s.YesUSD != 50 || s.NoUSD != 50 {
		t.Fatalf("yes=%v no=%v", s.YesUSD, s.NoUSD)
	}
	if s.NetUSD != 50-40+10 {
		t.Fatalf("net=%v", s.NetUSD)
	}
	if s.Stronger != "yes" {
		t.Fatalf("stronger=%q", s.Stronger)
	}
	if s.Unique != 2 || s.YesTakers != 1 || s.NoTakers != 1 {
		t.Fatalf("unique=%d yes_takers=%d no_takers=%d", s.Unique, s.YesTakers, s.NoTakers)
	}
	if len(s.YesTakerList) != 1 || s.YesTakerList[0].Wallet != "0xaaa" {
		t.Fatalf("yes list: %+v", s.YesTakerList)
	}
}

func TestAggregateTakerFlowsMaxUSD(t *testing.T) {
	raw := []polymarket.Trade{
		trade("BUY", "Yes", "0x1", 10, 1),  // $10 keep
		trade("BUY", "Yes", "0x2", 500, 1), // $500 drop
	}
	s := AggregateTakerFlows(raw, FlowOptions{MaxUSD: 50})
	if s.Orders != 1 || s.TakerUSD != 10 {
		t.Fatalf("got orders=%d usd=%v", s.Orders, s.TakerUSD)
	}
}

func TestAggregateTakerFlowsEmpty(t *testing.T) {
	s := AggregateTakerFlows(nil, FlowOptions{})
	if s.Stronger != "tie" || s.Orders != 0 || s.TakerUSD != 0 {
		t.Fatalf("unexpected %+v", s)
	}
}

func trade(side, outcome, wallet string, size, price float64) polymarket.Trade {
	return polymarket.Trade{
		Side:        side,
		Outcome:     outcome,
		ProxyWallet: wallet,
		Size:        size,
		Price:       price,
	}
}
