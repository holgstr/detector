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
