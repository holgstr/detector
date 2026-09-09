package alert

import "testing"

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
