package main

import (
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/alert"
)

func TestCommandRepliesUnknownPings(t *testing.T) {
	got := commandReplies(false, alert.ParsedCommand{}, 100)
	if len(got) != 1 || !strings.Contains(got[0], "Still here") {
		t.Fatalf("%q", got)
	}
}

func TestCommandRepliesFirstMessageIsWelcome(t *testing.T) {
	got := commandReplies(true, alert.ParsedCommand{}, 100)
	if len(got) != 1 || !strings.Contains(got[0], "Watching") {
		t.Fatalf("%q", got)
	}
}

func TestCommandRepliesHelp(t *testing.T) {
	got := commandReplies(false, alert.ParsedCommand{Cmd: alert.CmdHelp}, 50)
	if len(got) != 1 || !strings.Contains(got[0], "/net") || !strings.Contains(got[0], "/pos") || !strings.Contains(got[0], "/holders") || !strings.Contains(got[0], "/port") || !strings.Contains(got[0], "/lasttrades") || !strings.Contains(got[0], "/kelly") || !strings.Contains(got[0], "/ob") || !strings.Contains(got[0], "/obp") || !strings.Contains(got[0], "/obk") || !strings.Contains(got[0], "/alert") || !strings.Contains(got[0], "/pricewatch") || !strings.Contains(got[0], "/tracked") || !strings.Contains(got[0], "/add") || !strings.Contains(got[0], "/unadd") || !strings.Contains(got[0], "/update") {
		t.Fatalf("%q", got)
	}
}

func TestStickMarketReusesLastNamed(t *testing.T) {
	var last string

	cmd, last := stickMarket(alert.ParsedCommand{Cmd: alert.CmdOB, Market: " Aliens "}, last)
	if cmd.Market != "Aliens" || last != "Aliens" {
		t.Fatalf("named /ob: cmd=%q last=%q", cmd.Market, last)
	}
	for _, c := range []alert.Command{alert.CmdOBK, alert.CmdOBP, alert.CmdOB, alert.CmdPos, alert.CmdHolders} {
		cmd, last = stickMarket(alert.ParsedCommand{Cmd: c}, last)
		if cmd.Market != "Aliens" || last != "Aliens" {
			t.Fatalf("bare %d: cmd=%q last=%q", c, cmd.Market, last)
		}
	}

	cmd, last = stickMarket(alert.ParsedCommand{Cmd: alert.CmdPos, Market: "Mars"}, last)
	if cmd.Market != "Mars" || last != "Mars" {
		t.Fatalf("named /pos: cmd=%q last=%q", cmd.Market, last)
	}
	cmd, last = stickMarket(alert.ParsedCommand{Cmd: alert.CmdOB}, last)
	if cmd.Market != "Mars" {
		t.Fatalf("bare /ob after /pos: %q", cmd.Market)
	}

	cmd, last = stickMarket(alert.ParsedCommand{Cmd: alert.CmdAlert, Price: 0.32, MinSize: 1000}, last)
	if cmd.Market != "Mars" || last != "Mars" {
		t.Fatalf("/alert price size: cmd=%q last=%q", cmd.Market, last)
	}
	cmd, last = stickMarket(alert.ParsedCommand{Cmd: alert.CmdPriceWatch, Outcome: "No", Delta: 0.03}, last)
	if cmd.Market != "Mars" || last != "Mars" {
		t.Fatalf("/pricewatch side: cmd=%q last=%q", cmd.Market, last)
	}

	bareAlert := alert.ParsedCommand{Cmd: alert.CmdAlert}
	cmd, last = stickMarket(bareAlert, last)
	if cmd.Market != "" || last != "Mars" {
		t.Fatalf("bare /alert lists: cmd=%q last=%q", cmd.Market, last)
	}
	cmd, last = stickMarket(alert.ParsedCommand{Cmd: alert.CmdPriceWatch}, last)
	if cmd.Market != "" || last != "Mars" {
		t.Fatalf("bare /pricewatch lists: cmd=%q last=%q", cmd.Market, last)
	}
	cmd, last = stickMarket(alert.ParsedCommand{Cmd: alert.CmdUnalert}, last)
	if cmd.Market != "" || last != "Mars" {
		t.Fatalf("bare /unalert: cmd=%q last=%q", cmd.Market, last)
	}
	cmd, last = stickMarket(alert.ParsedCommand{Cmd: alert.CmdLastTrades}, last)
	if cmd.Market != "" || last != "Mars" {
		t.Fatalf("bare /lasttrades: cmd=%q last=%q", cmd.Market, last)
	}
	cmd, last = stickMarket(alert.ParsedCommand{Cmd: alert.CmdHelp}, last)
	if last != "Mars" {
		t.Fatalf("/help cleared memory: %q", last)
	}
	cmd, _ = stickMarket(alert.ParsedCommand{Cmd: alert.CmdHolders}, "")
	if cmd.Market != "" {
		t.Fatalf("no history: %q", cmd.Market)
	}
	cmd, last = stickMarket(alert.ParsedCommand{Cmd: alert.CmdHolders, Market: "Venus"}, "Mars")
	if cmd.Market != "Venus" || last != "Venus" {
		t.Fatalf("named /holders: cmd=%q last=%q", cmd.Market, last)
	}
}

func TestApplyMarketStickPersistsLastNamedMarket(t *testing.T) {
	b := &bot{state: &alert.State{LastOBQuery: "OldBook"}}
	cmd, note := b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdHolders}, "")
	if note != "" || cmd.Market != "OldBook" || b.state.LastMarketQuery != "OldBook" {
		t.Fatalf("legacy ob memory %+v cmd %+v note %q", b.state, cmd, note)
	}
	cmd, note = b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdOB, Market: "Aliens"}, "")
	if note != "" || cmd.Market != "Aliens" || b.state.LastMarketQuery != "Aliens" || b.state.LastOBQuery != "Aliens" {
		t.Fatalf("state %+v cmd %+v", b.state, cmd)
	}
	cmd, note = b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdPos}, "")
	if note != "" || cmd.Market != "Aliens" || b.state.LastMarketQuery != "Aliens" {
		t.Fatalf("inherit %+v cmd %+v", b.state, cmd)
	}
}

func TestApplyMarketStickUsesReply(t *testing.T) {
	fill := alert.Format(alert.Alert{
		Name: "SnowLover7", Side: "BUY", Outcome: "No", Size: 32000, Price: 0.32,
		Title: "Fed decision in September?",
	})
	b := &bot{state: &alert.State{LastMarketQuery: "Aliens"}}
	cmd, note := b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdOB}, fill)
	if note != "" || cmd.Market != "Fed decision in September?" || b.state.LastMarketQuery != "Fed decision in September?" {
		t.Fatalf("reply %+v note %q state %+v", cmd, note, b.state)
	}
	cmd, note = b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdOB, Market: "Mars"}, fill)
	if note != "" || cmd.Market != "Mars" {
		t.Fatalf("explicit wins %+v", cmd)
	}
	many := "Snow\n+10 YES  Fed decision?\n\nBob\n-5 NO  Other market"
	cmd, note = b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdOB}, many)
	if note == "" || cmd.Market != "" || b.state.LastMarketQuery != "Mars" {
		t.Fatalf("ambiguous cmd %+v note %q state %+v", cmd, note, b.state)
	}
}

func TestCommandRepliesKelly(t *testing.T) {
	got := commandReplies(false, alert.ParsedCommand{Cmd: alert.CmdKelly, Price: 0.40, FV: 0.50}, 100)
	if len(got) != 1 || !strings.Contains(got[0], "Full") || strings.Contains(got[0], "Kelly") {
		t.Fatalf("%q", got)
	}
	got = commandReplies(false, alert.ParsedCommand{Cmd: alert.CmdKelly}, 100)
	if len(got) != 1 || !strings.Contains(got[0], "Usage: /kelly") {
		t.Fatalf("%q", got)
	}
}
