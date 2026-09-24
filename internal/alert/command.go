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
	CmdHolders
	CmdPort
	CmdLastTrades
	CmdUpdate
	CmdTracked
	CmdAdd
	CmdUnadd
	CmdKelly
	CmdOB
	CmdOBP
	CmdOBK
	CmdAlert
	CmdUnalert
	CmdPriceWatch
	CmdUnpriceWatch
	CmdCancel
)

// ParsedCommand is a recognized chat line.
type ParsedCommand struct {
	Cmd     Command
	MinUSD  float64
	Window  time.Duration
	Trader  string
	Market  string
	Query   string
	Price   float64
	FV      float64
	MinSize float64
	Outcome string
	Delta   float64
	Limit   int
}

// ParseCommand understands /start, /help, /minsize [amount], /net [Nh|trader], /pos [market], /holders [market], /port [N] [trader], /lasttrades [trader] [market] [Nh], /tracked, /add, /unadd, /kelly, /ob, /obp, /obk, /alert, /unalert, /pricewatch, /unpricewatch, /cancel, and /update.
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
	case "holders", "holder":
		return ParsedCommand{Cmd: CmdHolders, Market: strings.TrimSpace(rest)}
	case "port", "portfolio":
		limit, trader := parsePortArgs(rest)
		return ParsedCommand{Cmd: CmdPort, Trader: trader, Limit: limit}
	case "lasttrades", "lasttrade", "last-trades", "last_trades", "trades":
		w, trader, market, ok := parseLastTradesArgs(rest)
		if !ok {
			return ParsedCommand{Cmd: CmdHelp}
		}
		return ParsedCommand{Cmd: CmdLastTrades, Window: w, Trader: trader, Market: market}
	case "update", "upgrade", "pull", "deploy":
		return ParsedCommand{Cmd: CmdUpdate}
	case "tracked", "tracking", "wallets":
		return ParsedCommand{Cmd: CmdTracked}
	case "add", "track":
		return ParsedCommand{Cmd: CmdAdd, Query: strings.TrimSpace(rest)}
	case "unadd", "untrack", "remove":
		return ParsedCommand{Cmd: CmdUnadd, Query: strings.TrimSpace(rest)}
	case "kelly", "k":
		if strings.TrimSpace(rest) == "" {
			return ParsedCommand{Cmd: CmdKelly}
		}
		price, fv, ok := parseKellyArgs(rest)
		if !ok {
			return ParsedCommand{Cmd: CmdHelp}
		}
		return ParsedCommand{Cmd: CmdKelly, Price: price, FV: fv}
	case "ob", "orderbook", "order-book", "order_book", "book":
		return ParsedCommand{Cmd: CmdOB, Market: strings.TrimSpace(rest)}
	case "obp", "pascal", "pascalbook", "pascal-book":
		return ParsedCommand{Cmd: CmdOBP, Market: strings.TrimSpace(rest)}
	case "obk":
		return ParsedCommand{Cmd: CmdOBK, Market: strings.TrimSpace(rest)}
	case "alert", "pricealert", "price-alert", "askalert", "ask-alert", "bidalert", "bid-alert":
		return parseAlertCommand(rest)
	case "unalert", "un-alert", "delalert", "stopalert":
		return ParsedCommand{Cmd: CmdUnalert, Market: strings.TrimSpace(rest)}
	case "pricewatch", "price-watch", "price_watch":
		return parsePriceWatchCommand(rest)
	case "unpricewatch", "un-pricewatch", "un_pricewatch", "stoppricewatch":
		return ParsedCommand{Cmd: CmdUnpriceWatch, Market: strings.TrimSpace(rest)}
	case "cancel":
		return ParsedCommand{Cmd: CmdCancel}
	default:
		return ParsedCommand{}
	}
}

func parseAlertCommand(rest string) ParsedCommand {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ParsedCommand{Cmd: CmdAlert}
	}
	fields := strings.Fields(rest)
	if len(fields) >= 2 {
		price, minSize, ok := ParseAlertConfirm(strings.Join(fields[len(fields)-2:], " "))
		if ok {
			market := strings.TrimSpace(strings.Join(fields[:len(fields)-2], " "))
			if market != "" {
				return ParsedCommand{Cmd: CmdAlert, Market: market, Price: price, MinSize: minSize}
			}
		}
	}
	return ParsedCommand{Cmd: CmdAlert, Market: rest}
}

func parsePriceWatchCommand(rest string) ParsedCommand {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ParsedCommand{Cmd: CmdPriceWatch}
	}
	market, outcome, delta, ok := parsePriceWatchArgs(rest)
	if !ok {
		return ParsedCommand{Cmd: CmdHelp}
	}
	return ParsedCommand{Cmd: CmdPriceWatch, Market: market, Outcome: outcome, Delta: delta}
}

// parsePriceWatchArgs reads "<market> YES|NO <cents>" or "<market> <cents> YES|NO".
// Delta is returned in probability units (3 cents → 0.03).
func parsePriceWatchArgs(rest string) (market, outcome string, delta float64, ok bool) {
	fields := strings.Fields(rest)
	if len(fields) < 3 {
		return "", "", 0, false
	}
	last := fields[len(fields)-1]
	prev := fields[len(fields)-2]
	market = strings.TrimSpace(strings.Join(fields[:len(fields)-2], " "))
	if market == "" {
		return "", "", 0, false
	}
	if d, okd := parseDeltaCents(last); okd {
		if side, oks := parseSide(prev); oks {
			return market, side, d, true
		}
	}
	if d, okd := parseDeltaCents(prev); okd {
		if side, oks := parseSide(last); oks {
			return market, side, d, true
		}
	}
	return "", "", 0, false
}

func parseSide(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes":
		return "Yes", true
	case "no":
		return "No", true
	default:
		return "", false
	}
}

func parseDeltaCents(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "¢")
	s = strings.TrimSuffix(s, "c")
	s = strings.TrimSuffix(s, "C")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 || v >= 100 {
		return 0, false
	}
	return v / 100, true
}

func parseLastTradesArgs(rest string) (time.Duration, string, string, bool) {
	window := defaultNetWindow
	var parts []string
	sawWindow := false
	for _, tok := range strings.Fields(rest) {
		if d, ok := parseWindowToken(tok); ok {
			if sawWindow {
				return 0, "", "", false
			}
			window = clampNetWindow(d)
			sawWindow = true
			continue
		}
		parts = append(parts, tok)
	}
	trader, market := splitTraderMarket(parts)
	return window, trader, market, true
}

// splitTraderMarket takes the first token as a trader candidate (resolved later
// against tracked names, then Polymarket). "all" means every tracked wallet.
func splitTraderMarket(parts []string) (trader, market string) {
	if len(parts) == 0 {
		return "", ""
	}
	if strings.EqualFold(parts[0], "all") {
		return "", strings.Join(parts[1:], " ")
	}
	return parts[0], strings.Join(parts[1:], " ")
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

// parsePortArgs reads an optional leading count (/port 5 Flip) and the trader name.
// The count is how many top holdings to keep after the usual market-value sort.
func parsePortArgs(rest string) (limit int, trader string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return 0, ""
	}
	fields := strings.Fields(rest)
	if n, ok := parsePositiveInt(fields[0]); ok {
		return n, strings.TrimSpace(strings.Join(fields[1:], " "))
	}
	return 0, rest
}

func parsePositiveInt(s string) (int, bool) {
	if s == "" || len(s) > 6 {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
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
	return fmt.Sprintf("Commands:\n/minsize — show min size (now %s)\n/minsize 100 — hide fills under $100 after aggregating same-market same-direction trades\n/net — net share changes in the last 24h with effective avg price (flat markets omitted)\n/net 6h Flip — same as /net Flip 6h (short names match)\n/pos <market> — tracked holdings (words, slug, or URL; ambiguous names prefer large tracked nets)\n/holders <market> — top 10 holders on each side, netted when a wallet holds both (words, slug, or URL)\n/port <trader> — that trader's open non-sports nets of $100+, shares sorted by market value (any Polymarket name)\n/port 5 <trader> — same list, only the top 5 by market value\n/lasttrades — fills in the last 24h (sports excluded; trader, market, and window are optional)\n/lasttrades Flip Andersson 6h — one trader in one market; names resolve even if untracked; omit the trader to use all tracked wallets\n/kelly <price> <fv> — full / half / 1/3 / 1/4 Kelly %% of bankroll (cents or 0–1)\n/ob <market> — Yes CLOB ticks (4 each side) with size (words pick tracked-heavy or high-volume markets)\n/obp <market> — Pascal book (4 closest ticks each side; an event name shows every outcome)\n/obk <market> — Kalshi book (4 closest Yes ticks each side; an event name shows every outcome)\nAfter one of those names a market, the other two with no market reuse that name (so /ob Aliens then /obk is /obk Aliens)\n/alert <market> — watch Yes asks to take; then reply with price and min size (or /alert <market> 32 1000)\n/unalert <market> — stop a price alert\n/pricewatch <market> <YES|NO> <cents> — ping when that side's midpoint moves by N cents, then re-anchor\n/unpricewatch <market> — stop a price watch\n/tracked — names of wallets being watched\n/add <wallet or name> — start watching (name looks up the current wallet id)\n/unadd <wallet or name> — stop watching that wallet id\n/update — pull origin/main from GitHub, rebuild, and restart\n/help", formatUSD(minUSD))
}

// MinSizeStatus is the reply after /minsize or a change.
func MinSizeStatus(minUSD float64) string {
	if minUSD <= 0 {
		return "Min size is off."
	}
	return fmt.Sprintf("Min size is %s.", formatUSD(minUSD))
}
