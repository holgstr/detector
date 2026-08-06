package holdertabs

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

const DefaultMinLifetimePnL = 100_000.0

// SideStats is the qualified-position aggregate for one outcome.
type SideStats struct {
	Outcome         string  `json:"outcome"`
	QualifiedCount  int     `json:"qualified_count"`
	QualifiedSize   float64 `json:"qualified_size"`
	Strength        float64 `json:"strength"` // two-way share in [0,1]
	StrengthPercent float64 `json:"strength_percent"`
}

// Strength is the holder-tabs strength snapshot for a market.
// For each side, qualified_size sums position size of holders with
// lifetime_pnl > MinLifetimePnL. Strength is the two-way share of
// those aggregates (yes_size / (yes_size+no_size), and likewise for no).
type Strength struct {
	ComputedAt     string             `json:"computed_at"`
	MinLifetimePnL float64            `json:"min_lifetime_pnl"`
	Market         polymarket.Market  `json:"market"`
	SourceFetched  string             `json:"source_fetched_at,omitempty"`
	Yes            SideStats          `json:"yes"`
	No             SideStats          `json:"no"`
	Stronger       string             `json:"stronger"` // "yes", "no", or "tie"
	TotalQualified float64            `json:"total_qualified_size"`
}

// ComputeStrength builds strength tabs from a holders intermediate result.
func ComputeStrength(r *polymarket.Result, minPnL float64) Strength {
	if minPnL <= 0 {
		minPnL = DefaultMinLifetimePnL
	}

	yesSize, yesCount := sumQualified(r.Yes.Holders, minPnL)
	noSize, noCount := sumQualified(r.No.Holders, minPnL)
	total := yesSize + noSize

	yesStrength, noStrength := 0.0, 0.0
	if total > 0 {
		yesStrength = yesSize / total
		noStrength = noSize / total
	}

	yesOutcome, noOutcome := r.Yes.Outcome, r.No.Outcome
	if yesOutcome == "" {
		yesOutcome = "Yes"
	}
	if noOutcome == "" {
		noOutcome = "No"
	}

	stronger := "tie"
	switch {
	case yesSize > noSize:
		stronger = "yes"
	case noSize > yesSize:
		stronger = "no"
	}

	return Strength{
		ComputedAt:     time.Now().UTC().Format(time.RFC3339),
		MinLifetimePnL: minPnL,
		Market:         r.Market,
		SourceFetched:  r.FetchedAt,
		Yes: SideStats{
			Outcome:         yesOutcome,
			QualifiedCount:  yesCount,
			QualifiedSize:   yesSize,
			Strength:        yesStrength,
			StrengthPercent: yesStrength * 100,
		},
		No: SideStats{
			Outcome:         noOutcome,
			QualifiedCount:  noCount,
			QualifiedSize:   noSize,
			Strength:        noStrength,
			StrengthPercent: noStrength * 100,
		},
		Stronger:       stronger,
		TotalQualified: total,
	}
}

func sumQualified(holders []polymarket.HolderRecord, minPnL float64) (size float64, count int) {
	for _, h := range holders {
		if h.LifetimePnL == nil || *h.LifetimePnL <= minPnL {
			continue
		}
		size += h.Size
		count++
	}
	return size, count
}

// Format writes strength in the requested format: json | text | csv | tsv.
func Format(w io.Writer, s Strength, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	case "text", "txt", "table":
		return writeText(w, s)
	case "csv":
		return writeDelimited(w, s, ',')
	case "tsv":
		return writeDelimited(w, s, '\t')
	default:
		return fmt.Errorf("unknown format %q (want json, text, csv, tsv)", format)
	}
}

func writeText(w io.Writer, s Strength) error {
	_, err := fmt.Fprintf(w, `holder tabs · strength
market:    %s
condition: %s
threshold: lifetime_pnl > %.0f
fetched:   %s

side    qualified_size  holders  strength
%-6s  %14.2f  %7d  %6.2f%%
%-6s  %14.2f  %7d  %6.2f%%
total   %14.2f
stronger: %s
`,
		s.Market.Question,
		s.Market.ConditionID,
		s.MinLifetimePnL,
		s.SourceFetched,
		s.Yes.Outcome, s.Yes.QualifiedSize, s.Yes.QualifiedCount, s.Yes.StrengthPercent,
		s.No.Outcome, s.No.QualifiedSize, s.No.QualifiedCount, s.No.StrengthPercent,
		s.TotalQualified,
		s.Stronger,
	)
	return err
}

func writeDelimited(w io.Writer, s Strength, comma rune) error {
	cw := csv.NewWriter(w)
	cw.Comma = comma
	rows := [][]string{
		{"side", "outcome", "qualified_count", "qualified_size", "strength", "strength_percent", "min_lifetime_pnl", "stronger", "condition_id", "slug", "question"},
		sideRow("yes", s.Yes, s),
		sideRow("no", s.No, s),
	}
	for _, row := range rows {
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func sideRow(side string, st SideStats, s Strength) []string {
	return []string{
		side,
		st.Outcome,
		fmt.Sprintf("%d", st.QualifiedCount),
		fmt.Sprintf("%.6f", st.QualifiedSize),
		fmt.Sprintf("%.8f", st.Strength),
		fmt.Sprintf("%.4f", st.StrengthPercent),
		fmt.Sprintf("%.2f", s.MinLifetimePnL),
		s.Stronger,
		s.Market.ConditionID,
		s.Market.Slug,
		s.Market.Question,
	}
}
