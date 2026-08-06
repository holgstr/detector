package polymarket

// Market is the resolved Polymarket market metadata we care about.
type Market struct {
	ConditionID string   `json:"condition_id"`
	Slug        string   `json:"slug"`
	Question    string   `json:"question"`
	Outcomes    []string `json:"outcomes"`
	EventSlug   string   `json:"event_slug,omitempty"`
	URL         string   `json:"url,omitempty"`
}

// HolderSide is one outcome side's ranked holders with lifetime PnL.
type HolderSide struct {
	Outcome string         `json:"outcome"`
	TokenID string         `json:"token_id"`
	Holders []HolderRecord `json:"holders"`
}

// HolderRecord is the intermediate row for later tabulations.
type HolderRecord struct {
	Holder      string   `json:"holder"`
	Name        string   `json:"name,omitempty"`
	Size        float64  `json:"size"`
	LifetimePnL *float64 `json:"lifetime_pnl"`
}

// Result is the intermediate artifact written to disk.
type Result struct {
	FetchedAt string      `json:"fetched_at"`
	Market    Market      `json:"market"`
	Yes       HolderSide  `json:"yes"`
	No        HolderSide  `json:"no"`
}

type gammaMarket struct {
	ConditionID  string `json:"conditionId"`
	Slug         string `json:"slug"`
	Question     string `json:"question"`
	Outcomes     string `json:"outcomes"`
	ClobTokenIDs string `json:"clobTokenIds"`
	Events       []struct {
		Slug string `json:"slug"`
	} `json:"events"`
}

type holdersResponse struct {
	Token   string `json:"token"`
	Holders []struct {
		ProxyWallet  string  `json:"proxyWallet"`
		Name         string  `json:"name"`
		Amount       float64 `json:"amount"`
		OutcomeIndex int     `json:"outcomeIndex"`
	} `json:"holders"`
}

type leaderboardEntry struct {
	ProxyWallet string  `json:"proxyWallet"`
	UserName    string  `json:"userName"`
	PnL         float64 `json:"pnl"`
	Vol         float64 `json:"vol"`
}
