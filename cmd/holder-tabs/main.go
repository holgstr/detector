package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/holgstr/detector/internal/holdertabs"
	"github.com/holgstr/detector/internal/polymarket"
)

func main() {
	inPath := flag.String("in", "", "Path to holders intermediate JSON (from market-holders)")
	outPath := flag.String("out", "", "Optional output path (default: stdout)")
	format := flag.String("format", "text", "Output format: json, text, csv, tsv")
	minPnL := flag.Float64("min-pnl", holdertabs.DefaultMinLifetimePnL, "Minimum lifetime PnL to qualify a holder (exclusive)")
	flag.Parse()

	if *inPath == "" {
		fmt.Fprintln(os.Stderr, "usage: holder-tabs -in data/holders-<slug>.json [-format text|json|csv|tsv] [-min-pnl 100000] [-out path]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	raw, err := os.ReadFile(*inPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", *inPath, err)
		os.Exit(1)
	}

	var holders polymarket.Result
	if err := json.Unmarshal(raw, &holders); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse holders json: %v\n", err)
		os.Exit(1)
	}

	strength := holdertabs.ComputeStrength(&holders, *minPnL)

	out := os.Stdout
	if *outPath != "" {
		if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "error: mkdir: %v\n", err)
			os.Exit(1)
		}
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: create %s: %v\n", *outPath, err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}

	if err := holdertabs.Format(out, strength, *format); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *outPath != "" {
		fmt.Fprintf(os.Stderr, "wrote %s (%s)\n", *outPath, strings.ToLower(*format))
	}
}
