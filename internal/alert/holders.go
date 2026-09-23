package alert

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const (
	holdersPerSide     = 10
	holdersLookupLimit = 20 // data-api /holders cap; extras cover nets that drop a wallet off one side
)

type holderLookup interface {
	ListHolders(ctx context.Context, conditionID string, limit int) ([]polymarket.OutcomeHolder, error)
}

// HoldersReport is /holders output: top nets on each side of one market.
type HoldersReport struct {
	Query         string
	Title         string
	Slug          string
	URL           string
	Holdings      []PosHolding
	FailedWallets int
}

// BuildHoldersReport nets each wallet's Yes/No legs (positions when loaded,
// otherwise the holder rows) and keeps the largest holdersPerSide on each side.
// byWallet is keyed by lowercase address. Wallets in failed are ones whose
// position lookup failed; those fall back to the holder-list size with no price.
func BuildHoldersReport(query string, market polymarket.SearchMarket, raw []polymarket.OutcomeHolder, byWallet map[string][]polymarket.Position, failed map[string]struct{}) HoldersReport {
	rep := HoldersReport{
		Query:         query,
		Title:         marketTitle(market),
		Slug:          market.Market.Slug,
		URL:           market.Market.URL,
		FailedWallets: len(failed),
	}

	byAddr := make(map[string][]polymarket.OutcomeHolder)
	names := make(map[string]string)
	var order []string
	for _, h := range raw {
		addr := strings.ToLower(strings.TrimSpace(h.Wallet))
		if addr == "" {
			continue
		}
		if _, ok := byAddr[addr]; !ok {
			order = append(order, addr)
		}
		byAddr[addr] = append(byAddr[addr], h)
		if names[addr] == "" {
			names[addr] = strings.TrimSpace(h.Name)
		}
	}

	var all []PosHolding
	for _, addr := range order {
		positions, ok := byWallet[strings.ToLower(addr)]
		if !ok {
			if _, failedLookup := failed[addr]; failedLookup {
				positions = holderRowsAsPositions(byAddr[addr])
			}
		}
		net, kept := primaryNetHolding(positions)
		if !kept {
			continue
		}
		all = append(all, PosHolding{
			Name:     holderLabel(names[addr], addr),
			Wallet:   addr,
			Size:     net.Size,
			Outcome:  net.Outcome,
			CurPrice: net.CurPrice,
			AvgPrice: net.AvgPrice,
			HasCur:   net.HasCur,
			HasAvg:   net.HasAvg,
		})
	}

	var kept []PosHolding
	seenSide := map[string]bool{}
	for _, side := range posSides(nil) {
		seenSide[strings.ToUpper(side)] = true
		kept = append(kept, topHolders(all, side)...)
	}
	for _, h := range all {
		side := strings.ToUpper(strings.TrimSpace(h.Outcome))
		if side == "" || seenSide[side] {
			continue
		}
		seenSide[side] = true
		kept = append(kept, topHolders(all, side)...)
	}
	rep.Holdings = kept
	return rep
}

func topHolders(all []PosHolding, side string) []PosHolding {
	var rows []PosHolding
	for _, h := range all {
		if strings.EqualFold(h.Outcome, side) {
			rows = append(rows, h)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Size != rows[j].Size {
			return rows[i].Size > rows[j].Size
		}
		return rows[i].Name < rows[j].Name
	})
	if len(rows) > holdersPerSide {
		rows = rows[:holdersPerSide]
	}
	return rows
}

func holderRowsAsPositions(rows []polymarket.OutcomeHolder) []polymarket.Position {
	var out []polymarket.Position
	for _, h := range rows {
		if h.Size == 0 {
			continue
		}
		out = append(out, polymarket.Position{
			ConditionID: "_",
			Size:        h.Size,
			Outcome:     h.Outcome,
		})
	}
	return out
}

func holderLabel(name, wallet string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return shortWallet(wallet)
}

func marketTitle(market polymarket.SearchMarket) string {
	title := strings.TrimSpace(market.Market.Question)
	if title == "" {
		title = strings.TrimSpace(market.GroupItemTitle)
	}
	if title == "" {
		title = market.Market.Slug
	}
	return title
}

// FetchHoldersReport resolves the market, loads the holder list, then each
// wallet's open position in that market so a two-sided book is one net with
// an acquisition price.
func FetchHoldersReport(ctx context.Context, api interface {
	posMarketAPI
	positionLookup
	holderLookup
}, query string, wallets []sharps.Wallet) (HoldersReport, error) {
	query = strings.TrimSpace(query)
	hit, err := resolvePosMarket(ctx, api, query, wallets)
	if err != nil {
		return HoldersReport{Query: query}, err
	}
	raw, err := api.ListHolders(ctx, hit.Market.ConditionID, holdersLookupLimit)
	if err != nil {
		return HoldersReport{
			Query: query,
			Title: marketTitle(hit),
			Slug:  hit.Market.Slug,
			URL:   hit.Market.URL,
		}, err
	}

	seen := make(map[string]struct{})
	var toFetch []sharps.Wallet
	for _, h := range raw {
		addr := strings.ToLower(strings.TrimSpace(h.Wallet))
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		toFetch = append(toFetch, sharps.Wallet{Address: addr, Name: h.Name})
	}
	byWallet, _ := fetchTrackedPositions(ctx, api, toFetch, []string{hit.Market.ConditionID})
	failed := make(map[string]struct{})
	for addr := range seen {
		if _, ok := byWallet[addr]; !ok {
			failed[addr] = struct{}{}
		}
	}
	return BuildHoldersReport(query, hit, raw, byWallet, failed), nil
}

// FormatHoldersReport is one or more Telegram bodies (split under the 4096 cap).
func FormatHoldersReport(r HoldersReport) []string {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = r.Slug
	}
	if title == "" {
		title = strings.TrimSpace(r.Query)
	}
	head := title

	if len(r.Holdings) == 0 {
		body := head + "\nNo holders in this market."
		if r.FailedWallets > 0 {
			body += fmt.Sprintf("\n(%d wallet lookups failed.)", r.FailedWallets)
		}
		return []string{body}
	}

	var blocks []string
	for _, side := range posSides(r.Holdings) {
		if b := formatPosSide(side, r.Holdings); b != "" {
			blocks = append(blocks, b)
		}
	}
	if r.FailedWallets > 0 {
		blocks = append(blocks, fmt.Sprintf("(%d wallet lookups failed.)", r.FailedWallets))
	}
	return chunkTelegram(head, blocks)
}
