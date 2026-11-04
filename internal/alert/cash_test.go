package alert

import (
	"context"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
)

func TestResolveCashWallet(t *testing.T) {
	if _, msg := ResolveCashWallet(context.Background(), nil, ""); msg != CashUsage {
		t.Fatalf("empty %q", msg)
	}
	if _, msg := ResolveCashWallet(context.Background(), nil, "all"); msg != CashUsage {
		t.Fatalf("all %q", msg)
	}
	w, msg := ResolveCashWallet(context.Background(), nil, "Flip")
	if msg != "" || w.Name != "Flipadelphia" {
		t.Fatalf("%+v %q", w, msg)
	}
	addr := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	api := fakeUsers{byName: map[string][]polymarket.UserProfile{
		"newsharp": {{Address: addr, Name: "NewSharp"}},
	}}
	w, msg = ResolveCashWallet(context.Background(), api, "NewSharp")
	if msg != "" || w.Address != addr || w.Name != "NewSharp" {
		t.Fatalf("%+v %q", w, msg)
	}
}

func TestFormatCash(t *testing.T) {
	got := FormatCash("Flipadelphia", 1053394.836011)
	if got != "Flipadelphia\nCash $1,053,394.84" {
		t.Fatalf("%q", got)
	}
	if FormatCash("  ", 0) != "Wallet\nCash $0.00" {
		t.Fatal(FormatCash("  ", 0))
	}
	if !strings.Contains(FormatCash("denizz", 12.5), "$12.50") {
		t.Fatal(FormatCash("denizz", 12.5))
	}
}

func TestHelpTextListsCash(t *testing.T) {
	if !strings.Contains(HelpText(100), "/cash <trader>") {
		t.Fatal(HelpText(100))
	}
}
