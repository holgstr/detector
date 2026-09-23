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
	hit, next := EvaluatePriceWatch(w, bookAt(0.82, 0.83))
	if hit != nil {
		t.Fatalf("1¢ move should be quiet: %+v", hit)
	}

	hit, next = EvaluatePriceWatch(next, bookAt(0.84, 0.85))
	if hit == nil {
		t.Fatal("expected 84.5 ping")
	}
	if math.Abs(hit.From-0.815) > 1e-9 || math.Abs(hit.To-0.845) > 1e-9 {
		t.Fatalf("from %v to %v", hit.From, hit.To)
	}
	text := PriceWatchPingText(*hit)
	want := FormatOBReport(OBReport{Title: "Merz December", Books: []polymarket.OutcomeBook{bookForDisplay(hit.Book)}})
	if text != want {
		t.Fatalf("want /ob body:\n%s\ngot:\n%s", want, text)
	}
	if !strings.HasPrefix(text, "Merz December\n") || !strings.Contains(text, "NO") || !strings.Contains(text, "85¢") || !strings.Contains(text, "84¢") {
		t.Fatalf("%s", text)
	}
	if strings.Contains(text, "via mid") || strings.Contains(text, "next ") {
		t.Fatalf("old ping format leaked:\n%s", text)
	}

	hit, next = EvaluatePriceWatch(next, bookAt(0.84, 0.85))
	if hit != nil {
		t.Fatal("same price should not ping again")
	}
	hit, _ = EvaluatePriceWatch(next, bookAt(0.87, 0.88))
	if hit == nil || math.Abs(hit.To-0.875) > 1e-9 || math.Abs(hit.From-0.845) > 1e-9 {
		t.Fatalf("next step %+v", hit)
	}
}

func TestPriceWatchDownMove(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	hit, _ := EvaluatePriceWatch(w, bookAt(0.78, 0.79))
	if hit == nil || math.Abs(hit.To-0.785) > 1e-9 {
		t.Fatalf("%+v", hit)
	}
	text := PriceWatchPingText(*hit)
	if !strings.HasPrefix(text, "Merz December\n") || !strings.Contains(text, "79¢") || !strings.Contains(text, "78¢") {
		t.Fatalf("%s", text)
	}
	if strings.Contains(text, "-3¢") || strings.Contains(text, "NO 78.5¢") {
		t.Fatalf("old ping format leaked:\n%s", text)
	}
}

func TestPriceWatchSpreadOrPrintWithoutMidMove(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	// Ask walks to the +3¢ band. Mid only moves 1.75¢, so the price has not.
	hit, next := EvaluatePriceWatch(w, bookAt(0.81, 0.845))
	if hit != nil {
		t.Fatalf("ask widen should stay quiet: %+v", hit)
	}
	if math.Abs(next.Anchor-0.815) > 1e-9 {
		t.Fatalf("anchor %v", next.Anchor)
	}
	hit, _ = EvaluatePriceWatch(next, bookAt(0.81, 0.82))
	if hit != nil {
		t.Fatal("spread returning to the same book should stay quiet")
	}

	// A one-sided quote at the band is the price when the other side is gone.
	w = armedWatch(0.81, 0.82)
	oneSided := bookAt(0.845, 0.99)
	oneSided.Asks = nil
	hit, next = EvaluatePriceWatch(w, oneSided)
	if hit == nil || math.Abs(hit.To-0.845) > 1e-9 {
		t.Fatalf("one-sided bid %+v", hit)
	}
	hit, _ = EvaluatePriceWatch(next, oneSided)
	if hit != nil {
		t.Fatal("same one-sided bid should not ping again")
	}
}

func TestPriceWatchRepeatedBookAfterGap(t *testing.T) {
	w := armedWatch(0.70, 0.93) // mid 0.815; both sides already outside a 3¢ band
	empty := polymarket.OutcomeBook{Outcome: "No", Tick: 0.01}
	for i := 0; i < 4; i++ {
		hit, next := EvaluatePriceWatch(w, empty)
		if hit != nil {
			t.Fatalf("empty book ping %d: %+v", i, hit)
		}
		w = next
		hit, next = EvaluatePriceWatch(w, bookAt(0.70, 0.93))
		if hit != nil {
			t.Fatalf("unchanged book after gap %d: %+v", i, hit)
		}
		w = next
	}
	hit, next := EvaluatePriceWatch(w, bookAt(0.84, 0.85))
	if hit == nil || math.Abs(hit.To-0.845) > 1e-9 {
		t.Fatalf("move across a gap %+v", hit)
	}
	hit, _ = EvaluatePriceWatch(next, bookAt(0.84, 0.85))
	if hit != nil {
		t.Fatal("same price after the move")
	}
}

func TestPriceWatchWideQuoteStaysLatched(t *testing.T) {
	w := armedWatch(0.70, 0.93) // mid 0.815, ask already > 3¢ away
	if !w.AskLatched || !w.BidLatched {
		t.Fatalf("latches bid=%v ask=%v", w.BidLatched, w.AskLatched)
	}
	hit, next := EvaluatePriceWatch(w, bookAt(0.70, 0.94))
	if hit != nil {
		t.Fatal("twitch of an already-far ask")
	}
	hit, _ = EvaluatePriceWatch(next, bookAt(0.80, 0.83))
	if hit != nil {
		t.Fatalf("returning inside is not a ping: %+v", hit)
	}
}

func TestPriceWatchUnchangedBookStaysQuiet(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	w.Anchor = 0.785 // previous print pulled the anchor off a book that never moved
	hit, next := EvaluatePriceWatch(w, bookAt(0.81, 0.82))
	if hit != nil {
		t.Fatalf("same book %+v", hit)
	}
	empty := polymarket.OutcomeBook{Outcome: "No", Tick: 0.01}
	hit, next = EvaluatePriceWatch(next, empty)
	if hit != nil {
		t.Fatal("empty book")
	}
	hit, _ = EvaluatePriceWatch(next, bookAt(0.81, 0.82))
	if hit != nil {
		t.Fatalf("reprint after a gap %+v", hit)
	}
}

func TestPriceWatchOneSidedThenMidReturns(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	spike := bookAt(0.90, 0.99)
	spike.Asks = nil
	hit, next := EvaluatePriceWatch(w, spike)
	if hit == nil || math.Abs(hit.To-0.90) > 1e-9 {
		t.Fatalf("one-sided spike %+v", hit)
	}
	// Back to the original two-sided book: the midpoint moved by more than 3¢
	// from the spike, so this is a real move, not a repeat of the old book.
	hit, next = EvaluatePriceWatch(next, bookAt(0.81, 0.82))
	if hit == nil || math.Abs(hit.To-0.815) > 1e-9 {
		t.Fatalf("return %+v", hit)
	}
	hit, _ = EvaluatePriceWatch(next, bookAt(0.81, 0.82))
	if hit != nil {
		t.Fatal("settled book should stay quiet")
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
	book polymarket.OutcomeBook
}

func (f *fakeWatchAPI) FetchOutcomeBook(context.Context, string, string) (polymarket.OutcomeBook, error) {
	return f.book, nil
}

func TestCheckPriceWatches(t *testing.T) {
	w := armedWatch(0.81, 0.82)
	api := &fakeWatchAPI{book: bookAt(0.84, 0.85)}
	fired, next := CheckPriceWatches(context.Background(), api, []PriceWatch{w}, time.Unix(1003, 0))
	if len(fired) != 1 || math.Abs(fired[0].To-0.845) > 1e-9 {
		t.Fatalf("%+v", fired)
	}
	if len(next) != 1 || math.Abs(next[0].Anchor-0.845) > 1e-9 {
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
