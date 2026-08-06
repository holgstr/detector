package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

func main() {
	market := flag.String("market", "", "Polymarket URL, market slug, or condition ID (required)")
	outPath := flag.String("out", "", "Output JSON path (default: data/holders-<slug-or-id>.json)")
	limit := flag.Int("limit", 20, "Max holders per side (API cap 20)")
	workers := flag.Int("workers", 16, "Concurrent lifetime-PnL requests")
	timeout := flag.Duration("timeout", 60*time.Second, "Overall request timeout")
	flag.Parse()

	if *market == "" {
		fmt.Fprintln(os.Stderr, "usage: market-holders -market <url|slug|conditionId> [-out path] [-limit 20]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	client := polymarket.NewClient()
	client.Workers = *workers

	result, err := client.BuildResult(ctx, *market, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	path := *outPath
	if path == "" {
		name := result.Market.Slug
		if name == "" {
			name = result.Market.ConditionID
		}
		path = filepath.Join("data", "holders-"+name+".json")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error: mkdir: %v\n", err)
		os.Exit(1)
	}

	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: encode: %v\n", err)
		os.Exit(1)
	}
	raw = append(raw, '\n')

	if err := os.WriteFile(path, raw, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	fmt.Fprintf(os.Stderr, "market: %s\n", result.Market.Question)
	fmt.Fprintf(os.Stderr, "yes holders: %d  no holders: %d\n", len(result.Yes.Holders), len(result.No.Holders))
}
