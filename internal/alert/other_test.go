package alert

import (
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
)

func senateMarkets() []polymarket.EventMarket {
	return []polymarket.EventMarket{
		{SearchMarket: polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xa", Slug: "a", Question: "A", URL: "https://polymarket.com/event/race/a"}, GroupItemTitle: "Candidate A", Volume24hr: 5}, YesPrice: 0.62},
		{SearchMarket: polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xb", Slug: "b", Question: "B", URL: "https://polymarket.com/event/race/b"}, GroupItemTitle: "Candidate B", Volume24hr: 9}, YesPrice: 0.30},
		{SearchMarket: polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xc", Slug: "c", Question: "C"}, GroupItemTitle: "Candidate C"}, YesPrice: 0.04},
	}
}

func TestPickOtherOutcome(t *testing.T) {
	markets := senateMarkets()
	got, ok := PickOtherOutcome(markets[0].SearchMarket, markets)
	if !ok || got.Market.ConditionID != "0xb" {
		t.Fatalf("from A: %+v %v", got.Market.ConditionID, ok)
	}
	got, ok = PickOtherOutcome(markets[2].SearchMarket, markets)
	if !ok || got.Market.ConditionID != "0xa" {
		t.Fatalf("from C: %+v %v", got.Market.ConditionID, ok)
	}
	got, ok = PickOtherOutcome(markets[1].SearchMarket, markets)
	if !ok || got.Market.ConditionID != "0xa" {
		t.Fatalf("from B: %+v %v", got.Market.ConditionID, ok)
	}
	tied := []polymarket.EventMarket{
		{SearchMarket: polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xa", Question: "A"}, Volume24hr: 1}, YesPrice: 0.4},
		{SearchMarket: polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xb", Question: "B"}, Volume24hr: 8}, YesPrice: 0.4},
		{SearchMarket: polymarket.SearchMarket{Market: polymarket.Market{ConditionID: "0xc", Question: "C"}, Volume24hr: 3}, YesPrice: 0.4},
	}
	got, ok = PickOtherOutcome(tied[0].SearchMarket, tied)
	if !ok || got.Market.ConditionID != "0xb" {
		t.Fatalf("tie: %s", got.Market.ConditionID)
	}
	if _, ok = PickOtherOutcome(markets[0].SearchMarket, markets[:1]); ok {
		t.Fatal("only one outcome")
	}
}

func TestOtherQuery(t *testing.T) {
	m := senateMarkets()[1]
	if got := OtherQuery(CmdPos, m); got != m.Market.URL {
		t.Fatalf("pos %q", got)
	}
	if got := OtherQuery(CmdOBP, m); got != "Candidate B" {
		t.Fatalf("obp %q", got)
	}
	if got := OtherTrackRef(m); got != m.Market.URL {
		t.Fatalf("track %q", got)
	}
}

func TestOtherSource(t *testing.T) {
	pos := FormatPosReport(PosReport{
		Title: "Candidate A",
		Holdings: []PosHolding{
			{Name: "Cara", Size: 10, Outcome: "YES", CurPrice: 0.62, HasCur: true},
		},
	})[0]
	sent := []SentMarket{{MessageID: 7, Cmd: "pos", Market: "https://polymarket.com/event/race/a"}}

	cmd, market, errMsg := OtherSource(7, pos, sent, "ob", "somewhere else")
	if errMsg != "" || cmd != CmdPos || market != sent[0].Market {
		t.Fatalf("reply id cmd=%d market=%q err=%q", cmd, market, errMsg)
	}

	cmd, market, errMsg = OtherSource(0, pos, nil, "holders", "")
	if errMsg != "" || cmd != CmdHolders || market != "Candidate A" {
		t.Fatalf("reply text cmd=%d market=%q err=%q", cmd, market, errMsg)
	}

	cmd, market, errMsg = OtherSource(0, "", nil, "ob", "Candidate A")
	if errMsg != "" || cmd != CmdOB || market != "Candidate A" {
		t.Fatalf("subsequent cmd=%d market=%q err=%q", cmd, market, errMsg)
	}

	_, _, errMsg = OtherSource(0, "", nil, "", "")
	if errMsg == "" {
		t.Fatal("expected usage")
	}
	_, _, errMsg = OtherSource(3, "No net position changes.", nil, "ob", "Candidate A")
	if errMsg == "" || !strings.Contains(errMsg, "doesn't name") {
		t.Fatalf("err %q", errMsg)
	}
}

func TestParseOther(t *testing.T) {
	if ParseCommand("/other").Cmd != CmdOther {
		t.Fatal("other")
	}
	if ParseCommand("/other@detectx_bot").Cmd != CmdOther {
		t.Fatal("mention")
	}
}
