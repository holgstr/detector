package alert

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Command is an inbound Telegram chat action.
type Command int

const (
	CmdNone Command = iota
	CmdStart
	CmdHelp
	CmdMinSizeShow
	CmdMinSizeSet
	CmdNet
	CmdPos
)

// ParsedCommand is a recognized chat line.
type ParsedCommand struct {
	Cmd    Command
	MinUSD float64
	Window time.Duration
	Trader string
	Market string
}

// ParseCommand understands /start, /help, /minsize [amount], /net [Nh|trader], and /pos [market].
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
	case "net", "delta", "netpos", "exposure":
		w, trader, ok := parseNetArgs(rest)
		if !ok {
			return ParsedCommand{Cmd: CmdHelp}
		}
		return ParsedCommand{Cmd: CmdNet, Window: w, Trader: trader}
	case "pos", "position", "positions", "holdings":
		return ParsedCommand{Cmd: CmdPos, Market: strings.TrimSpace(rest)}
	default:
		return ParsedCommand{}
	}
}

func parseNetArgs(rest string) (time.Duration, string, bool) {
	window := defaultNetWindow
	var nameParts []string
	sawWindow := false
	for _, tok := range strings.Fields(rest) {
		if strings.EqualFold(tok, "all") {
			continue
		}
		if d, ok := parseWindowToken(tok); ok {
			if sawWindow {
				return 0, "", false
			}
			window = clampNetWindow(d)
			sawWindow = true
			continue
		}
		nameParts = append(nameParts, tok)
	}
	return window, strings.Join(nameParts, " "), true
}

func parseWindowToken(s string) (time.Duration, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return 0, false
	}
	if s == "all" {
		return 0, false
	}
	mult := time.Hour
	switch {
	case strings.HasSuffix(s, "hours"):
		s = strings.TrimSuffix(s, "hours")
	case strings.HasSuffix(s, "hour"):
		s = strings.TrimSuffix(s, "hour")
	case strings.HasSuffix(s, "hrs"):
		s = strings.TrimSuffix(s, "hrs")
	case strings.HasSuffix(s, "hr"):
		s = strings.TrimSuffix(s, "hr")
	case strings.HasSuffix(s, "h"):
		s = strings.TrimSuffix(s, "h")
	case strings.HasSuffix(s, "minutes"):
		s = strings.TrimSuffix(s, "minutes")
		mult = time.Minute
	case strings.HasSuffix(s, "minute"):
		s = strings.TrimSuffix(s, "minute")
		mult = time.Minute
	case strings.HasSuffix(s, "mins"):
		s = strings.TrimSuffix(s, "mins")
		mult = time.Minute
	case strings.HasSuffix(s, "min"):
		s = strings.TrimSuffix(s, "min")
		mult = time.Minute
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSuffix(s, "m")
		mult = time.Minute
	case strings.HasSuffix(s, "days"):
		s = strings.TrimSuffix(s, "days")
		mult = 24 * time.Hour
	case strings.HasSuffix(s, "day"):
		s = strings.TrimSuffix(s, "day")
		mult = 24 * time.Hour
	case strings.HasSuffix(s, "d"):
		s = strings.TrimSuffix(s, "d")
		mult = 24 * time.Hour
	default:
		for _, r := range s {
			if !unicode.IsDigit(r) && r != '.' {
				return 0, false
			}
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return time.Duration(v * float64(mult)), true
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
	return fmt.Sprintf("Commands:\n/minsize — show min size (now %s)\n/minsize 100 — hide fills under $100 after aggregating same-market same-direction trades\n/net — net share changes in the last 24h (flat markets omitted)\n/net 6h Flip — same as /net Flip 6h (short names match)\n/pos Andersson — tracked holdings in any market (words, slug, or URL)\n/help", formatUSD(minUSD))
}

// MinSizeStatus is the reply after /minsize or a change.
func MinSizeStatus(minUSD float64) string {
	if minUSD <= 0 {
		return "Min size is off — every non-sports fill is sent. /minsize 100 to match the Activity tab."
	}
	return fmt.Sprintf("Min size is %s. Same-market same-direction fills are added up first, then this floor is applied. /minsize 0 to turn off.", formatUSD(minUSD))
}
