package polymarket

import "strings"

// pagination is the shared v2 page envelope. next_cursor is null on the last page.
type pagination struct {
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

func (p pagination) next() string {
	if p.NextCursor == nil {
		return ""
	}
	return *p.NextCursor
}

type positionWire struct {
	ProxyWallet  string  `json:"proxy_wallet"`
	TokenID      string  `json:"token_id"`
	ConditionID  string  `json:"condition_id"`
	CurrentSize  float64 `json:"current_size"`
	AvgPrice     float64 `json:"avg_price"`
	CurrentPrice float64 `json:"current_price"`
	Title        string  `json:"title"`
	Slug         string  `json:"slug"`
	EventSlug    string  `json:"event_slug"`
	Outcome      string  `json:"outcome"`
}

func (w positionWire) position() Position {
	return Position{
		ProxyWallet: w.ProxyWallet,
		Asset:       w.TokenID,
		ConditionID: w.ConditionID,
		Size:        w.CurrentSize,
		AvgPrice:    w.AvgPrice,
		CurPrice:    w.CurrentPrice,
		Title:       w.Title,
		Slug:        w.Slug,
		EventSlug:   w.EventSlug,
		Outcome:     w.Outcome,
	}
}

type activityWire struct {
	ProxyWallet     string  `json:"proxy_wallet"`
	Timestamp       int64   `json:"timestamp"`
	ConditionID     string  `json:"condition_id"`
	Type            string  `json:"type"`
	Size            float64 `json:"size"`
	USDCSize        float64 `json:"usdc_size"`
	TransactionHash string  `json:"transaction_hash"`
	Price           float64 `json:"price"`
	TokenID         string  `json:"token_id"`
	Side            string  `json:"side"`
	OutcomeIndex    int     `json:"outcome_index"`
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	Icon            string  `json:"icon"`
	EventSlug       string  `json:"event_slug"`
	Outcome         string  `json:"outcome"`
	Name            string  `json:"name"`
	Pseudonym       string  `json:"pseudonym"`
}

func (w activityWire) activity() Activity {
	return Activity{
		ProxyWallet:     w.ProxyWallet,
		Timestamp:       w.Timestamp,
		ConditionID:     w.ConditionID,
		Type:            w.Type,
		Size:            w.Size,
		USDCSize:        w.USDCSize,
		TransactionHash: w.TransactionHash,
		Price:           w.Price,
		Asset:           w.TokenID,
		Side:            w.Side,
		OutcomeIndex:    w.OutcomeIndex,
		Title:           w.Title,
		Slug:            w.Slug,
		Icon:            w.Icon,
		EventSlug:       w.EventSlug,
		Outcome:         w.Outcome,
		Name:            w.Name,
		Pseudonym:       w.Pseudonym,
	}
}

type tradeWire struct {
	ProxyWallet     string  `json:"proxy_wallet"`
	Side            string  `json:"side"`
	TokenID         string  `json:"token_id"`
	ConditionID     string  `json:"condition_id"`
	Size            float64 `json:"size"`
	Price           float64 `json:"price"`
	Timestamp       int64   `json:"timestamp"`
	Title           string  `json:"title"`
	Slug            string  `json:"slug"`
	Icon            string  `json:"icon"`
	EventSlug       string  `json:"event_slug"`
	Outcome         string  `json:"outcome"`
	OutcomeIndex    int     `json:"outcome_index"`
	Name            string  `json:"name"`
	Pseudonym       string  `json:"pseudonym"`
	TransactionHash string  `json:"transaction_hash"`
}

func (w tradeWire) trade() Trade {
	return Trade{
		ProxyWallet:     w.ProxyWallet,
		Side:            w.Side,
		Asset:           w.TokenID,
		ConditionID:     w.ConditionID,
		Size:            w.Size,
		Price:           w.Price,
		Timestamp:       w.Timestamp,
		Title:           w.Title,
		Slug:            w.Slug,
		Icon:            w.Icon,
		EventSlug:       w.EventSlug,
		Outcome:         w.Outcome,
		OutcomeIndex:    w.OutcomeIndex,
		Name:            w.Name,
		Pseudonym:       w.Pseudonym,
		TransactionHash: w.TransactionHash,
	}
}

// v2PositionSort maps the v1 sortBy value CURRENT onto v2's CURRENT_VALUE.
func v2PositionSort(sortBy string) string {
	s := strings.TrimSpace(sortBy)
	if strings.EqualFold(s, "CURRENT") {
		return "CURRENT_VALUE"
	}
	return s
}
