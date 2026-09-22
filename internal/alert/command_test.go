package alert

import (
	"strings"
	"testing"
	"time"
)

func TestParseCommand(t *testing.T) {
	if ParseCommand("/start").Cmd != CmdStart {
		t.Fatal("start")
	}
	if ParseCommand("/help").Cmd != CmdHelp {
		t.Fatal("help")
	}
	if ParseCommand("/minsize").Cmd != CmdMinSizeShow {
		t.Fatal("show")
	}
	got := ParseCommand("/minsize@detectx_bot 100")
	if got.Cmd != CmdMinSizeSet || got.MinUSD != 100 {
		t.Fatalf("%+v", got)
	}
	got = ParseCommand("/min $40")
	if got.Cmd != CmdMinSizeSet || got.MinUSD != 40 {
		t.Fatalf("%+v", got)
	}
	got = ParseCommand("minsize 1,250")
	if got.Cmd != CmdMinSizeSet || got.MinUSD != 1250 {
		t.Fatalf("%+v", got)
	}
	if ParseCommand("hello").Cmd != CmdNone {
		t.Fatal("unknown")
	}
	if ParseCommand("/minsize nope").Cmd != CmdHelp {
		t.Fatal("bad amount → help")
	}

	got = ParseCommand("/net")
	if got.Cmd != CmdNet || got.Window != 24*time.Hour || got.Trader != "" {
		t.Fatalf("net default %+v", got)
	}
	got = ParseCommand("/net Flip 6h")
	if got.Cmd != CmdNet || got.Window != 6*time.Hour || got.Trader != "Flip" {
		t.Fatalf("name then window %+v", got)
	}
	got = ParseCommand("/net 6h Flip")
	if got.Cmd != CmdNet || got.Window != 6*time.Hour || got.Trader != "Flip" {
		t.Fatalf("window then name %+v", got)
	}
	got = ParseCommand("/net Flipadelphia 12")
	if got.Cmd != CmdNet || got.Window != 12*time.Hour || got.Trader != "Flipadelphia" {
		t.Fatalf("%+v", got)
	}
	got = ParseCommand("/net 1d")
	if got.Cmd != CmdNet || got.Window != 24*time.Hour {
		t.Fatalf("%+v", got)
	}
	got = ParseCommand("/net all")
	if got.Cmd != CmdNet || got.Trader != "" {
		t.Fatalf("all %+v", got)
	}
	if ParseCommand("/net 6h 12h").Cmd != CmdHelp {
		t.Fatal("two windows → help")
	}

	got = ParseCommand("/pos Andersson")
	if got.Cmd != CmdPos || got.Market != "Andersson" {
		t.Fatalf("pos %+v", got)
	}
	got = ParseCommand("/pos@detectx_bot Magdalena Andersson")
	if got.Cmd != CmdPos || got.Market != "Magdalena Andersson" {
		t.Fatalf("pos mention %+v", got)
	}
	got = ParseCommand("/holdings")
	if got.Cmd != CmdPos || got.Market != "" {
		t.Fatalf("pos empty %+v", got)
	}

	got = ParseCommand("/port Flip")
	if got.Cmd != CmdPort || got.Trader != "Flip" {
		t.Fatalf("port %+v", got)
	}
	got = ParseCommand("/portfolio@detectx_bot Flipadelphia")
	if got.Cmd != CmdPort || got.Trader != "Flipadelphia" {
		t.Fatalf("port mention %+v", got)
	}
	got = ParseCommand("/port")
	if got.Cmd != CmdPort || got.Trader != "" {
		t.Fatalf("port empty %+v", got)
	}

	if ParseCommand("/update").Cmd != CmdUpdate {
		t.Fatal("update")
	}
	if ParseCommand("/update@detectx_bot").Cmd != CmdUpdate {
		t.Fatal("update mention")
	}
	if ParseCommand("/pull").Cmd != CmdUpdate {
		t.Fatal("pull alias")
	}

	got = ParseCommand("/lasttrades")
	if got.Cmd != CmdLastTrades || got.Window != 24*time.Hour || got.Trader != "" || got.Market != "" {
		t.Fatalf("lasttrades default %+v", got)
	}
	got = ParseCommand("/lasttrades Flip 6h")
	if got.Cmd != CmdLastTrades || got.Window != 6*time.Hour || got.Trader != "Flip" || got.Market != "" {
		t.Fatalf("lasttrades trader window %+v", got)
	}
	got = ParseCommand("/trades 6h Flip Andersson")
	if got.Cmd != CmdLastTrades || got.Window != 6*time.Hour || got.Trader != "Flip" || got.Market != "Andersson" {
		t.Fatalf("lasttrades mixed %+v", got)
	}
	got = ParseCommand("/lasttrades@detectx_bot Magdalena Andersson 12h")
	if got.Cmd != CmdLastTrades || got.Window != 12*time.Hour || got.Trader != "Magdalena" || got.Market != "Andersson" {
		t.Fatalf("lasttrades market only %+v", got)
	}
	got = ParseCommand("/lasttrades all Andersson")
	if got.Cmd != CmdLastTrades || got.Trader != "" || got.Market != "Andersson" {
		t.Fatalf("lasttrades all market %+v", got)
	}
	got = ParseCommand("/last-trades Flipadelphia")
	if got.Cmd != CmdLastTrades || got.Trader != "Flipadelphia" || got.Market != "" {
		t.Fatalf("lasttrades trader only %+v", got)
	}
	if ParseCommand("/lasttrades 6h 12h").Cmd != CmdHelp {
		t.Fatal("two windows → help")
	}

	if ParseCommand("/tracked").Cmd != CmdTracked {
		t.Fatal("tracked")
	}
	if ParseCommand("/wallets@detectx_bot").Cmd != CmdTracked {
		t.Fatal("tracked mention")
	}
	got = ParseCommand("/add Flipadelphia")
	if got.Cmd != CmdAdd || got.Query != "Flipadelphia" {
		t.Fatalf("add %+v", got)
	}
	got = ParseCommand("/track@detectx_bot 0x448861155279dbf833d041b963e3ac854599e319")
	if got.Cmd != CmdAdd || !strings.HasPrefix(got.Query, "0x4488") {
		t.Fatalf("add addr %+v", got)
	}
	got = ParseCommand("/unadd Flip")
	if got.Cmd != CmdUnadd || got.Query != "Flip" {
		t.Fatalf("unadd %+v", got)
	}
	got = ParseCommand("/remove SnowLover7")
	if got.Cmd != CmdUnadd || got.Query != "SnowLover7" {
		t.Fatalf("remove %+v", got)
	}

	got = ParseCommand("/kelly 42 55")
	if got.Cmd != CmdKelly || got.Price != 0.42 || got.FV != 0.55 {
		t.Fatalf("kelly cents %+v", got)
	}
	got = ParseCommand("/kelly@detectx_bot 0.40 0.50")
	if got.Cmd != CmdKelly || got.Price != 0.40 || got.FV != 0.50 {
		t.Fatalf("kelly mention %+v", got)
	}
	if ParseCommand("/kelly").Cmd != CmdKelly || ParseCommand("/kelly").Price != 0 {
		t.Fatal("kelly usage")
	}
	if ParseCommand("/kelly 42").Cmd != CmdHelp {
		t.Fatal("kelly one arg → help")
	}

	got = ParseCommand("/ob Andersson")
	if got.Cmd != CmdOB || got.Market != "Andersson" {
		t.Fatalf("ob %+v", got)
	}
	got = ParseCommand("/book@detectx_bot Magdalena Andersson")
	if got.Cmd != CmdOB || got.Market != "Magdalena Andersson" {
		t.Fatalf("ob mention %+v", got)
	}
	if ParseCommand("/orderbook").Cmd != CmdOB {
		t.Fatal("orderbook alias")
	}
	got = ParseCommand("/obp florida governor")
	if got.Cmd != CmdOBP || got.Market != "florida governor" {
		t.Fatalf("obp %+v", got)
	}
	got = ParseCommand("/obp@detectx_bot FL_GOV_2026.REP")
	if got.Cmd != CmdOBP || got.Market != "FL_GOV_2026.REP" {
		t.Fatalf("obp mention %+v", got)
	}
	got = ParseCommand("/obk florida governor")
	if got.Cmd != CmdOBK || got.Market != "florida governor" {
		t.Fatalf("obk %+v", got)
	}
	got = ParseCommand("/obk@detectx_bot KXFEDDECISION-26OCT-H0")
	if got.Cmd != CmdOBK || got.Market != "KXFEDDECISION-26OCT-H0" {
		t.Fatalf("obk mention %+v", got)
	}

	got = ParseCommand("/alert Andersson")
	if got.Cmd != CmdAlert || got.Market != "Andersson" || got.Price != 0 {
		t.Fatalf("alert %+v", got)
	}
	got = ParseCommand("/alert@detectx_bot Magdalena Andersson 32 1000")
	if got.Cmd != CmdAlert || got.Market != "Magdalena Andersson" || got.Price != 0.32 || got.MinSize != 1000 {
		t.Fatalf("alert one shot %+v", got)
	}
	got = ParseCommand("/alert")
	if got.Cmd != CmdAlert || got.Market != "" {
		t.Fatalf("alert list %+v", got)
	}
	got = ParseCommand("/unalert Andersson")
	if got.Cmd != CmdUnalert || got.Market != "Andersson" {
		t.Fatalf("unalert %+v", got)
	}
	if ParseCommand("/cancel").Cmd != CmdCancel {
		t.Fatal("cancel")
	}

	got = ParseCommand("/pricewatch Merz December NO 3")
	if got.Cmd != CmdPriceWatch || got.Market != "Merz December" || got.Outcome != "No" || got.Delta != 0.03 {
		t.Fatalf("pricewatch %+v", got)
	}
	got = ParseCommand("/pricewatch@detectx_bot Merz December YES 1.5")
	if got.Cmd != CmdPriceWatch || got.Market != "Merz December" || got.Outcome != "Yes" || got.Delta != 0.015 {
		t.Fatalf("pricewatch mention %+v", got)
	}
	got = ParseCommand("/price-watch Merz December 3c NO")
	if got.Cmd != CmdPriceWatch || got.Market != "Merz December" || got.Outcome != "No" || got.Delta != 0.03 {
		t.Fatalf("pricewatch swapped %+v", got)
	}
	got = ParseCommand("/pricewatch")
	if got.Cmd != CmdPriceWatch || got.Market != "" || got.Delta != 0 {
		t.Fatalf("pricewatch list %+v", got)
	}
	if ParseCommand("/pricewatch Merz 3").Cmd != CmdHelp {
		t.Fatal("pricewatch missing side → help")
	}
	got = ParseCommand("/unpricewatch Merz December NO")
	if got.Cmd != CmdUnpriceWatch || got.Market != "Merz December NO" {
		t.Fatalf("unpricewatch %+v", got)
	}
}

func TestEffectiveMinUSD(t *testing.T) {
	s := &State{}
	if s.EffectiveMinUSD(100) != 100 {
		t.Fatal("fallback")
	}
	s.SetMinUSD(0)
	if s.EffectiveMinUSD(100) != 0 {
		t.Fatal("zero is a real override")
	}
	s.SetMinUSD(75)
	if s.EffectiveMinUSD(100) != 75 {
		t.Fatal("override")
	}
}
