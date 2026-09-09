package alert

import (
	"fmt"
	"strconv"
	"strings"
)

// Command is an inbound Telegram chat action.
type Command int

const (
	CmdNone Command = iota
	CmdStart
	CmdHelp
	CmdMinSizeShow
	CmdMinSizeSet
)

// ParsedCommand is a recognized chat line.
type ParsedCommand struct {
	Cmd    Command
	MinUSD float64
}

// ParseCommand understands /start, /help, and /minsize [amount] (Activity tab min size).
func ParseCommand(text string) ParsedCommand {
	line := strings.TrimSpace(text)
	if line == "" {
		return ParsedCommand{}
	}
	line = stripBotMention(line)
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ParsedCommand{}
	}
	verb := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	rest := ""
	if len(fields) > 1 {
		rest = strings.Join(fields[1:], " ")
	}

	switch verb {
	case "start":
		return ParsedCommand{Cmd: CmdStart}
	case "help":
		return ParsedCommand{Cmd: CmdHelp}
	case "minsize", "min", "minusd", "min-usd", "min_usd":
		if strings.TrimSpace(rest) == "" {
			return ParsedCommand{Cmd: CmdMinSizeShow}
		}
		v, ok := parseUSDAmount(rest)
		if !ok {
			return ParsedCommand{Cmd: CmdHelp}
		}
		if v < 0 {
			v = 0
		}
		return ParsedCommand{Cmd: CmdMinSizeSet, MinUSD: v}
	default:
		return ParsedCommand{}
	}
}

func stripBotMention(text string) string {
	if !strings.HasPrefix(text, "/") {
		return text
	}
	cmd, rest, found := strings.Cut(text, " ")
	at := strings.IndexByte(cmd, '@')
	if at <= 1 {
		return text
	}
	cmd = cmd[:at]
	if found {
		return cmd + " " + rest
	}
	return cmd
}

func parseUSDAmount(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSuffix(s, "$")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// HelpText lists chat commands.
func HelpText(minUSD float64) string {
	return fmt.Sprintf("Commands:\n/minsize — show min size (now %s)\n/minsize 100 — hide fills under $100 after aggregating same-market same-direction trades\n/help", formatUSD(minUSD))
}

// MinSizeStatus is the reply after /minsize or a change.
func MinSizeStatus(minUSD float64) string {
	if minUSD <= 0 {
		return "Min size is off — every non-sports fill is sent. /minsize 100 to match the Activity tab."
	}
	return fmt.Sprintf("Min size is %s. Same-market same-direction fills are added up first, then this floor is applied. /minsize 0 to turn off.", formatUSD(minUSD))
}
