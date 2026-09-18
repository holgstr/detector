package alert

import (
	"context"
	"fmt"
	"strings"

	"github.com/holgstr/detector/internal/polymarket"
)

type obMarketAPI interface {
	FindMarket(ctx context.Context, query string) (polymarket.SearchMarket, error)
	FetchOutcomeBooks(ctx context.Context, conditionID string) ([]polymarket.OutcomeBook, error)
}

// OBReport is /ob output: inside depth for each outcome.
type OBReport struct {
	Query string
	Title string
	Slug  string
	URL   string
	Books []polymarket.OutcomeBook
}

// FetchOBReport resolves a market and loads CLOB depth.
func FetchOBReport(ctx context.Context, api obMarketAPI, query string) (OBReport, error) {
	query = strings.TrimSpace(query)
	hit, err := api.FindMarket(ctx, query)
	if err != nil {
		return OBReport{Query: query}, err
	}
	books, err := api.FetchOutcomeBooks(ctx, hit.Market.ConditionID)
	title := strings.TrimSpace(hit.Market.Question)
	if title == "" {
		title = strings.TrimSpace(hit.GroupItemTitle)
	}
	if title == "" {
		title = hit.Market.Slug
	}
	rep := OBReport{
		Query: query,
		Title: title,
		Slug:  hit.Market.Slug,
		URL:   hit.Market.URL,
		Books: books,
	}
	return rep, err
}

// FormatOBReport is the Telegram body for /ob.
func FormatOBReport(r OBReport) string {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = r.Slug
	}
	if title == "" {
		title = strings.TrimSpace(r.Query)
	}
	var b strings.Builder
	b.WriteString("Order book · ")
	b.WriteString(title)
	if len(r.Books) == 0 {
		b.WriteString("\nNo CLOB depth.")
		return b.String()
	}
	for _, book := range r.Books {
		b.WriteByte('\n')
		b.WriteString(strings.ToUpper(strings.TrimSpace(book.Outcome)))
		b.WriteString("\n  Asks  ")
		b.WriteString(formatBookSide(book.Asks, book.Tick))
		b.WriteString("\n  Bids  ")
		b.WriteString(formatBookSide(book.Bids, book.Tick))
	}
	return b.String()
}

func formatBookSide(levels []polymarket.BookLevel, tick float64) string {
	if len(levels) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(levels))
	for _, lv := range levels {
		size := "—"
		if lv.Size > 0 {
			size = formatShares(lv.Size)
		}
		parts = append(parts, fmt.Sprintf("%s %s", formatTickPrice(lv.Price, tick), size))
	}
	return strings.Join(parts, " · ")
}

func formatTickPrice(p, tick float64) string {
	cents := p * 100
	if tick > 0 && tick < 0.005 {
		return fmt.Sprintf("%.1f¢", cents)
	}
	return fmt.Sprintf("%.0f¢", cents)
}
