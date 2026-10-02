package alert

import (
	"strings"
	"testing"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

func TestMarketFromReplyFill(t *testing.T) {
	text := Format(Alert{
		Name: "SnowLover7", Side: "BUY", Outcome: "No", Size: 32000, Price: 0.32,
		Title: "Fed decision in September?",
		HasPosition: true, PositionSize: 27500, PositionOutcome: "YES",
	})
	got, n := MarketFromReply(text)
	if n != 1 || got != "Fed decision in September?" {
		t.Fatalf("n=%d %q\n%s", n, got, text)
	}
}

func TestMarketFromReplyOrderBook(t *testing.T) {
	text := FormatOBReport(OBReport{
		Title: "Aliens?",
		Books: []polymarket.OutcomeBook{{
			Outcome: "Yes", Tick: 0.01,
			Bids: []polymarket.BookLevel{{Price: 0.42, Size: 50}},
			Asks: []polymarket.BookLevel{{Price: 0.43, Size: 10}},
		}},
	})
	got, n := MarketFromReply(text)
	if n != 1 || got != "Aliens?" {
		t.Fatalf("n=%d %q\n%s", n, got, text)
	}
}

func TestMarketFromReplyPos(t *testing.T) {
	chunks := FormatPosReport(PosReport{
		Title: "Will it happen?",
		Holdings: []PosHolding{
			{Name: "Cara", Size: 100, Outcome: "YES", AvgPrice: 0.61, HasAvg: true, CurPrice: 0.64, HasCur: true},
		},
	})
	got, n := MarketFromReply(chunks[0])
	if n != 1 || got != "Will it happen?" {
		t.Fatalf("n=%d %q\n%s", n, got, chunks[0])
	}
}

func TestMarketFromReplyNet(t *testing.T) {
	one := FormatNetReport(NetReport{
		Traders: []TraderDelta{{
			Name:    "SnowLover7",
			Markets: []MarketDelta{{Title: "Fed decision?", Size: 10, Outcome: "YES", AvgPrice: 0.42, HasAvg: true}},
		}},
	}, "")
	got, n := MarketFromReply(one[0])
	if n != 1 || got != "Fed decision?" {
		t.Fatalf("one n=%d %q\n%s", n, got, one[0])
	}

	many := FormatNetReport(NetReport{
		Traders: []TraderDelta{{
			Name: "SnowLover7",
			Markets: []MarketDelta{
				{Title: "Fed decision?", Size: 10, Outcome: "YES"},
				{Title: "Other market", Size: -5, Outcome: "NO"},
			},
		}},
	}, "")
	_, n = MarketFromReply(many[0])
	if n < 2 {
		t.Fatalf("many n=%d\n%s", n, many[0])
	}
	same := FormatNetReport(NetReport{
		Traders: []TraderDelta{
			{Name: "A", Markets: []MarketDelta{{Title: "Fed decision?", Size: 10, Outcome: "YES"}}},
			{Name: "B", Markets: []MarketDelta{{Title: "Fed decision?", Size: 4, Outcome: "NO"}}},
		},
	}, "")
	got, n = MarketFromReply(strings.Join(same, "\n\n"))
	if n != 1 || got != "Fed decision?" {
		t.Fatalf("same n=%d %q\n%s", n, got, strings.Join(same, "\n\n"))
	}
}

func TestMarketFromReplyLastTrades(t *testing.T) {
	now := time.Unix(200, 0)
	open := FormatLastTradesReport(LastTradesReport{
		Now: now,
		Trades: []LastTrade{
			{Name: "Alice", Side: "BUY", Outcome: "YES", Title: "Market alpha", Size: 10, Price: 0.4, Timestamp: 140},
			{Name: "Bob", Side: "SELL", Outcome: "NO", Title: "Market beta", Size: 20, Price: 0.55, Timestamp: 100},
		},
	})
	_, n := MarketFromReply(strings.Join(open, "\n"))
	if n < 2 {
		t.Fatalf("open n=%d\n%s", n, strings.Join(open, "\n"))
	}

	one := FormatLastTradesReport(LastTradesReport{
		Now:         now,
		MarketTitle: "Fed decision?",
		TraderQuery: "Flip",
		Trades: []LastTrade{
			{Name: "Flip", Side: "BUY", Outcome: "YES", Title: "Fed decision?", Size: 10, Price: 0.4, Timestamp: 140},
		},
	})
	got, n := MarketFromReply(one[0])
	if n != 1 || got != "Fed decision?" {
		t.Fatalf("filtered n=%d %q\n%s", n, got, one[0])
	}
}

func TestMarketFromReplyURLAndPlainLine(t *testing.T) {
	text := "Will it happen?\nhttps://polymarket.com/event/some-event/will-it-happen"
	got, n := MarketFromReply(text)
	if n != 1 || got != "https://polymarket.com/event/some-event/will-it-happen" {
		t.Fatalf("n=%d %q", n, got)
	}
	got, n = MarketFromReply("Fed decision in September?")
	if n != 1 || got != "Fed decision in September?" {
		t.Fatalf("plain n=%d %q", n, got)
	}
	if _, n = MarketFromReply("Still here. /help for commands."); n != 0 {
		t.Fatal("status")
	}
	if _, n = MarketFromReply(""); n != 0 {
		t.Fatal("empty")
	}
}

func TestMarketFromReplyPort(t *testing.T) {
	one := FormatPortReport(PortReport{
		Name: "Alice",
		Holdings: []PortHolding{
			{Title: "Market A", Size: 8600, Outcome: "YES", CurPrice: 0.64, AvgPrice: 0.61, HasCur: true, HasAvg: true},
		},
	})
	got, n := MarketFromReply(one[0])
	if n != 1 || got != "Market A" {
		t.Fatalf("n=%d %q\n%s", n, got, one[0])
	}
	many := FormatPortReport(PortReport{
		Name: "Alice",
		Holdings: []PortHolding{
			{Title: "Market A", Size: 8600, Outcome: "YES", CurPrice: 0.64, AvgPrice: 0.61, HasCur: true, HasAvg: true},
			{Title: "Market B", Size: 3400, Outcome: "NO", CurPrice: 0.20, AvgPrice: 0.21, HasCur: true, HasAvg: true},
		},
	})
	if _, n = MarketFromReply(many[0]); n < 2 {
		t.Fatalf("many n=%d\n%s", n, many[0])
	}
}

func TestMarketFromReplyPriceAlert(t *testing.T) {
	text := PriceAlertSetText(PriceAlert{Title: "Aliens?", Price: 0.32, MinSize: 1000})
	got, n := MarketFromReply(text)
	if n != 1 || got != "Aliens?" {
		t.Fatalf("set n=%d %q\n%s", n, got, text)
	}
	list := FormatPriceAlertList([]PriceAlert{
		{Title: "Aliens?", Price: 0.32, MinSize: 1000},
		{Title: "Mars?", Price: 0.10, MinSize: 50},
	})
	if _, n = MarketFromReply(list); n < 2 {
		t.Fatalf("list n=%d\n%s", n, list)
	}
}
