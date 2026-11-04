package alert

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/holgstr/detector/internal/sharps"
)

// CashUsage is the reply when /cash has no trader (or "all").
const CashUsage = "Usage: /cash <trader> — pUSD cash on that Polymarket profile."

// ResolveCashWallet requires exactly one trader (tracked or a Polymarket name/wallet).
func ResolveCashWallet(ctx context.Context, api userLookup, query string) (sharps.Wallet, string) {
	q := strings.TrimSpace(query)
	if q == "" || strings.EqualFold(q, "all") {
		return sharps.Wallet{}, CashUsage
	}
	return ResolveAnyTrader(ctx, api, q)
}

// FormatCash is the Telegram body for /cash.
func FormatCash(name string, usd float64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Wallet"
	}
	return name + "\nCash " + formatCashUSD(usd)
}

func formatCashUSD(n float64) string {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return "—"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	cents := int64(math.Round(n * 100))
	whole := cents / 100
	frac := cents % 100
	digits := strconv.FormatInt(whole, 10)
	var b strings.Builder
	for i := 0; i < len(digits); i++ {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(digits[i])
	}
	return fmt.Sprintf("%s$%s.%02d", sign, b.String(), frac)
}
