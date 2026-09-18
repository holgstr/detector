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

// OBReport is /ob output: inside depth (Yes only when the market is Yes/No).
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
// Yes/No books are symmetric, so only Yes is shown and the outcome is omitted.
func FormatOBReport(r OBReport) string {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = r.Slug
	}
	if title == "" {
		title = strings.TrimSpace(r.Query)
	}
	var b strings.Builder
	b.WriteString(title)
	books := booksToShow(r.Books)
	b.WriteByte('\n')
	if len(books) == 0 {
		b.WriteString("\nNo CLOB depth.")
		return b.String()
	}
	nameOutcomes := !(len(books) == 1 && isYesOutcome(books[0].Outcome))
	for _, book := range books {
		if nameOutcomes {
			b.WriteByte('\n')
			b.WriteString(strings.ToUpper(strings.TrimSpace(book.Outcome)))
		}
		b.WriteString(formatBookLadder(book))
	}
	return b.String()
}

func booksToShow(books []polymarket.OutcomeBook) []polymarket.OutcomeBook {
	for _, book := range books {
		if isYesOutcome(book.Outcome) {
			return []polymarket.OutcomeBook{book}
		}
	}
	return books
}

func isYesOutcome(outcome string) bool {
	return strings.EqualFold(strings.TrimSpace(outcome), "yes")
}

func formatBookLadder(book polymarket.OutcomeBook) string {
	asks := levelsWithSize(book.Asks)
	for i, j := 0, len(asks)-1; i < j; i, j = i+1, j-1 {
		asks[i], asks[j] = asks[j], asks[i]
	}
	bids := levelsWithSize(book.Bids)
	rows := make([][2]string, 0, len(asks)+len(bids))
	appendLevels := func(levels []polymarket.BookLevel) {
		for _, lv := range levels {
			rows = append(rows, [2]string{formatTickPrice(lv.Price, book.Tick), formatShares(lv.Size)})
		}
	}
	appendLevels(asks)
	appendLevels(bids)
	priceW, sizeW := 0, 0
	for _, row := range rows {
		if n := len([]rune(row[0])); n > priceW {
			priceW = n
		}
		if n := len([]rune(row[1])); n > sizeW {
			sizeW = n
		}
	}

	var b strings.Builder
	writeLevels := func(levels []polymarket.BookLevel) {
		for _, lv := range levels {
			b.WriteByte('\n')
			b.WriteString(padRunes(formatTickPrice(lv.Price, book.Tick), priceW))
			b.WriteString("  ")
			b.WriteString(padRunes(formatShares(lv.Size), sizeW))
		}
	}
	writeLevels(asks)
	if len(asks) > 0 && len(bids) > 0 {
		b.WriteString("\n- - -")
	}
	writeLevels(bids)
	if len(asks) == 0 && len(bids) == 0 {
		b.WriteString("\nNo CLOB depth.")
	}
	return b.String()
}

func levelsWithSize(levels []polymarket.BookLevel) []polymarket.BookLevel {
	out := make([]polymarket.BookLevel, 0, len(levels))
	for _, lv := range levels {
		if lv.Size > 0 {
			out = append(out, lv)
		}
	}
	return out
}

func padRunes(s string, width int) string {
	n := len([]rune(s))
	if n >= width {
		return s
	}
	return strings.Repeat(" ", width-n) + s
}

func formatTickPrice(p, tick float64) string {
	cents := p * 100
	if tick > 0 && tick < 0.005 {
		return fmt.Sprintf("%.1f¢", cents)
	}
	return fmt.Sprintf("%.0f¢", cents)
}
