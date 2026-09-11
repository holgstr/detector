package alert

import (
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
