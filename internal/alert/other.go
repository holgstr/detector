package alert

import (
	"strings"

	"github.com/holgstr/detector/internal/polymarket"
)

// MarketCommandName is the stored name for a command /other can repeat.
func MarketCommandName(c Command) (string, bool) {
	switch c {
	case CmdPos:
		return "pos", true
	case CmdHolders:
		return "holders", true
	case CmdOB:
		return "ob", true
	case CmdOBP:
		return "obp", true
	case CmdOBK:
		return "obk", true
	default:
		return "", false
	}
}

// ParseMarketCommand reads a stored /other command name.
func ParseMarketCommand(name string) (Command, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "pos", "position", "positions", "holdings":
		return CmdPos, true
	case "holders", "holder":
		return CmdHolders, true
	case "ob", "orderbook", "book":
		return CmdOB, true
	case "obp":
		return CmdOBP, true
	case "obk":
		return CmdOBK, true
	default:
		return 0, false
	}
}

// OtherSource decides which command and market /other should switch from.
// A reply to a message this bot sent uses that message's command. Otherwise
// a reply that names one market uses that market, and a bare /other uses the
// last /pos, /holders, /ob, /obp, or /obk.
func OtherSource(replyID int64, replyText string, sent []SentMarket, lastCmd, lastMarket string) (Command, string, string) {
	if row, ok := sentByID(sent, replyID); ok {
		cmd, cmdOK := ParseMarketCommand(row.Cmd)
		market := strings.TrimSpace(row.Market)
		if !cmdOK || market == "" {
			return 0, "", "That message isn't a /pos, /holders, or /ob."
		}
		return cmd, market, ""
	}

	replyText = strings.TrimSpace(replyText)
	hasReply := replyID != 0 || replyText != ""
	if !hasReply {
		return lastOther(lastCmd, lastMarket)
	}

	market, n := MarketFromReply(replyText)
	if n > 1 {
		return 0, "", "That message names more than one market."
	}
	if n != 1 {
		return 0, "", "That message doesn't name one market."
	}
	cmd, ok := commandFromMarketText(replyText, lastCmd)
	if !ok {
		if c, cmdOK := ParseMarketCommand(lastCmd); cmdOK {
			cmd = c
		} else {
			cmd = CmdOB
		}
	}
	return cmd, market, ""
}

func lastOther(lastCmd, lastMarket string) (Command, string, string) {
	cmd, ok := ParseMarketCommand(lastCmd)
	market := strings.TrimSpace(lastMarket)
	if !ok || market == "" {
		return 0, "", "Reply to a /pos, /holders, or /ob, or run one of those first."
	}
	return cmd, market, ""
}

func sentByID(sent []SentMarket, id int64) (SentMarket, bool) {
	if id == 0 {
		return SentMarket{}, false
	}
	for i := len(sent) - 1; i >= 0; i-- {
		if sent[i].MessageID == id {
			return sent[i], true
		}
	}
	return SentMarket{}, false
}

// commandFromMarketText recognizes /ob, /pos, and /holders bodies.
// Holdings lists look the same for /pos and /holders, so a previous /holders
// stays /holders and anything else is /pos. An order book stays /obp or /obk
// when that was the previous command.
func commandFromMarketText(text, lastCmd string) (Command, bool) {
	var sawLadder, sawPosEmpty, sawHoldersEmpty, sawSide bool
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if isLadderLine(t) || strings.HasPrefix(t, "Ask alert") {
			sawLadder = true
		}
		if strings.HasPrefix(t, "No tracked holdings") {
			sawPosEmpty = true
		}
		if strings.HasPrefix(t, "No holders") {
			sawHoldersEmpty = true
		}
		if isSideHeader(t) {
			sawSide = true
		}
	}
	if sawLadder {
		if c, ok := ParseMarketCommand(lastCmd); ok && (c == CmdOBP || c == CmdOBK) {
			return c, true
		}
		return CmdOB, true
	}
	if sawHoldersEmpty {
		return CmdHolders, true
	}
	if sawPosEmpty {
		return CmdPos, true
	}
	if sawSide {
		if c, ok := ParseMarketCommand(lastCmd); ok && c == CmdHolders {
			return CmdHolders, true
		}
		return CmdPos, true
	}
	return 0, false
}

// PickOtherOutcome is the other live outcome with the highest Yes price.
// The current market (same condition id or slug) is skipped. Equal prices
// prefer the market with more 24h volume.
func PickOtherOutcome(current polymarket.SearchMarket, markets []polymarket.EventMarket) (polymarket.EventMarket, bool) {
	curID := strings.ToLower(strings.TrimSpace(current.Market.ConditionID))
	curSlug := strings.ToLower(strings.TrimSpace(current.Market.Slug))
	var best polymarket.EventMarket
	found := false
	for _, m := range markets {
		id := strings.ToLower(strings.TrimSpace(m.Market.ConditionID))
		slug := strings.ToLower(strings.TrimSpace(m.Market.Slug))
		if id == "" {
			continue
		}
		if (curID != "" && id == curID) || (curSlug != "" && slug == curSlug) {
			continue
		}
		if m.YesPrice <= 0 {
			continue
		}
		if !found || betterOutcome(m, best) {
			best = m
			found = true
		}
	}
	return best, found
}

func betterOutcome(a, b polymarket.EventMarket) bool {
	if a.YesPrice != b.YesPrice {
		return a.YesPrice > b.YesPrice
	}
	if a.Volume24hr != b.Volume24hr {
		return a.Volume24hr > b.Volume24hr
	}
	if a.Volume != b.Volume {
		return a.Volume > b.Volume
	}
	return a.Market.Question < b.Market.Question
}

// OtherQuery is the market text to pass into the repeated command.
// Polymarket commands get a URL or slug. /obp and /obk get the outcome name.
func OtherQuery(cmd Command, m polymarket.EventMarket) string {
	switch cmd {
	case CmdOBP, CmdOBK:
		if t := strings.TrimSpace(m.GroupItemTitle); t != "" {
			return t
		}
		if t := strings.TrimSpace(m.Market.Question); t != "" {
			return t
		}
	}
	if u := strings.TrimSpace(m.Market.URL); u != "" {
		return u
	}
	if s := strings.TrimSpace(m.Market.Slug); s != "" {
		return s
	}
	if t := strings.TrimSpace(m.GroupItemTitle); t != "" {
		return t
	}
	return strings.TrimSpace(m.Market.Question)
}

// OtherTrackRef is the Polymarket identity stored so the next /other starts
// from this outcome.
func OtherTrackRef(m polymarket.EventMarket) string {
	if u := strings.TrimSpace(m.Market.URL); u != "" {
		return u
	}
	if s := strings.TrimSpace(m.Market.Slug); s != "" {
		return s
	}
	return strings.TrimSpace(m.Market.Question)
}
