package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

func main() {
	tag := flag.String("tag", "", "Tag slug (e.g. politics, elections) - resolved via Gamma /tags/slug/{slug}")
	tagID := flag.Int("tag-id", 0, "Tag ID (optional; skips slug lookup)")
	minVol := flag.Float64("min-volume-24h", 10000, "Minimum 24h volume (exclusive); markets sorted by volume24hr desc")
	limit := flag.Int("limit", 0, "Max markets to keep (0 = all above min volume)")
	binaryOnly := flag.Bool("binary", true, "Keep only Yes/No markets")
	activeOnly := flag.Bool("active", true, "Keep only active, non-closed markets")
	outPath := flag.String("out", "", "Output path (default: data/markets-<tag>.json)")
	format := flag.String("format", "json", "Output format: json, csv, text")
	timeout := flag.Duration("timeout", 120*time.Second, "Overall request timeout")
	flag.Parse()

	if *tag == "" && *tagID == 0 {
		fmt.Fprintln(os.Stderr, "usage: list-markets -tag politics|elections [-min-volume-24h 10000] [-out path] [-format json|csv|text]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	client := polymarket.NewClient()
	catalog, err := client.ListMarkets(ctx, polymarket.ListMarketsOptions{
		TagSlug:     *tag,
		TagID:       *tagID,
		MinVolume24: *minVol,
		BinaryOnly:  *binaryOnly,
		ActiveOnly:  *activeOnly,
		Limit:       *limit,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	path := *outPath
	if path == "" {
		name := catalog.Tag.Slug
		if name == "" {
			name = fmt.Sprintf("tag-%d", catalog.Tag.ID)
		}
		ext := "json"
		switch strings.ToLower(*format) {
		case "csv":
			ext = "csv"
		case "text", "txt":
			ext = "txt"
		}
		path = filepath.Join("data", fmt.Sprintf("markets-%s.%s", name, ext))
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()

	switch strings.ToLower(*format) {
	case "json", "":
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(catalog); err != nil {
			fmt.Fprintf(os.Stderr, "error: encode: %v\n", err)
			os.Exit(1)
		}
	case "csv":
		if err := writeCSV(f, catalog); err != nil {
			fmt.Fprintf(os.Stderr, "error: csv: %v\n", err)
			os.Exit(1)
		}
	case "text", "txt":
		if err := writeText(f, catalog); err != nil {
			fmt.Fprintf(os.Stderr, "error: text: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "error: unknown format %q\n", *format)
		os.Exit(2)
	}

	fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	fmt.Fprintf(os.Stderr, "tag: %s (%d)  markets: %d  min_volume_24h: %.0f\n",
		catalog.Tag.Label, catalog.Tag.ID, catalog.Count, catalog.MinVolume24)
	if catalog.Count > 0 {
		fmt.Fprintf(os.Stderr, "top: %.0f  %s\n", catalog.Markets[0].Volume24hr, catalog.Markets[0].Question)
	}
}

func writeCSV(f *os.File, c *polymarket.MarketCatalog) error {
	w := csv.NewWriter(f)
	if err := w.Write([]string{
		"condition_id", "slug", "question", "event_slug", "event_title",
		"volume_24hr", "volume", "liquidity", "url", "outcomes",
	}); err != nil {
		return err
	}
	for _, m := range c.Markets {
		if err := w.Write([]string{
			m.ConditionID,
			m.Slug,
			m.Question,
			m.EventSlug,
			m.EventTitle,
			fmt.Sprintf("%.6f", m.Volume24hr),
			fmt.Sprintf("%.6f", m.Volume),
			fmt.Sprintf("%.6f", m.Liquidity),
			m.URL,
			strings.Join(m.Outcomes, "|"),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writeText(f *os.File, c *polymarket.MarketCatalog) error {
	if _, err := fmt.Fprintf(f, "markets · %s (%s / %d)\nmin_volume_24h: > %.0f\ncount: %d\nfetched: %s\n\n",
		c.Tag.Label, c.Tag.Slug, c.Tag.ID, c.MinVolume24, c.Count, c.FetchedAt); err != nil {
		return err
	}
	for i, m := range c.Markets {
		if _, err := fmt.Fprintf(f, "%3d  %12.0f  %s\n", i+1, m.Volume24hr, m.Question); err != nil {
			return err
		}
	}
	return nil
}
