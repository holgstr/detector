package holdertabs

import (
	"sort"
	"strings"

	"github.com/holgstr/detector/internal/polymarket"
)

// FlowOptions filters which taker fills count toward a market's flow.
type FlowOptions struct {
	SkipWallets map[string]struct{} // lowercase addresses to drop (tracked sharps)
	MinUSD      float64             // inclusive minimum fill notional (0 = off)
	MaxUSD      float64             // inclusive maximum fill notional (0 = off)
}

// FlowTaker is one wallet's aggregated aggressor flow on a market.
type FlowTaker struct {
	Wallet  string  `json:"wallet"`
	Name    string  `json:"name"`
	USD     float64 `json:"usd"`
	Orders  int     `json:"orders"`
	NetUSD  float64 `json:"net_usd"` // Yes-pressure: +buy yes / +sell no
	Outcome string  `json:"outcome"` // "yes", "no"
}

// FlowStats is the taker-flow snapshot for one market over a lookback window.
type FlowStats struct {
	TakerUSD     float64     `json:"taker_usd"`
	Orders       int         `json:"orders"`
	Unique       int         `json:"unique"`
	YesUSD       float64     `json:"yes_usd"`
	NoUSD        float64     `json:"no_usd"`
	YesOrders    int         `json:"yes_orders"`
	NoOrders     int         `json:"no_orders"`
	YesTakers    int         `json:"yes_takers"`
	NoTakers     int         `json:"no_takers"`
	NetUSD       float64     `json:"net_usd"`
	NetUSDAbs    float64     `json:"net_usd_abs"`
	Stronger     string      `json:"stronger"` // "yes", "no", or "tie"
	YesTakerList []FlowTaker `json:"yes_takers_list,omitempty"`
	NoTakerList  []FlowTaker `json:"no_takers_list,omitempty"`
}

// TradeUSD is the USDC notional of a fill (shares × price).
func TradeUSD(t polymarket.Trade) float64 {
	return t.Size * t.Price
}

// YesPressure is signed USDC flow: buying Yes or selling No is positive.
func YesPressure(t polymarket.Trade) float64 {
	usd := TradeUSD(t)
	yes := isYesOutcome(t.Outcome)
	buy := strings.EqualFold(t.Side, "BUY")
	switch {
	case yes && buy:
		return usd
	case yes && !buy:
		return -usd
	case !yes && buy:
		return -usd
	default:
		return usd
	}
}

func isYesOutcome(outcome string) bool {
	return strings.EqualFold(strings.TrimSpace(outcome), "Yes")
}

// AggregateTakerFlows sums aggressor fills into per-market flow stats.
func AggregateTakerFlows(trades []polymarket.Trade, opt FlowOptions) FlowStats {
	skip := opt.SkipWallets
	type acc struct {
		wallet string
		name   string
		usd    float64
		orders int
		net    float64
	}
	byWallet := make(map[string]*acc)
	var s FlowStats

	for _, t := range trades {
		wallet := strings.ToLower(strings.TrimSpace(t.ProxyWallet))
		if wallet == "" {
			continue
		}
		if skip != nil {
			if _, ok := skip[wallet]; ok {
				continue
			}
		}
		usd := TradeUSD(t)
		if opt.MinUSD > 0 && usd < opt.MinUSD {
			continue
		}
		if opt.MaxUSD > 0 && usd > opt.MaxUSD {
			continue
		}

		s.TakerUSD += usd
		s.Orders++
		s.NetUSD += YesPressure(t)
		if isYesOutcome(t.Outcome) {
			s.YesUSD += usd
			s.YesOrders++
		} else {
			s.NoUSD += usd
			s.NoOrders++
		}

		row := byWallet[wallet]
		if row == nil {
			row = &acc{wallet: wallet, name: displayName(t)}
			byWallet[wallet] = row
		}
		if row.name == "" {
			row.name = displayName(t)
		}
		row.usd += usd
		row.orders++
		row.net += YesPressure(t)
	}

	s.Unique = len(byWallet)
	if s.NetUSD > 0 {
		s.Stronger = "yes"
	} else if s.NetUSD < 0 {
		s.Stronger = "no"
	} else {
		s.Stronger = "tie"
	}
	if s.NetUSD < 0 {
		s.NetUSDAbs = -s.NetUSD
	} else {
		s.NetUSDAbs = s.NetUSD
	}

	yesList := make([]FlowTaker, 0)
	noList := make([]FlowTaker, 0)
	for _, row := range byWallet {
		ft := FlowTaker{
			Wallet: row.wallet,
			Name:   row.name,
			USD:    row.usd,
			Orders: row.orders,
			NetUSD: row.net,
		}
		if row.net > 0 {
			ft.Outcome = "yes"
			yesList = append(yesList, ft)
		} else if row.net < 0 {
			ft.Outcome = "no"
			noList = append(noList, ft)
		}
	}
	sortTakers(yesList)
	sortTakers(noList)
	s.YesTakers = len(yesList)
	s.NoTakers = len(noList)
	s.YesTakerList = yesList
	s.NoTakerList = noList
	return s
}

func displayName(t polymarket.Trade) string {
	name := strings.TrimSpace(t.Name)
	pseudo := strings.TrimSpace(t.Pseudonym)
	if name != "" && !strings.HasPrefix(strings.ToLower(name), "0x") {
		return name
	}
	if pseudo != "" {
		return pseudo
	}
	return name
}

func sortTakers(list []FlowTaker) {
	sort.Slice(list, func(i, j int) bool { return list[i].USD > list[j].USD })
}
