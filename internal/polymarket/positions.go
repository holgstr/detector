package polymarket

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const defaultPositionLimit = 50

// Position is one Data API /positions row (shares held of one outcome).
type Position struct {
	ProxyWallet string  `json:"proxyWallet"`
	Asset       string  `json:"asset"`
	ConditionID string  `json:"conditionId"`
	Size        float64 `json:"size"`
	AvgPrice    float64 `json:"avgPrice"`
	CurPrice    float64 `json:"curPrice"`
	Title       string  `json:"title"`
	Slug        string  `json:"slug"`
	EventSlug   string  `json:"eventSlug"`
	Outcome     string  `json:"outcome"`
}

// FetchPositionsOptions filters a wallet's open positions.
type FetchPositionsOptions struct {
	User   string
	Market string // condition ID; empty = all markets
	Limit  int    // default 50, max 500
}

// FetchPositions returns open positions for a wallet from /positions.
func (c *Client) FetchPositions(ctx context.Context, opt FetchPositionsOptions) ([]Position, error) {
	user := strings.TrimSpace(opt.User)
	if user == "" {
		return nil, fmt.Errorf("user wallet required")
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = defaultPositionLimit
	}
	if limit > 500 {
		limit = 500
	}

	q := url.Values{}
	q.Set("user", user)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("sizeThreshold", "0")
	if m := strings.TrimSpace(opt.Market); m != "" {
		q.Set("market", m)
	}

	var out []Position
	if err := c.getJSON(ctx, dataBase+"/positions?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// NetPosition is YES minus NO shares (or a single non-binary outcome).
type NetPosition struct {
	Size    float64
	Outcome string
	Known   bool
}

// NetShares collapses Yes/No legs into one signed net, matching the
// Activity tab: long Yes if yesSize > noSize, else long No.
func NetShares(positions []Position) NetPosition {
	var yes, no float64
	otherSize := 0.0
	otherOut := ""
	for _, p := range positions {
		switch parseOutcomeSide(p.Outcome) {
		case "yes":
			yes += p.Size
		case "no":
			no += p.Size
		default:
			if p.Size > otherSize {
				otherSize = p.Size
				otherOut = strings.TrimSpace(p.Outcome)
			}
		}
	}
	net := yes - no
	if net > 0 {
		return NetPosition{Size: net, Outcome: "YES", Known: true}
	}
	if net < 0 {
		return NetPosition{Size: -net, Outcome: "NO", Known: true}
	}
	if otherSize > 0 && otherOut != "" {
		return NetPosition{Size: otherSize, Outcome: strings.ToUpper(otherOut), Known: true}
	}
	return NetPosition{Known: true}
}

func parseOutcomeSide(outcome string) string {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "yes":
		return "yes"
	case "no":
		return "no"
	default:
		return ""
	}
}
