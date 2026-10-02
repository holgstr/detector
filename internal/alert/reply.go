package alert

import (
	"regexp"
	"strings"
)

var polyMarketURL = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?polymarket\.com/(?:event|market)/[^\s<>)]+`)

// MarketFromReply reports how many distinct markets a replied-to message names.
// When n is 1, query is that market: a title, or a Polymarket URL when one is present.
// n is 0 when the text has no market, and greater than 1 when it names several.
func MarketFromReply(text string) (query string, n int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", 0
	}
	if urls := polymarketURLs(text); len(urls) > 0 {
		return oneOf(urls)
	}
	if found := marketsFromLines(text); len(found) > 0 {
		return oneOf(found)
	}
	if title, ok := headerMarket(text); ok {
		return title, 1
	}
	if title, ok := singleLineMarket(text); ok {
		return title, 1
	}
	return "", 0
}

func oneOf(found []string) (string, int) {
	if len(found) == 1 {
		return found[0], 1
	}
	return "", len(found)
}

func polymarketURLs(text string) []string {
	raw := polyMarketURL.FindAllString(text, -1)
	var out []string
	for _, u := range raw {
		u = strings.TrimRight(u, ".,;")
		if !strings.Contains(u, "://") {
			u = "https://" + u
		}
		out = appendUnique(out, u)
	}
	return out
}

func marketsFromLines(text string) []string {
	lines := strings.Split(text, "\n")
	var found []string
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if isFillLine(line) {
			if title, j, ok := titleAfterFill(lines, i); ok {
				found = appendUnique(found, title)
				i = j
			}
			continue
		}
		if title, ok := netOrPortTitle(line); ok {
			found = appendUnique(found, title)
			continue
		}
		if title, ok := listMarket(line); ok {
			found = appendUnique(found, title)
		}
	}
	return found
}

func titleAfterFill(lines []string, i int) (string, int, bool) {
	for j := i + 1; j < len(lines); j++ {
		line := strings.TrimSpace(lines[j])
		if line == "" || isNote(line) {
			continue
		}
		if isMarketTitleLine(line) {
			return line, j, true
		}
		return "", i, false
	}
	return "", i, false
}

func headerMarket(text string) (string, bool) {
	var head string
	var body []string
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || isNote(t) {
			continue
		}
		if head == "" {
			head = t
			continue
		}
		body = append(body, t)
	}
	if head == "" || len(body) == 0 || isFillLine(head) || isStatus(head) {
		return "", false
	}
	if title, ok := splitTrailingSide(head); ok && watchBody(body) {
		return title, true
	}
	if bookOrPosBody(body) {
		return head, true
	}
	if allFills(body) {
		if title, ok := splitHeadMarket(head); ok {
			return title, true
		}
		return head, true
	}
	return "", false
}

func splitHeadMarket(head string) (string, bool) {
	const sep = " · "
	i := strings.LastIndex(head, sep)
	if i < 0 {
		return "", false
	}
	title := strings.TrimSpace(head[i+len(sep):])
	if title == "" || isNote(title) {
		return "", false
	}
	return title, true
}

func splitTrailingSide(head string) (string, bool) {
	fields := strings.Fields(head)
	if len(fields) < 2 {
		return "", false
	}
	last := fields[len(fields)-1]
	if !strings.EqualFold(last, "YES") && !strings.EqualFold(last, "NO") {
		return "", false
	}
	title := strings.TrimSpace(strings.Join(fields[:len(fields)-1], " "))
	if title == "" {
		return "", false
	}
	return title, true
}

func singleLineMarket(text string) (string, bool) {
	if strings.Contains(text, "\n") {
		return "", false
	}
	line := strings.TrimSpace(text)
	if len(line) < 3 || strings.HasPrefix(line, "/") || isStatus(line) || isFillLine(line) || isLadderLine(line) || isNote(line) {
		return "", false
	}
	if _, ok := netOrPortTitle(line); ok {
		return "", false
	}
	return line, true
}

func bookOrPosBody(lines []string) bool {
	for _, ln := range lines {
		if isLadderLine(ln) || isSideHeader(ln) || strings.HasPrefix(ln, "Yes ≤") || strings.HasPrefix(ln, "Yes <=") || strings.HasPrefix(ln, "Ask alert") {
			return true
		}
		if strings.HasPrefix(ln, "No tracked holdings") || strings.HasPrefix(ln, "No holders") || ln == "No CLOB depth." || ln == "No orders." {
			return true
		}
	}
	return false
}

func watchBody(lines []string) bool {
	for _, ln := range lines {
		if strings.Contains(ln, "·") && strings.Contains(ln, " or ") {
			return true
		}
	}
	return false
}

func allFills(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	for _, ln := range lines {
		if !isFillLine(ln) {
			return false
		}
	}
	return true
}

func isFillLine(line string) bool {
	fields := strings.Fields(line)
	at := -1
	for i, f := range fields {
		if f == "·" {
			break
		}
		if f == "@" {
			at = i
			break
		}
	}
	if at < 3 {
		return false
	}
	switch strings.ToUpper(fields[at-3]) {
	case "BUY", "SELL", "TRADE":
		return true
	default:
		return false
	}
}

func isMarketTitleLine(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || isFillLine(s) || isPositionLine(s) || isLadderLine(s) || isSideHeader(s) || isNote(s) || isStatus(s) {
		return false
	}
	if _, ok := netOrPortTitle(s); ok {
		return false
	}
	if _, ok := listMarket(s); ok {
		return false
	}
	return true
}

func isPositionLine(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "Position:")
}

func isLadderLine(s string) bool {
	s = strings.TrimSpace(s)
	if s == "- - -" || s == "No CLOB depth." || s == "No orders." {
		return true
	}
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return false
	}
	return strings.Contains(fields[0], "¢") && looksLikeSize(fields[1])
}

func isSideHeader(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	side, rest, ok := strings.Cut(s, " @ ")
	if !ok || strings.TrimSpace(rest) == "" {
		return false
	}
	return !strings.Contains(side, " ")
}

func isNote(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")")
}

func isStatus(s string) bool {
	s = strings.TrimSpace(s)
	prefixes := []string{
		"Watching ", "Still here", "Min size", "Commands:", "Usage:", "Couldn't",
		"No net", "No fills", "No price", "No open", "No holders", "No tracked",
		"No CLOB", "No orders", "An update", "Pulling ", "Already on", "Restarting",
		"Update failed", "Restart failed", "That message", "Which alert", "Which watch",
		"Stopped ", "No matching", "No edge", "Price and FV",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func netOrPortTitle(line string) (string, bool) {
	line = strings.TrimSpace(line)
	i := strings.Index(line, "  ")
	if i < 0 {
		return "", false
	}
	left := strings.TrimSpace(line[:i])
	right := strings.TrimSpace(line[i+2:])
	fields := strings.Fields(left)
	if len(fields) != 2 || !looksLikeSize(fields[0]) || fields[1] == "" {
		return "", false
	}
	if cut := strings.Index(right, " @ "); cut >= 0 {
		right = strings.TrimSpace(right[:cut])
	}
	if cut := strings.Index(right, " | "); cut >= 0 {
		right = strings.TrimSpace(right[:cut])
	}
	if right == "" || strings.Contains(right, "  ") {
		return "", false
	}
	return right, true
}

func listMarket(line string) (string, bool) {
	left, right, ok := strings.Cut(line, " — ")
	if !ok {
		return "", false
	}
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" {
		return "", false
	}
	if strings.Contains(right, "or lower") {
		return left, true
	}
	if strings.Contains(right, "±") {
		if title, ok := splitTrailingSide(left); ok {
			return title, true
		}
	}
	return "", false
}

func looksLikeSize(s string) bool {
	s = strings.TrimPrefix(s, "+")
	s = strings.TrimPrefix(s, "-")
	if s == "" {
		return false
	}
	seenDigit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			seenDigit = true
		case r == '.' || r == ',' || r == 'k' || r == 'K' || r == 'M':
		default:
			return false
		}
	}
	return seenDigit
}

func appendUnique(found []string, title string) []string {
	title = strings.TrimSpace(title)
	if title == "" {
		return found
	}
	for _, have := range found {
		if strings.EqualFold(have, title) {
			return found
		}
	}
	return append(found, title)
}
