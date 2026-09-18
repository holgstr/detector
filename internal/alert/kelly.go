package alert

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// KellyUsage is the /kelly argument hint.
const KellyUsage = "Usage: /kelly <price> <fv> — e.g. /kelly 42 55 or /kelly 0.42 0.55"

// KellyResult is bankroll fractions for a binary contract at price vs fair value.
type KellyResult struct {
	Price  float64
	FV     float64
	Side   string // YES or NO
	Full   float64
	Half   float64
	Third  float64
	Fourth float64
	Err    string
}

// ComputeKelly is the binary Kelly fraction of bankroll to spend on the
// underpriced side: (fv-price)/(1-price) for YES, (price-fv)/price for NO.
func ComputeKelly(price, fv float64) KellyResult {
	r := KellyResult{Price: price, FV: fv}
	if price <= 0 || price >= 1 || fv <= 0 || fv >= 1 {
		r.Err = "Price and FV must be between 0 and 1 (or 0–100 cents)."
		return r
	}
	if fv == price {
		r.Side = "NONE"
		return r
	}
	var f float64
	if fv > price {
		r.Side = "YES"
		f = (fv - price) / (1 - price)
	} else {
		r.Side = "NO"
		f = (price - fv) / price
	}
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	r.Full = f
	r.Half = f / 2
	r.Third = f / 3
	r.Fourth = f / 4
	return r
}

// KellyText is the Telegram body for /kelly.
func KellyText(price, fv float64) string {
	r := ComputeKelly(price, fv)
	if r.Err != "" {
		return r.Err + "\n" + KellyUsage
	}
	if r.Side == "NONE" || r.Full == 0 {
		return fmt.Sprintf("No edge at %s (FV %s).", formatTickPrice(r.Price, 0.01), formatTickPrice(r.FV, 0.01))
	}
	return fmt.Sprintf(
		"Kelly · buy %s @ %s  FV %s\nFull  %s\nHalf  %s\n1/3   %s\n1/4   %s",
		r.Side,
		formatTickPrice(r.Price, 0.01),
		formatTickPrice(r.FV, 0.01),
		formatKellyPct(r.Full),
		formatKellyPct(r.Half),
		formatKellyPct(r.Third),
		formatKellyPct(r.Fourth),
	)
}

func formatKellyPct(f float64) string {
	p := f * 100
	if p < 0 {
		p = 0
	}
	if p >= 10 || math.Abs(p-math.Round(p)) < 0.05 {
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.2f%%", p)
}

func parseKellyArgs(rest string) (price, fv float64, ok bool) {
	fields := strings.Fields(rest)
	if len(fields) != 2 {
		return 0, 0, false
	}
	price, ok1 := parseProb(fields[0])
	fv, ok2 := parseProb(fields[1])
	return price, fv, ok1 && ok2
}

func parseProb(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSuffix(s, "$")
	lower := strings.ToLower(s)
	pct := strings.HasSuffix(lower, "%")
	if pct {
		s = strings.TrimSpace(s[:len(s)-1])
	}
	s = strings.TrimSuffix(s, "¢")
	s = strings.TrimSpace(s)
	if strings.HasSuffix(strings.ToLower(s), "c") {
		s = strings.TrimSpace(s[:len(s)-1])
	}
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	if pct || v > 1 {
		v = v / 100
	}
	if v < 0 || v > 1 {
		return 0, false
	}
	return v, true
}
