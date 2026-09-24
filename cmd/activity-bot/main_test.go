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

func TestStickOrderBookMarketReusesLastNamedOnTheOtherBooks(t *testing.T) {
	var lastQ string
	var lastC alert.Command

	cmd, lastQ, lastC := stickOrderBookMarket(alert.ParsedCommand{Cmd: alert.CmdOB, Market: " Aliens "}, lastQ, lastC)
	if cmd.Market != "Aliens" || lastQ != "Aliens" || lastC != alert.CmdOB {
		t.Fatalf("named /ob: cmd=%q last=%q which=%d", cmd.Market, lastQ, lastC)
	}

	cmd, lastQ, lastC = stickOrderBookMarket(alert.ParsedCommand{Cmd: alert.CmdOBK}, lastQ, lastC)
	if cmd.Market != "Aliens" || lastQ != "Aliens" || lastC != alert.CmdOB {
		t.Fatalf("bare /obk: cmd=%q last=%q which=%d", cmd.Market, lastQ, lastC)
	}

	cmd, lastQ, lastC = stickOrderBookMarket(alert.ParsedCommand{Cmd: alert.CmdOBP}, lastQ, lastC)
	if cmd.Market != "Aliens" || lastQ != "Aliens" || lastC != alert.CmdOB {
		t.Fatalf("bare /obp: cmd=%q last=%q which=%d", cmd.Market, lastQ, lastC)
	}

	cmd, lastQ, lastC = stickOrderBookMarket(alert.ParsedCommand{Cmd: alert.CmdOB}, lastQ, lastC)
	if cmd.Market != "" || lastC != alert.CmdOB {
		t.Fatalf("bare /ob should still ask for a market: cmd=%q which=%d", cmd.Market, lastC)
	}

	cmd, lastQ, lastC = stickOrderBookMarket(alert.ParsedCommand{Cmd: alert.CmdOBK, Market: "Mars"}, lastQ, lastC)
	if cmd.Market != "Mars" || lastQ != "Mars" || lastC != alert.CmdOBK {
		t.Fatalf("named /obk: cmd=%q last=%q which=%d", cmd.Market, lastQ, lastC)
	}
	cmd, lastQ, lastC = stickOrderBookMarket(alert.ParsedCommand{Cmd: alert.CmdOB}, lastQ, lastC)
	if cmd.Market != "Mars" {
		t.Fatalf("bare /ob after /obk Mars: %q", cmd.Market)
	}

	cmd, _, _ = stickOrderBookMarket(alert.ParsedCommand{Cmd: alert.CmdOBP}, "", alert.CmdNone)
	if cmd.Market != "" {
		t.Fatalf("no history: %q", cmd.Market)
	}

	other := alert.ParsedCommand{Cmd: alert.CmdPos, Market: "Aliens"}
	got, q, which := stickOrderBookMarket(other, "Aliens", alert.CmdOB)
	if got != other || q != "Aliens" || which != alert.CmdOB {
		t.Fatalf("non-book command changed memory: %+v %q %d", got, q, which)
	}
}

func TestApplyOBStickPersistsLastNamedMarket(t *testing.T) {
	b := &bot{state: &alert.State{}}
	cmd := b.applyOBStick(alert.ParsedCommand{Cmd: alert.CmdOB, Market: "Aliens"})
	if cmd.Market != "Aliens" || b.state.LastOBQuery != "Aliens" || b.state.LastOBCmd != "ob" {
		t.Fatalf("state %+v cmd %+v", b.state, cmd)
	}
	cmd = b.applyOBStick(alert.ParsedCommand{Cmd: alert.CmdOBP})
	if cmd.Market != "Aliens" || b.state.LastOBCmd != "ob" {
		t.Fatalf("inherit %+v cmd %+v", b.state, cmd)
	}
	cmd = b.applyOBStick(alert.ParsedCommand{Cmd: alert.CmdOBK})
	if cmd.Market != "Aliens" || b.state.LastOBCmd != "ob" {
		t.Fatalf("third %+v cmd %+v", b.state, cmd)
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
