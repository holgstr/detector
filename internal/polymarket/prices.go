package polymarket

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// PricePoint is one sampled outcome price. T is unix seconds, Price is 0–1.
type PricePoint struct {
	Time  int64
	Price float64
}

// PriceHistory is the CLOB price series for one outcome token.
type PriceHistory struct {
	Outcome string
	TokenID string
	Points  []PricePoint
}

// FetchYesPriceHistory loads sampled Yes prices (or the first outcome) between
// start and end. fidelityMin is the sample spacing in minutes (minimum 1).
// The series is one price per sample, ordered oldest-first.
func (c *Client) FetchYesPriceHistory(ctx context.Context, conditionID string, start, end int64, fidelityMin int) (PriceHistory, error) {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return PriceHistory{}, fmt.Errorf("empty condition id")
	}
	if end <= start {
		return PriceHistory{}, fmt.Errorf("empty price window")
	}
	if fidelityMin < 1 {
		fidelityMin = 1
	}
	g, err := c.fetchGammaByCondition(ctx, conditionID)
	if err != nil {
		return PriceHistory{}, err
	}
	m := toMarket(g)
	outcome, token, err := yesToken(m)
	if err != nil {
		return PriceHistory{}, err
	}
	q := url.Values{}
	q.Set("market", token)
	q.Set("startTs", strconv.FormatInt(start, 10))
	q.Set("endTs", strconv.FormatInt(end, 10))
	q.Set("fidelity", strconv.Itoa(fidelityMin))
	var body struct {
		History []struct {
			T int64   `json:"t"`
			P float64 `json:"p"`
		} `json:"history"`
	}
	if err := c.getJSON(ctx, c.clobAPI()+"/prices-history?"+q.Encode(), &body); err != nil {
		return PriceHistory{}, err
	}
	pts := make([]PricePoint, 0, len(body.History))
	for _, row := range body.History {
		if row.T < start || row.T > end {
			continue
		}
		if row.P < 0 || row.P > 1 {
			continue
		}
		pts = append(pts, PricePoint{Time: row.T, Price: row.P})
	}
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].Time == pts[j].Time {
			return pts[i].Price < pts[j].Price
		}
		return pts[i].Time < pts[j].Time
	})
	return PriceHistory{Outcome: outcome, TokenID: token, Points: pts}, nil
}

func yesToken(m Market) (outcome, token string, err error) {
	if len(m.TokenIDs) == 0 {
		return "", "", fmt.Errorf("no CLOB tokens for %s", m.ConditionID)
	}
	idx := 0
	for i, name := range m.Outcomes {
		if strings.EqualFold(strings.TrimSpace(name), "yes") {
			idx = i
			break
		}
	}
	if idx >= len(m.TokenIDs) {
		idx = 0
	}
	outcome = "Yes"
	if idx < len(m.Outcomes) && strings.TrimSpace(m.Outcomes[idx]) != "" {
		outcome = m.Outcomes[idx]
	}
	token = strings.TrimSpace(m.TokenIDs[idx])
	if token == "" {
		return "", "", fmt.Errorf("no CLOB tokens for %s", m.ConditionID)
	}
	return outcome, token, nil
}
