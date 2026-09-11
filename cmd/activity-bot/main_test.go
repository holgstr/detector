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
	if len(got) != 1 || !strings.Contains(got[0], "/net") || !strings.Contains(got[0], "/pos") {
		t.Fatalf("%q", got)
	}
}
