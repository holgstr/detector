package alert

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

type fakeAlertAPI struct {
	hit   polymarket.SearchMarket
	book  polymarket.OutcomeBook
	err   error
	calls int
}

func (f *fakeAlertAPI) FindMarket(context.Context, string) (polymarket.SearchMarket, error) {
	return f.hit, f.err
}

func (f *fakeAlertAPI) FetchYesBook(context.Context, string) (polymarket.OutcomeBook, error) {
	f.calls++
	return f.book, f.err
}

func TestParseAlertConfirm(t *testing.T) {
	p, n, ok := ParseAlertConfirm("32 1000")
	if !ok || p != 0.32 || n != 1000 {
		t.Fatalf("%v %v %v", p, n, ok)
	}
	p, n, ok = ParseAlertConfirm("0.32 1k")
	if !ok || p != 0.32 || n != 1000 {
		t.Fatalf("1k %v %v %v", p, n, ok)
	}
	p, n, ok = ParseAlertConfirm("32c 1.5k")
	if !ok || p != 0.32 || n != 1500 {
		t.Fatalf("cents k %v %v %v", p, n, ok)
	}
	if _, _, ok := ParseAlertConfirm("32"); ok {
		t.Fatal("one token")
	}
	if _, _, ok := ParseAlertConfirm("0 1000"); ok {
		t.Fatal("zero price")
	}
	if _, _, ok := ParseAlertConfirm("hello 1000"); ok {
		t.Fatal("bad price")
	}
}

func TestShouldFirePriceAlert(t *testing.T) {
	a := PriceAlert{Price: 0.32, MinSize: 1000}
	now := time.Unix(1_700_000_000, 0)
	if ShouldFirePriceAlert(a, 999, now) {
		t.Fatal("below floor")
	}
	if !ShouldFirePriceAlert(a, 1000, now) {
		t.Fatal("first hit")
	}
	a.LastNotifiedUnix = now.Unix()
	if ShouldFirePriceAlert(a, 2000, now.Add(59*time.Minute)) {
		t.Fatal("cooldown")
	}
	if !ShouldFirePriceAlert(a, 2000, now.Add(time.Hour)) {
		t.Fatal("repeat")
	}
}

func TestCheckPriceAlertsIgnoresBids(t *testing.T) {
	api := &fakeAlertAPI{book: polymarket.OutcomeBook{
		Outcome: "Yes",
		Bids:    []polymarket.BookLevel{{Price: 0.32, Size: 99999}},
		Asks:    []polymarket.BookLevel{{Price: 0.40, Size: 10}},
	}}
	alerts := []PriceAlert{{ConditionID: "0xabc", Price: 0.32, MinSize: 1000}}
	fired, _ := CheckPriceAlerts(context.Background(), api, alerts, time.Unix(10, 0))
	if len(fired) != 0 {
		t.Fatalf("bids must not trigger take alert: %+v", fired)
	}
}

func TestCheckPriceAlertsFiresThenCooldown(t *testing.T) {
	api := &fakeAlertAPI{book: polymarket.OutcomeBook{
		Outcome: "Yes",
		Bids: []polymarket.BookLevel{
			{Price: 0.32, Size: 99999},
		},
		Asks: []polymarket.BookLevel{
			{Price: 0.32, Size: 400},
			{Price: 0.30, Size: 700},
		},
	}}
	alerts := []PriceAlert{{
		Title:       "Aliens?",
		ConditionID: "0xabc",
		Price:       0.32,
		MinSize:     1000,
	}}
	now := time.Unix(1_700_000_000, 0)
	fired, next := CheckPriceAlerts(context.Background(), api, alerts, now)
	if len(fired) != 1 || fired[0].Size != 1100 {
		t.Fatalf("fired=%+v", fired)
	}
	if next[0].LastNotifiedUnix != now.Unix() {
		t.Fatalf("stamp %+v", next[0])
	}
	fired, next = CheckPriceAlerts(context.Background(), api, next, now.Add(time.Minute))
	if len(fired) != 0 {
		t.Fatalf("cooldown fired %+v", fired)
	}
	fired, _ = CheckPriceAlerts(context.Background(), api, next, now.Add(time.Hour))
	if len(fired) != 1 {
		t.Fatalf("hourly %+v", fired)
	}
}

func TestCheckPriceAlertsSkipsFetchErrors(t *testing.T) {
	api := &fakeAlertAPI{err: context.DeadlineExceeded, book: polymarket.OutcomeBook{}}
	alerts := []PriceAlert{{ConditionID: "0xabc", Price: 0.4, MinSize: 1}}
	fired, next := CheckPriceAlerts(context.Background(), api, alerts, time.Unix(10, 0))
	if len(fired) != 0 || len(next) != 1 || next[0].LastNotifiedUnix != 0 {
		t.Fatalf("fired=%+v next=%+v", fired, next)
	}
}

func TestUpsertAndRemovePriceAlert(t *testing.T) {
	s := &State{}
	s.PendingPriceAlert = &PriceAlertDraft{Title: "x"}
	s.UpsertPriceAlert(PriceAlert{ConditionID: "0x1", Title: "A", Price: 0.3, MinSize: 10})
	if s.PendingPriceAlert != nil || len(s.PriceAlerts) != 1 {
		t.Fatal("upsert")
	}
	s.UpsertPriceAlert(PriceAlert{ConditionID: "0x1", Title: "A", Price: 0.2, MinSize: 50})
	if len(s.PriceAlerts) != 1 || s.PriceAlerts[0].Price != 0.2 {
		t.Fatalf("replace %+v", s.PriceAlerts)
	}
	s.UpsertPriceAlert(PriceAlert{ConditionID: "0x2", Title: "B Magdalena", Price: 0.4, MinSize: 1})
	if _, ok := s.RemovePriceAlert("Magdalena"); !ok {
		t.Fatal("remove magdalena")
	}
	got, ok := s.RemovePriceAlert("")
	if !ok || got.ConditionID != "0x1" {
		t.Fatalf("remove last %+v %v", got, ok)
	}
}

func TestPriceAlertPromptAndPing(t *testing.T) {
	text := PriceAlertPrompt(PriceAlertDraft{Title: "Aliens?", URL: "https://polymarket.com/event/x"})
	if !strings.Contains(text, "Aliens?") || !strings.Contains(text, "32 1000") || !strings.Contains(text, "asks") {
		t.Fatalf("%q", text)
	}
	ping := PriceAlertPingText(PriceAlert{Title: "Aliens?", Price: 0.32, MinSize: 1000}, 1500)
	if !strings.Contains(ping, "Ask alert") || !strings.Contains(ping, "1.5k") || !strings.Contains(ping, "32¢") {
		t.Fatalf("%q", ping)
	}
	list := FormatPriceAlertList(nil)
	if !strings.Contains(list, "No price alerts") {
		t.Fatalf("%q", list)
	}
}

func TestDraftFromMarket(t *testing.T) {
	d := DraftFromMarket("aliens", polymarket.SearchMarket{
		Market: polymarket.Market{Question: "Aliens?", Slug: "aliens", ConditionID: "0xabc", URL: "https://x"},
	})
	if d.Title != "Aliens?" || d.ConditionID != "0xabc" {
		t.Fatalf("%+v", d)
	}
}
