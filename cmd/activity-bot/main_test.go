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
	if len(got) != 1 || !strings.Contains(got[0], "/net") || !strings.Contains(got[0], "/pos") || !strings.Contains(got[0], "/holders") || !strings.Contains(got[0], "/port") || !strings.Contains(got[0], "/lasttrades") || !strings.Contains(got[0], "/kelly") || !strings.Contains(got[0], "/ob") || !strings.Contains(got[0], "/obp") || !strings.Contains(got[0], "/obk") || !strings.Contains(got[0], "/other") || !strings.Contains(got[0], "/alert") || !strings.Contains(got[0], "/pricewatch") || !strings.Contains(got[0], "/tracked") || !strings.Contains(got[0], "/add") || !strings.Contains(got[0], "/unadd") || !strings.Contains(got[0], "/update") {
		t.Fatalf("%q", got)
	}
}

func TestStickMarketReusesLastNamed(t *testing.T) {
	var mem marketMemory

	cmd, mem := stickMarket(alert.ParsedCommand{Cmd: alert.CmdOB, Market: " Aliens "}, mem)
	if cmd.Market != "Aliens" || mem.Precise != "Aliens" || mem.Search != "Aliens" {
		t.Fatalf("named /ob: cmd=%q mem=%+v", cmd.Market, mem)
	}
	for _, c := range []alert.Command{alert.CmdOBK, alert.CmdOBP, alert.CmdOB, alert.CmdPos, alert.CmdHolders} {
		cmd, mem = stickMarket(alert.ParsedCommand{Cmd: c}, mem)
		if cmd.Market != "Aliens" || mem.Precise != "Aliens" || mem.Search != "Aliens" {
			t.Fatalf("bare %d: cmd=%q mem=%+v", c, cmd.Market, mem)
		}
	}

	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdPos, Market: "Mars"}, mem)
	if cmd.Market != "Mars" || mem.Precise != "Mars" || mem.Search != "Mars" {
		t.Fatalf("named /pos: cmd=%q mem=%+v", cmd.Market, mem)
	}
	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdOB}, mem)
	if cmd.Market != "Mars" {
		t.Fatalf("bare /ob after /pos: %q", cmd.Market)
	}

	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdAlert, Price: 0.32, MinSize: 1000}, mem)
	if cmd.Market != "Mars" || mem.Precise != "Mars" {
		t.Fatalf("/alert price size: cmd=%q mem=%+v", cmd.Market, mem)
	}
	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdPriceWatch, Outcome: "No", Delta: 0.03}, mem)
	if cmd.Market != "Mars" || mem.Precise != "Mars" {
		t.Fatalf("/pricewatch side: cmd=%q mem=%+v", cmd.Market, mem)
	}

	bareAlert := alert.ParsedCommand{Cmd: alert.CmdAlert}
	cmd, mem = stickMarket(bareAlert, mem)
	if cmd.Market != "" || mem.Precise != "Mars" {
		t.Fatalf("bare /alert lists: cmd=%q mem=%+v", cmd.Market, mem)
	}
	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdPriceWatch}, mem)
	if cmd.Market != "" || mem.Precise != "Mars" {
		t.Fatalf("bare /pricewatch lists: cmd=%q mem=%+v", cmd.Market, mem)
	}
	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdUnalert}, mem)
	if cmd.Market != "" || mem.Precise != "Mars" {
		t.Fatalf("bare /unalert: cmd=%q mem=%+v", cmd.Market, mem)
	}
	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdLastTrades}, mem)
	if cmd.Market != "" || mem.Precise != "Mars" {
		t.Fatalf("bare /lasttrades: cmd=%q mem=%+v", cmd.Market, mem)
	}
	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdHelp}, mem)
	if mem.Precise != "Mars" || mem.Search != "Mars" {
		t.Fatalf("/help cleared memory: %+v", mem)
	}
	cmd, _ = stickMarket(alert.ParsedCommand{Cmd: alert.CmdHolders}, marketMemory{})
	if cmd.Market != "" {
		t.Fatalf("no history: %q", cmd.Market)
	}
	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdHolders, Market: "Venus"}, marketMemory{Precise: "Mars", Search: "Mars"})
	if cmd.Market != "Venus" || mem.Precise != "Venus" || mem.Search != "Venus" {
		t.Fatalf("named /holders: cmd=%q mem=%+v", cmd.Market, mem)
	}
}

func TestStickMarketCrossVenueUsesTypedWords(t *testing.T) {
	url := "https://polymarket.com/event/south-carolina-senate-2026/will-the-democrats-win-the-south-carolina-senate-race-in-2026"
	mem := marketMemory{Precise: url, Search: "South Carolina Senate", Cmd: "ob"}

	cmd, mem := stickMarket(alert.ParsedCommand{Cmd: alert.CmdOBP}, mem)
	if cmd.Market != "South Carolina Senate" || mem.Precise != url || mem.Search != "South Carolina Senate" {
		t.Fatalf("/obp after /ob: cmd=%q mem=%+v", cmd.Market, mem)
	}
	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdOBK}, mem)
	if cmd.Market != "South Carolina Senate" {
		t.Fatalf("/obk after /ob: %q", cmd.Market)
	}
	cmd, mem = stickMarket(alert.ParsedCommand{Cmd: alert.CmdPos}, mem)
	if cmd.Market != url {
		t.Fatalf("same-venue /pos should keep the polymarket url: %q", cmd.Market)
	}

	mem = marketMemory{Precise: "SC_SENATE_2026.DEM", Search: "South Carolina Senate", Cmd: "obp"}
	cmd, _ = stickMarket(alert.ParsedCommand{Cmd: alert.CmdOB}, mem)
	if cmd.Market != "South Carolina Senate" {
		t.Fatalf("/ob after /obp: %q", cmd.Market)
	}
	cmd, _ = stickMarket(alert.ParsedCommand{Cmd: alert.CmdOBP}, mem)
	if cmd.Market != "SC_SENATE_2026.DEM" {
		t.Fatalf("same-venue /obp should keep the symbol: %q", cmd.Market)
	}
}

func TestApplyMarketStickPersistsLastNamedMarket(t *testing.T) {
	b := &bot{state: &alert.State{LastOBQuery: "OldBook"}}
	cmd, note := b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdHolders}, "")
	if note != "" || cmd.Market != "OldBook" || b.state.LastMarketQuery != "OldBook" {
		t.Fatalf("legacy ob memory %+v cmd %+v note %q", b.state, cmd, note)
	}
	cmd, note = b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdOB, Market: "Aliens"}, "")
	if note != "" || cmd.Market != "Aliens" || b.state.LastMarketQuery != "Aliens" || b.state.LastOBQuery != "Aliens" || b.state.LastSearchQuery != "Aliens" {
		t.Fatalf("state %+v cmd %+v", b.state, cmd)
	}
	cmd, note = b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdPos}, "")
	if note != "" || cmd.Market != "Aliens" || b.state.LastMarketQuery != "Aliens" {
		t.Fatalf("inherit %+v cmd %+v", b.state, cmd)
	}
}

func TestApplyMarketStickKeepsTypedWordsAcrossVenues(t *testing.T) {
	b := &bot{state: &alert.State{}}
	cmd, note := b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdOB, Market: "South Carolina Senate"}, "")
	if note != "" || cmd.Market != "South Carolina Senate" || b.state.LastSearchQuery != "South Carolina Senate" {
		t.Fatalf("named %+v note %q state %+v", cmd, note, b.state)
	}
	url := "https://polymarket.com/event/south-carolina-senate-2026/will-the-democrats-win-the-south-carolina-senate-race-in-2026"
	b.state.RememberSent(9, "ob", url)
	if b.state.LastSearchQuery != "South Carolina Senate" || b.state.LastMarketQuery != url {
		t.Fatalf("after /ob reply %+v", b.state)
	}
	cmd, note = b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdOBP}, "")
	if note != "" || cmd.Market != "South Carolina Senate" {
		t.Fatalf("/obp cmd=%q note %q", cmd.Market, note)
	}
	b.state.RememberSent(10, "obp", "SC_SENATE_2026.DEM")
	cmd, note = b.applyMarketStick(alert.ParsedCommand{Cmd: alert.CmdOB}, "")
	if note != "" || cmd.Market != "South Carolina Senate" {
		t.Fatalf("/ob after pascal cmd=%q note %q state %+v", cmd.Market, note, b.state)
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
