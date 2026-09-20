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

const (
	posShareEps     = 0.5
	posFetchWorkers = 16
)

type marketFinder interface {
	FindMarket(ctx context.Context, query string) (polymarket.SearchMarket, error)
}

type posMarketAPI interface {
	marketFinder
	SearchMarkets(ctx context.Context, query string) ([]polymarket.SearchMarket, error)
}

// PosHolding is one tracked wallet's net shares in a market.
type PosHolding struct {
	Name     string
	Wallet   string
	Size     float64
	Outcome  string
	CurPrice float64
	AvgPrice float64
	HasCur   bool
	HasAvg   bool
}

// PosReport is /pos output: current nets for tracked traders in one market.
type PosReport struct {
	Query         string
	Title         string
	Slug          string
	URL           string
	OverallSize   float64
	OverallSide   string
	CurPrice      float64
	HasCur        bool
	Holdings      []PosHolding
	FailedWallets int
}

func signedHolding(size float64, outcome string) float64 {
	if strings.EqualFold(strings.TrimSpace(outcome), "NO") {
		return -size
	}
	return size
}

// BuildPosReport nets each wallet's Yes/No legs, drops flats, and sorts YES
// first (largest size first), then NO.
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
		net, ok := primaryNetHolding(byWallet[addr])
		if !ok {
			continue
		}
		holdings = append(holdings, PosHolding{
			Name:     w.Name,
			Wallet:   addr,
			Size:     net.Size,
			Outcome:  net.Outcome,
			CurPrice: net.CurPrice,
			AvgPrice: net.AvgPrice,
			HasCur:   net.HasCur,
			HasAvg:   net.HasAvg,
		})
		overall += signedHolding(net.Size, net.Outcome)
	}

	sort.SliceStable(holdings, func(i, j int) bool {
		ri := posOutcomeRank(holdings[i].Outcome)
		rj := posOutcomeRank(holdings[j].Outcome)
		if ri != rj {
			return ri < rj
		}
		if holdings[i].Size != holdings[j].Size {
			return holdings[i].Size > holdings[j].Size
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
	if px, ok := marketPriceForSide(holdings, rep.OverallSide); ok {
		rep.CurPrice = px
		rep.HasCur = true
	}
	return rep
}

func marketPriceForSide(holdings []PosHolding, side string) (float64, bool) {
	if side == "" {
		return 0, false
	}
	for _, h := range holdings {
		if h.HasCur && strings.EqualFold(h.Outcome, side) {
			return h.CurPrice, true
		}
	}
	return 0, false
}

func posOutcomeRank(outcome string) int {
	switch strings.ToUpper(strings.TrimSpace(outcome)) {
	case "YES":
		return 0
	case "NO":
		return 1
	default:
		return 2
	}
}

// resolvePosMarket picks a market for /pos. Explicit slugs, URLs, and condition
// ids resolve directly; free-text queries prefer markets where tracked wallets
// hold positions when several matches share the top text rank.
func resolvePosMarket(ctx context.Context, api interface {
	posMarketAPI
	positionLookup
}, query string, wallets []sharps.Wallet) (polymarket.SearchMarket, error) {
	if polymarket.IsExplicitMarketRef(query) {
		return api.FindMarket(ctx, query)
	}
	hits, err := api.SearchMarkets(ctx, query)
	if err != nil {
		return polymarket.SearchMarket{}, err
	}
	candidates := polymarket.TopRankMatches(query, hits)
	if len(candidates) == 0 {
		return polymarket.SearchMarket{}, fmt.Errorf("no active market matching %q", query)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return pickMarketByTrackedPositions(ctx, api, candidates, wallets)
}

func pickMarketByTrackedPositions(ctx context.Context, api positionLookup, candidates []polymarket.SearchMarket, wallets []sharps.Wallet) (polymarket.SearchMarket, error) {
	byWallet, _ := fetchTrackedPositions(ctx, api, wallets, candidateConditionIDs(candidates))
	return pickMarketFromPositions(candidates, wallets, byWallet), nil
}

func candidateConditionIDs(candidates []polymarket.SearchMarket) []string {
	ids := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		id := strings.TrimSpace(c.Market.ConditionID)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func pickMarketFromPositions(candidates []polymarket.SearchMarket, wallets []sharps.Wallet, byWallet map[string][]polymarket.Position) polymarket.SearchMarket {
	if len(candidates) == 0 {
		return polymarket.SearchMarket{}
	}
	if len(wallets) == 0 || len(byWallet) == 0 {
		return candidates[0]
	}

	cidIndex := make(map[string]int, len(candidates))
	for i, c := range candidates {
		cid := strings.ToLower(strings.TrimSpace(c.Market.ConditionID))
		if cid == "" {
			continue
		}
		cidIndex[cid] = i
	}

	type marketScore struct {
		holders int
		size    float64
	}
	scores := make([]marketScore, len(candidates))
	for _, w := range wallets {
		addr := strings.ToLower(w.Address)
		byMarket := make(map[string][]polymarket.Position)
		for _, p := range byWallet[addr] {
			cid := strings.ToLower(strings.TrimSpace(p.ConditionID))
			byMarket[cid] = append(byMarket[cid], p)
		}
		for cid, positions := range byMarket {
			idx, ok := cidIndex[cid]
			if !ok {
				continue
			}
			np := polymarket.NetShares(positions)
			if !np.Known || math.Abs(np.Size) < posShareEps || strings.TrimSpace(np.Outcome) == "" {
				continue
			}
			scores[idx].holders++
			scores[idx].size += math.Abs(np.Size)
		}
	}

	bestIdx := 0
	for i := 1; i < len(candidates); i++ {
		if scores[i].holders > scores[bestIdx].holders ||
			(scores[i].holders == scores[bestIdx].holders && scores[i].size > scores[bestIdx].size) {
			bestIdx = i
		}
	}
	return candidates[bestIdx]
}

func filterPositionsByMarket(byWallet map[string][]polymarket.Position, conditionID string) map[string][]polymarket.Position {
	cid := strings.ToLower(strings.TrimSpace(conditionID))
	if cid == "" {
		return byWallet
	}
	out := make(map[string][]polymarket.Position, len(byWallet))
	for addr, pos := range byWallet {
		var keep []polymarket.Position
		for _, p := range pos {
			if strings.ToLower(strings.TrimSpace(p.ConditionID)) == cid {
				keep = append(keep, p)
			}
		}
		if len(keep) > 0 {
			out[addr] = keep
		}
	}
	return out
}

func posWorkerCount(n int) int {
	if n <= 1 {
		return n
	}
	w := posFetchWorkers
	if w > n {
		return n
	}
	return w
}

// fetchTrackedPositions loads open positions for every tracked wallet in one
// parallel round (up to posFetchWorkers in flight). markets are condition IDs;
// several IDs are sent as a CSV so one request covers disambiguation candidates.
func fetchTrackedPositions(ctx context.Context, api positionLookup, wallets []sharps.Wallet, markets []string) (map[string][]polymarket.Position, int) {
	byWallet := make(map[string][]polymarket.Position, len(wallets))
	if len(wallets) == 0 {
		return byWallet, 0
	}
	market := strings.Join(markets, ",")
	limit := 0
	if n := len(markets); n > 1 {
		limit = 2 * n
		if limit < 50 {
			limit = 50
		}
		if limit > 500 {
			limit = 500
		}
	}

	type res struct {
		addr string
		pos  []polymarket.Position
		err  error
	}
	results := make(chan res, len(wallets))
	jobs := make(chan sharps.Wallet)
	var wg sync.WaitGroup
	workers := posWorkerCount(len(wallets))
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range jobs {
				pos, err := api.FetchPositions(ctx, polymarket.FetchPositionsOptions{
					User:   w.Address,
					Market: market,
					Limit:  limit,
				})
				results <- res{addr: strings.ToLower(w.Address), pos: pos, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, w := range wallets {
			select {
			case <-ctx.Done():
				return
			case jobs <- w:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	failed := 0
	for r := range results {
		if r.err != nil {
			failed++
			continue
		}
		byWallet[r.addr] = r.pos
	}
	return byWallet, failed
}

// FetchPosReport finds the market and loads tracked wallets' open positions
// in a single parallel round. Ambiguous names reuse that round both to pick
// the market and to fill holdings, so wallets are not fetched twice.
func FetchPosReport(ctx context.Context, api interface {
	posMarketAPI
	positionLookup
}, query string, wallets []sharps.Wallet) (PosReport, error) {
	hit, byWallet, failed, err := loadPosMarketAndPositions(ctx, api, query, wallets)
	if err != nil {
		return PosReport{Query: query}, err
	}
	rep := BuildPosReport(query, hit, wallets, byWallet, failed)
	if failed == len(wallets) && len(wallets) > 0 {
		return rep, fmt.Errorf("couldn't load positions")
	}
	return rep, nil
}

func loadPosMarketAndPositions(ctx context.Context, api interface {
	posMarketAPI
	positionLookup
}, query string, wallets []sharps.Wallet) (polymarket.SearchMarket, map[string][]polymarket.Position, int, error) {
	if polymarket.IsExplicitMarketRef(query) {
		hit, err := api.FindMarket(ctx, query)
		if err != nil {
			return polymarket.SearchMarket{}, nil, 0, err
		}
		byWallet, failed := fetchTrackedPositions(ctx, api, wallets, []string{hit.Market.ConditionID})
		return hit, byWallet, failed, nil
	}

	hits, err := api.SearchMarkets(ctx, query)
	if err != nil {
		return polymarket.SearchMarket{}, nil, 0, err
	}
	candidates := polymarket.TopRankMatches(query, hits)
	if len(candidates) == 0 {
		return polymarket.SearchMarket{}, nil, 0, fmt.Errorf("no active market matching %q", query)
	}
	if len(candidates) == 1 {
		byWallet, failed := fetchTrackedPositions(ctx, api, wallets, []string{candidates[0].Market.ConditionID})
		return candidates[0], byWallet, failed, nil
	}

	cids := candidateConditionIDs(candidates)
	all, failed := fetchTrackedPositions(ctx, api, wallets, cids)
	hit := pickMarketFromPositions(candidates, wallets, all)
	return hit, filterPositionsByMarket(all, hit.Market.ConditionID), failed, nil
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
	head := title

	if len(r.Holdings) == 0 {
		body := head + "\nNo tracked holdings in this market."
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

func posSides(holdings []PosHolding) []string {
	sides := []string{"YES", "NO"}
	seen := map[string]bool{"YES": true, "NO": true}
	for _, h := range holdings {
		o := strings.ToUpper(strings.TrimSpace(h.Outcome))
		if o == "" || seen[o] {
			continue
		}
		seen[o] = true
		sides = append(sides, o)
	}
	return sides
}

func formatPosSide(side string, holdings []PosHolding) string {
	var people []PosHolding
	for _, h := range holdings {
		if strings.EqualFold(h.Outcome, side) {
			people = append(people, h)
		}
	}
	if len(people) == 0 {
		return ""
	}
	px, hasPx := marketPriceForSide(people, side)
	var b strings.Builder
	b.WriteString(side)
	b.WriteString(formatAtPrice(hasPx, px))
	for _, h := range people {
		fmt.Fprintf(&b, "\n%s %s%s", h.Name, formatShares(h.Size), formatAtPrice(h.HasAvg, h.AvgPrice))
	}
	return b.String()
}
