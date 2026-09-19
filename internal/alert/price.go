package alert

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/holgstr/detector/internal/polymarket"
)

// PriceAlertRepeat is the cooldown between pings while the book still qualifies.
const PriceAlertRepeat = time.Hour

const priceAlertConfirmHint = "Reply with ask price and min size, e.g. 32 1000 (32¢, 1000 shares to take). /cancel to abort."

// PriceAlertDraft is an unresolved /alert <market> waiting for price + min size.
type PriceAlertDraft struct {
	Query       string `json:"query,omitempty"`
	Title       string `json:"title,omitempty"`
	Slug        string `json:"slug,omitempty"`
	URL         string `json:"url,omitempty"`
	ConditionID string `json:"condition_id,omitempty"`
}

// PriceAlert watches Yes ask size at Price or lower (size you can take).
type PriceAlert struct {
	Title            string  `json:"title,omitempty"`
	Slug             string  `json:"slug,omitempty"`
	URL              string  `json:"url,omitempty"`
	ConditionID      string  `json:"condition_id"`
	Outcome          string  `json:"outcome,omitempty"`
	Price            float64 `json:"price"`
	MinSize          float64 `json:"min_size"`
	LastNotifiedUnix int64   `json:"last_notified_unix,omitempty"`
}

type yesBookAPI interface {
	FindMarket(ctx context.Context, query string) (polymarket.SearchMarket, error)
	FetchYesBook(ctx context.Context, conditionID string) (polymarket.OutcomeBook, error)
}

// PriceAlertHit is one ping from CheckPriceAlerts.
type PriceAlertHit struct {
	Alert PriceAlert
	Size  float64
}

// PriceAlertPrompt is the Telegram body after a market is found.
func PriceAlertPrompt(d PriceAlertDraft) string {
	title := strings.TrimSpace(d.Title)
	if title == "" {
		title = d.Slug
	}
	if title == "" {
		title = d.Query
	}
	var b strings.Builder
	b.WriteString("Watch Yes asks (take) on:\n")
	b.WriteString(title)
	if u := strings.TrimSpace(d.URL); u != "" {
		b.WriteByte('\n')
		b.WriteString(u)
	}
	b.WriteByte('\n')
	b.WriteString(priceAlertConfirmHint)
	return b.String()
}

// PriceAlertSetText confirms a live watch.
func PriceAlertSetText(a PriceAlert) string {
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = a.Slug
	}
	return fmt.Sprintf(
		"Alert set · %s\nPing when Yes asks at %s or lower total ≥ %s shares (take).\nFirst hit once, then every 1h while it stays. /unalert to stop.",
		title,
		formatTickPrice(a.Price, 0.01),
		formatTickSize(a.MinSize),
	)
}

// PriceAlertPingText is the Telegram body when the book qualifies.
func PriceAlertPingText(a PriceAlert, size float64) string {
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = a.Slug
	}
	return fmt.Sprintf(
		"Ask alert · %s\n%s shares at %s or lower (need %s)",
		title,
		formatTickSize(size),
		formatTickPrice(a.Price, 0.01),
		formatTickSize(a.MinSize),
	)
}

// FormatPriceAlertList is /alert with no args.
func FormatPriceAlertList(alerts []PriceAlert) string {
	if len(alerts) == 0 {
		return "No price alerts. /alert <market> then reply with ask price and min size."
	}
	var b strings.Builder
	b.WriteString("Price alerts:")
	for _, a := range alerts {
		title := strings.TrimSpace(a.Title)
		if title == "" {
			title = a.Slug
		}
		b.WriteString(fmt.Sprintf("\n%s — take %s or lower, min %s", title, formatTickPrice(a.Price, 0.01), formatTickSize(a.MinSize)))
	}
	b.WriteString("\n/unalert <market> to stop one. /alert <market> to add.")
	return b.String()
}

// ParseAlertConfirm reads "32 1000", "32c 1k", "0.32 1,000".
func ParseAlertConfirm(text string) (price, minSize float64, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 2 {
		return 0, 0, false
	}
	price, ok1 := parseProb(fields[0])
	minSize, ok2 := parseShareSize(fields[1])
	if !ok1 || !ok2 || price <= 0 || price >= 1 || minSize <= 0 {
		return 0, 0, false
	}
	return price, minSize, true
}

func parseShareSize(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	lower := strings.ToLower(s)
	mult := 1.0
	switch {
	case strings.HasSuffix(lower, "mm"):
		s = strings.TrimSpace(s[:len(s)-2])
		mult = 1e6
	case strings.HasSuffix(lower, "m"):
		s = strings.TrimSpace(s[:len(s)-1])
		mult = 1e6
	case strings.HasSuffix(lower, "k"):
		s = strings.TrimSpace(s[:len(s)-1])
		mult = 1e3
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) && r != '.' {
			return 0, false
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v * mult, true
}

// DraftFromMarket builds a pending /alert after FindMarket.
func DraftFromMarket(query string, hit polymarket.SearchMarket) PriceAlertDraft {
	title := strings.TrimSpace(hit.Market.Question)
	if title == "" {
		title = strings.TrimSpace(hit.GroupItemTitle)
	}
	if title == "" {
		title = hit.Market.Slug
	}
	return PriceAlertDraft{
		Query:       strings.TrimSpace(query),
		Title:       title,
		Slug:        hit.Market.Slug,
		URL:         hit.Market.URL,
		ConditionID: hit.Market.ConditionID,
	}
}

// UpsertPriceAlert replaces a watch on the same condition id.
func (s *State) UpsertPriceAlert(a PriceAlert) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(a.ConditionID)
	kept := s.PriceAlerts[:0]
	for _, old := range s.PriceAlerts {
		if strings.TrimSpace(old.ConditionID) == id {
			continue
		}
		kept = append(kept, old)
	}
	s.PriceAlerts = append(kept, a)
	s.PendingPriceAlert = nil
}

// ClearPendingPriceAlert drops an unfinished /alert confirm.
func (s *State) ClearPendingPriceAlert() {
	if s != nil {
		s.PendingPriceAlert = nil
	}
}

// RemovePriceAlert deletes watches matching query (condition id, slug, or title words).
// Empty query removes the only alert. Returns the removed row and whether it was unique.
func (s *State) RemovePriceAlert(query string) (PriceAlert, bool) {
	if s == nil || len(s.PriceAlerts) == 0 {
		return PriceAlert{}, false
	}
	query = strings.TrimSpace(query)
	if query == "" {
		if len(s.PriceAlerts) == 1 {
			got := s.PriceAlerts[0]
			s.PriceAlerts = nil
			return got, true
		}
		return PriceAlert{}, false
	}
	hits := make([]int, 0, len(s.PriceAlerts))
	for i, a := range s.PriceAlerts {
		if priceAlertMatches(a, query) {
			hits = append(hits, i)
		}
	}
	if len(hits) != 1 {
		return PriceAlert{}, false
	}
	got := s.PriceAlerts[hits[0]]
	s.PriceAlerts = append(s.PriceAlerts[:hits[0]], s.PriceAlerts[hits[0]+1:]...)
	return got, true
}

func priceAlertMatches(a PriceAlert, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(a.ConditionID), query) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(a.Slug), query) {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(a.Title))
	if title == q || strings.Contains(title, q) {
		return true
	}
	toks := strings.Fields(q)
	if len(toks) == 0 {
		return false
	}
	for _, t := range toks {
		if !strings.Contains(title, t) && !strings.Contains(strings.ToLower(a.Slug), t) {
			return false
		}
	}
	return true
}

// ShouldFirePriceAlert is true when size meets the floor and the 1h cooldown has elapsed.
func ShouldFirePriceAlert(a PriceAlert, size float64, now time.Time) bool {
	if size+1e-9 < a.MinSize {
		return false
	}
	if a.LastNotifiedUnix <= 0 {
		return true
	}
	if now.IsZero() {
		now = time.Now()
	}
	return now.Unix()-a.LastNotifiedUnix >= int64(PriceAlertRepeat.Seconds())
}

// ApplyPriceAlertPoll writes notify stamps from a poll snapshot onto the live list.
func (s *State) ApplyPriceAlertPoll(evaluated []PriceAlert) {
	if s == nil || len(evaluated) == 0 || len(s.PriceAlerts) == 0 {
		return
	}
	upd := make(map[string]PriceAlert, len(evaluated))
	for _, a := range evaluated {
		id := strings.TrimSpace(a.ConditionID)
		if id != "" {
			upd[id] = a
		}
	}
	for i, cur := range s.PriceAlerts {
		n, ok := upd[strings.TrimSpace(cur.ConditionID)]
		if !ok {
			continue
		}
		if n.Price != cur.Price || n.MinSize != cur.MinSize {
			continue
		}
		s.PriceAlerts[i].LastNotifiedUnix = n.LastNotifiedUnix
		if oc := strings.TrimSpace(n.Outcome); oc != "" {
			s.PriceAlerts[i].Outcome = oc
		}
	}
}

func PriceAlertStillArmed(s *State, a PriceAlert) bool {
	if s == nil {
		return false
	}
	id := strings.TrimSpace(a.ConditionID)
	for _, cur := range s.PriceAlerts {
		if strings.TrimSpace(cur.ConditionID) != id {
			continue
		}
		return cur.Price == a.Price && cur.MinSize == a.MinSize
	}
	return false
}

// CheckPriceAlerts loads Yes books and returns pings that should go out now.
func CheckPriceAlerts(ctx context.Context, api yesBookAPI, alerts []PriceAlert, now time.Time) (fired []PriceAlertHit, next []PriceAlert) {
	if now.IsZero() {
		now = time.Now()
	}
	next = make([]PriceAlert, 0, len(alerts))
	for _, a := range alerts {
		book, err := api.FetchYesBook(ctx, a.ConditionID)
		if err != nil {
			next = append(next, a)
			continue
		}
		if oc := strings.TrimSpace(book.Outcome); oc != "" {
			a.Outcome = oc
		}
		size := polymarket.SizeAtOrBelow(book.Asks, a.Price)
		if ShouldFirePriceAlert(a, size, now) {
			a.LastNotifiedUnix = now.Unix()
			fired = append(fired, PriceAlertHit{Alert: a, Size: size})
		}
		next = append(next, a)
	}
	return fired, next
}

// ResolvePriceAlertMarket looks up an active market for /alert.
func ResolvePriceAlertMarket(ctx context.Context, api yesBookAPI, query string) (PriceAlertDraft, string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return PriceAlertDraft{}, "Usage: /alert <market> — then reply with ask price and min size."
	}
	hit, err := api.FindMarket(ctx, query)
	if err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "resolved") {
			return PriceAlertDraft{}, fmt.Sprintf("%q is resolved, not an active market.", query)
		}
		if strings.Contains(low, "active market") || strings.Contains(low, "no active") {
			return PriceAlertDraft{}, fmt.Sprintf("No active market matching %q.", query)
		}
		return PriceAlertDraft{}, fmt.Sprintf("Couldn't load market: %v", err)
	}
	if strings.TrimSpace(hit.Market.ConditionID) == "" {
		return PriceAlertDraft{}, fmt.Sprintf("No CLOB market matching %q.", query)
	}
	return DraftFromMarket(query, hit), ""
}
