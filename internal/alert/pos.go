package alert

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const posShareEps = 0.5

type marketFinder interface {
	FindMarket(ctx context.Context, query string) (polymarket.SearchMarket, error)
}

// PosHolding is one tracked wallet's net shares in a market.
type PosHolding struct {
	Name    string
	Wallet  string
	Size    float64
	Outcome string
}

// PosReport is /pos output: current nets for tracked traders in one market.
type PosReport struct {
	Query         string
	Title         string
	Slug          string
	URL           string
	OverallSize   float64
	OverallSide   string
	Holdings      []PosHolding
	FailedWallets int
}

func signedHolding(size float64, outcome string) float64 {
	if strings.EqualFold(strings.TrimSpace(outcome), "NO") {
		return -size
	}
	return size
}

// BuildPosReport nets each wallet's Yes/No legs, drops flats, and sorts by
// the overall tracked side (net YES → largest YES first; otherwise largest NO).
func BuildPosReport(query string, market polymarket.SearchMarket, wallets []sharps.Wallet, byWallet map[string][]polymarket.Position, failed int) PosReport {
	title := strings.TrimSpace(market.Market.Question)
	if title == "" {
		title = strings.TrimSpace(market.GroupItemTitle)
	}
	if title == "" {
		title = market.Market.Slug
	}
	rep := PosReport{
		Query:         query,
		Title:         title,
		Slug:          market.Market.Slug,
		URL:           market.Market.URL,
		FailedWallets: failed,
	}

	var holdings []PosHolding
	var overall float64
	for _, w := range wallets {
		addr := strings.ToLower(w.Address)
		np := polymarket.NetShares(byWallet[addr])
		if !np.Known || math.Abs(np.Size) < posShareEps || strings.TrimSpace(np.Outcome) == "" {
			continue
		}
		holdings = append(holdings, PosHolding{
			Name:    w.Name,
			Wallet:  addr,
			Size:    np.Size,
			Outcome: np.Outcome,
		})
		overall += signedHolding(np.Size, np.Outcome)
	}

	yesFirst := overall >= 0
	sort.SliceStable(holdings, func(i, j int) bool {
		si := signedHolding(holdings[i].Size, holdings[i].Outcome)
		sj := signedHolding(holdings[j].Size, holdings[j].Outcome)
		if si != sj {
			if yesFirst {
				return si > sj
			}
			return si < sj
		}
		return holdings[i].Name < holdings[j].Name
	})
	rep.Holdings = holdings
	if overall > 0 {
		rep.OverallSize = overall
		rep.OverallSide = "YES"
	} else if overall < 0 {
		rep.OverallSize = -overall
		rep.OverallSide = "NO"
	}
	return rep
}

// FetchPosReport finds the market and loads tracked wallets' open positions.
func FetchPosReport(ctx context.Context, api interface {
	marketFinder
	positionLookup
}, query string, wallets []sharps.Wallet) (PosReport, error) {
	hit, err := api.FindMarket(ctx, query)
	if err != nil {
		return PosReport{Query: query}, err
	}

	byWallet := make(map[string][]polymarket.Position, len(wallets))
	failed := 0
	if len(wallets) > 0 {
		type res struct {
			addr string
			pos  []polymarket.Position
			err  error
		}
		results := make(chan res, len(wallets))
		var wg sync.WaitGroup
		workers := 4
		if workers > len(wallets) {
			workers = len(wallets)
		}
		jobs := make(chan sharps.Wallet)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for w := range jobs {
					pos, err := api.FetchPositions(ctx, polymarket.FetchPositionsOptions{
						User:   w.Address,
						Market: hit.Market.ConditionID,
					})
					results <- res{addr: strings.ToLower(w.Address), pos: pos, err: err}
				}
			}()
		}
		go func() {
			defer close(jobs)
			for _, w := range wallets {
				jobs <- w
			}
		}()
		go func() {
			wg.Wait()
			close(results)
		}()
		for r := range results {
			if r.err != nil {
				failed++
				continue
			}
			byWallet[r.addr] = r.pos
		}
	}
	rep := BuildPosReport(query, hit, wallets, byWallet, failed)
	if failed == len(wallets) && len(wallets) > 0 {
		return rep, fmt.Errorf("couldn't load positions")
	}
	return rep, nil
}

// FormatPosReport is one or more Telegram bodies (split under the 4096 cap).
func FormatPosReport(r PosReport) []string {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = r.Slug
	}
	if title == "" {
		title = strings.TrimSpace(r.Query)
	}
	head := "Holdings · " + title
	if r.OverallSide != "" {
		head += fmt.Sprintf("\nTracked net %s %s", formatShares(r.OverallSize), r.OverallSide)
	}

	if len(r.Holdings) == 0 {
		body := head + "\nNo tracked holdings in this market."
		if r.FailedWallets > 0 {
			body += fmt.Sprintf("\n(%d wallet lookups failed.)", r.FailedWallets)
		}
		return []string{body}
	}

	var blocks []string
	for _, h := range r.Holdings {
		blocks = append(blocks, fmt.Sprintf("%s %s  %s", formatShares(h.Size), h.Outcome, h.Name))
	}
	if r.FailedWallets > 0 {
		blocks = append(blocks, fmt.Sprintf("(%d wallet lookups failed.)", r.FailedWallets))
	}
	return chunkTelegram(head, blocks)
}
