package alert

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

func bookAt(bid, ask float64) polymarket.OutcomeBook {
	return polymarket.OutcomeBook{
		Outcome: "No",
		Tick:    0.01,
		Bids:    []polymarket.BookLevel{{Price: bid, Size: 100}},
		Asks:    []polymarket.BookLevel{{Price: ask, Size: 80}},
	}
}

func armedWatch(bid, ask float64) PriceWatch {
	w, errMsg := NewPriceWatch(PriceAlertDraft{
		Title:       "Merz December",
		Slug:        "merz-december",
		ConditionID: "0xabc",
	}, "No", 0.03, bookAt(bid, ask), time.Unix(1000, 0))
	if errMsg != "" {
		panic(errMsg)
	}
	return w
}

func TestNewPriceWatchAnchorsMidpoint(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	if math.Abs(w.Anchor-0.815) > 1e-9 {
		t.Fatalf("anchor %v", w.Anchor)
	}
	if w.Outcome != "No" || w.Delta != 0.03 {
		t.Fatalf("%+v", w)
	}
	text := PriceWatchSetText(w)
	if !strings.Contains(text, "81.5¢") || !strings.Contains(text, "78.5¢") || !strings.Contains(text, "84.5¢") || !strings.Contains(text, "NO") {
		t.Fatalf("%s", text)
	}
}

func TestPriceWatchMidpointStepsThenReanchors(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	hit, next := EvaluatePriceWatch(w, bookAt(0.82, 0.83), nil)
	if hit != nil {
		t.Fatalf("1¢ move should be quiet: %+v", hit)
	}

	hit, next = EvaluatePriceWatch(next, bookAt(0.84, 0.85), nil)
	if hit == nil {
		t.Fatal("expected 84.5 ping")
	}
	if math.Abs(hit.From-0.815) > 1e-9 || math.Abs(hit.To-0.845) > 1e-9 {
		t.Fatalf("from %v to %v", hit.From, hit.To)
	}
	text := PriceWatchPingText(*hit)
	if !strings.Contains(text, "NO 84.5¢") || !strings.Contains(text, "81.5¢") || !strings.Contains(text, "via mid") || !strings.Contains(text, "next 81.5¢ or 87.5¢") {
		t.Fatalf("%s", text)
	}

	hit, next = EvaluatePriceWatch(next, bookAt(0.84, 0.85), nil)
	if hit != nil {
		t.Fatal("same price should not ping again")
	}
	hit, _ = EvaluatePriceWatch(next, bookAt(0.87, 0.88), nil)
	if hit == nil || math.Abs(hit.To-0.875) > 1e-9 || math.Abs(hit.From-0.845) > 1e-9 {
		t.Fatalf("next step %+v", hit)
	}
}

func TestPriceWatchDownMove(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	hit, _ := EvaluatePriceWatch(w, bookAt(0.78, 0.79), nil)
	if hit == nil || math.Abs(hit.To-0.785) > 1e-9 {
		t.Fatalf("%+v", hit)
	}
	text := PriceWatchPingText(*hit)
	if !strings.Contains(text, "NO 78.5¢") || !strings.Contains(text, "-3¢") {
		t.Fatalf("%s", text)
	}
}

func TestPriceWatchBidOrFillWithoutMidMove(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	// Ask walks to the +3¢ band. Mid only moves 1.75¢, so the ask is the anchor.
	hit, next := EvaluatePriceWatch(w, bookAt(0.81, 0.845), nil)
	if hit == nil || math.Abs(hit.To-0.845) > 1e-9 {
		t.Fatalf("ask %+v", hit)
	}
	if !strings.Contains(PriceWatchPingText(*hit), "via ask") {
		t.Fatal(PriceWatchPingText(*hit))
	}

	w = armedWatch(0.81, 0.82)
	hit, next = EvaluatePriceWatch(w, bookAt(0.81, 0.82), []WatchFill{{
		Key: "f1", Price: 0.785, Size: 1200, Side: "SELL", Timestamp: 1001,
	}})
	if hit == nil || math.Abs(hit.To-0.785) > 1e-9 {
		t.Fatalf("fill %+v", hit)
	}
	text := PriceWatchPingText(*hit)
	if !strings.Contains(text, "SELL 1.2k @ 78.5¢") || !strings.Contains(text, "via fill") {
		t.Fatalf("%s", text)
	}
	// Old midpoint is now a full 3¢ from the fill anchor, but it has not changed.
	hit, next = EvaluatePriceWatch(next, bookAt(0.81, 0.82), []WatchFill{{
		Key: "f1", Price: 0.785, Size: 1200, Side: "SELL", Timestamp: 1001,
	}})
	if hit != nil {
		t.Fatal("latched mid / seen fill should stay quiet")
	}
	// A mid tick that is still a full 3¢ outside the fill anchor stays quiet.
	hit, _ = EvaluatePriceWatch(next, bookAt(0.80, 0.84), nil)
	if hit != nil {
		t.Fatalf("latched mid twitch %+v", hit)
	}
}

func TestPriceWatchOvershootAnchorsOnPrint(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	hit, next := EvaluatePriceWatch(w, bookAt(0.84, 0.85), []WatchFill{{
		Key: "gap", Price: 0.90, Size: 10, Side: "BUY", Timestamp: 1002,
	}})
	if hit == nil || math.Abs(hit.To-0.90) > 1e-9 {
		t.Fatalf("want fill past mid, got %+v", hit)
	}
	hit, _ = EvaluatePriceWatch(next, bookAt(0.84, 0.85), nil)
	if hit != nil {
		t.Fatal("mid already outside the new anchor should not echo")
	}
}

func TestPriceWatchWideQuoteStaysLatched(t *testing.T) {
	w := armedWatch(0.70, 0.93) // mid 0.815, ask already > 3¢ away
	if !w.AskLatched || !w.BidLatched {
		t.Fatalf("latches bid=%v ask=%v", w.BidLatched, w.AskLatched)
	}
	hit, next := EvaluatePriceWatch(w, bookAt(0.70, 0.94), nil)
	if hit != nil {
		t.Fatal("twitch of an already-far ask")
	}
	hit, _ = EvaluatePriceWatch(next, bookAt(0.80, 0.83), nil)
	if hit != nil {
		t.Fatalf("returning inside is not a ping: %+v", hit)
	}
}

func TestPriceWatchIgnoresSmallFill(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	hit, next := EvaluatePriceWatch(w, bookAt(0.81, 0.82), []WatchFill{{
		Key: "dust", Price: 0.82, Size: 5000, Side: "BUY", Timestamp: 1005,
	}})
	if hit != nil {
		t.Fatal("fill inside the band")
	}
	if next.TradeUnix != 1005 {
		t.Fatalf("watermark %d", next.TradeUnix)
	}
}

func TestUpsertAndRemovePriceWatch(t *testing.T) {
	s := &State{}
	s.UpsertPriceWatch(PriceWatch{ConditionID: "0x1", Title: "Merz December", Outcome: "No", Anchor: 0.815, Delta: 0.03})
	s.UpsertPriceWatch(PriceWatch{ConditionID: "0x1", Title: "Merz December", Outcome: "Yes", Anchor: 0.2, Delta: 0.03})
	s.UpsertPriceWatch(PriceWatch{ConditionID: "0x1", Title: "Merz December", Outcome: "No", Anchor: 0.80, Delta: 0.05})
	if len(s.PriceWatches) != 2 || s.PriceWatches[1].Delta != 0.05 {
		t.Fatalf("replace side %+v", s.PriceWatches)
	}
	if _, ok := s.RemovePriceWatch("Merz"); ok {
		t.Fatal("both sides match")
	}
	got, ok := s.RemovePriceWatch("Merz NO")
	if !ok || got.Outcome != "No" {
		t.Fatalf("remove no %+v %v", got, ok)
	}
	got, ok = s.RemovePriceWatch("")
	if !ok || got.Outcome != "Yes" {
		t.Fatalf("remove last %+v %v", got, ok)
	}
	list := FormatPriceWatchList(nil)
	if !strings.Contains(list, "No price watches") {
		t.Fatal(list)
	}
}

type fakeWatchAPI struct {
	book    polymarket.OutcomeBook
	trades  []polymarket.Trade
	bookErr error
}

func (f *fakeWatchAPI) FetchOutcomeBook(context.Context, string, string) (polymarket.OutcomeBook, error) {
	return f.book, f.bookErr
}

func (f *fakeWatchAPI) FetchTrades(context.Context, polymarket.FetchTradesOptions) ([]polymarket.Trade, bool, error) {
	return f.trades, false, nil
}

func TestCheckPriceWatchesFiltersOutcome(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	api := &fakeWatchAPI{
		book: bookAt(0.81, 0.82),
		trades: []polymarket.Trade{
			{Outcome: "Yes", Price: 0.10, Size: 5, Timestamp: 1001, TransactionHash: "y"},
			{Outcome: "No", Price: 0.86, Size: 40, Side: "BUY", Timestamp: 1002, TransactionHash: "n"},
		},
	}
	fired, next := CheckPriceWatches(context.Background(), api, []PriceWatch{w}, time.Unix(1003, 0))
	if len(fired) != 1 || math.Abs(fired[0].To-0.86) > 1e-9 {
		t.Fatalf("%+v", fired)
	}
	if len(next) != 1 || math.Abs(next[0].Anchor-0.86) > 1e-9 {
		t.Fatalf("anchor %+v", next)
	}
	s := &State{PriceWatches: next}
	if !PriceWatchStillArmed(s, fired[0].Watch) {
		t.Fatal("armed")
	}
	s.PriceWatches[0].SetUnix++
	if PriceWatchStillArmed(s, fired[0].Watch) {
		t.Fatal("replaced watch should drop the in-flight ping")
	}
}
