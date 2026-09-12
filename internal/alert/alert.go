package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const (
	maxSeen = 8000
	// MaxAlertAge is how recent a fill must be to page Telegram. The Data API
	// /activity page is the last ~100 trades per wallet, which often still
	// includes yesterday. After a checkpoint miss those rows look "unseen"
	// and would otherwise dump as live alerts.
	MaxAlertAge = 30 * time.Minute
)

// State is the on-disk checkpoint so restarts don't re-send old fills.
type State struct {
	Seeded         bool  `json:"seeded"`
	ChatID         int64 `json:"chat_id,omitempty"`
	TelegramOffset int64 `json:"telegram_offset,omitempty"`
	// MinUSD is the live chat-controlled floor (Activity tab "Min size $").
	// Nil means use the process default (flag / env).
	MinUSD *float64         `json:"min_usd,omitempty"`
	Seen   map[string]int64 `json:"seen"`
}

// EffectiveMinUSD is the chat override if set, otherwise fallback.
func (s *State) EffectiveMinUSD(fallback float64) float64 {
	if s != nil && s.MinUSD != nil {
		return *s.MinUSD
	}
	return fallback
}

// SetMinUSD persists a chat minsize (including 0 = show everything).
func (s *State) SetMinUSD(v float64) {
	if v < 0 {
		v = 0
	}
	s.MinUSD = &v
}

// Alert is one Telegram-ready (possibly aggregated) trade.
type Alert struct {
	Wallet      string
	Name        string
	Side        string
	Outcome     string
	Title       string
	Slug        string
	EventSlug   string
	ConditionID string
	Size        float64
	Price       float64
	USDC        float64
	Timestamp   int64
	Parts       int
	Keys        []string
	// Net position after the fill (YES minus NO, or a non-binary outcome).
	HasPosition     bool
	PositionSize    float64
	PositionOutcome string
}

type sportsLookup interface {
	EventIsSports(ctx context.Context, eventSlug string) (bool, error)
}

type positionLookup interface {
	FetchPositions(ctx context.Context, opt polymarket.FetchPositionsOptions) ([]polymarket.Position, error)
}

// LoadState reads a checkpoint, or returns an empty one if the file is missing.
func LoadState(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Seen: make(map[string]int64)}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s.Seen == nil {
		s.Seen = make(map[string]int64)
	}
	return &s, nil
}

// SaveState writes the checkpoint atomically.
func SaveState(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	s.prune()
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *State) prune() {
	if len(s.Seen) <= maxSeen {
		return
	}
	type kv struct {
		k string
		t int64
	}
	all := make([]kv, 0, len(s.Seen))
	for k, t := range s.Seen {
		all = append(all, kv{k, t})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].t > all[j].t })
	s.Seen = make(map[string]int64, maxSeen)
	for i := 0; i < maxSeen; i++ {
		s.Seen[all[i].k] = all[i].t
	}
}

func (s *State) mark(keys []string, now int64) {
	if s.Seen == nil {
		s.Seen = make(map[string]int64)
	}
	if now == 0 {
		now = time.Now().Unix()
	}
	for _, k := range keys {
		if k == "" {
			continue
		}
		s.Seen[k] = now
	}
}

func (s *State) known(key string) bool {
	_, ok := s.Seen[key]
	return ok
}

// Seed records the current feed as already-seen so the first poll is quiet.
func (s *State) Seed(acts []polymarket.Activity) {
	keys := make([]string, 0, len(acts))
	for _, a := range acts {
		keys = append(keys, polymarket.ActivityKey(a))
	}
	s.mark(keys, 0)
	s.Seeded = true
}

// Plan is the work for one poll: alerts to send, plus keys that can be
// dropped immediately (sports / dust). Unclassified rows are left unseen.
type Plan struct {
	FirstRun bool
	Alerts   []Alert
	DropKeys []string
	Stale    int
}

// BuildPlan classifies unseen fills. Sports and fills older than
// MaxAlertAge are dropped; lookup failures are skipped so the next poll
// can retry. minUSD is applied after aggregating same-wallet/market/
// side/outcome fills, so two $40+$70 BUYs become one $110 alert and pass
// a $100 floor. Sub-floor aggregates stay unseen until later fills push
// them over — unless they age out first.
func BuildPlan(ctx context.Context, api sportsLookup, s *State, acts []polymarket.Activity, minUSD float64) (Plan, error) {
	return BuildPlanAt(ctx, api, s, acts, minUSD, time.Now())
}

// BuildPlanAt is BuildPlan with a frozen clock (tests).
func BuildPlanAt(ctx context.Context, api sportsLookup, s *State, acts []polymarket.Activity, minUSD float64, now time.Time) (Plan, error) {
	if !s.Seeded {
		s.Seed(acts)
		return Plan{FirstRun: true}, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-MaxAlertAge).Unix()

	sportsCache := make(map[string]bool)
	failed := make(map[string]struct{})
	isSports := func(slug string) (bool, bool) {
		if slug == "" {
			return false, true
		}
		if _, ok := failed[slug]; ok {
			return false, false
		}
		if v, ok := sportsCache[slug]; ok {
			return v, true
		}
		v, err := api.EventIsSports(ctx, slug)
		if err != nil {
			failed[slug] = struct{}{}
			return false, false
		}
		sportsCache[slug] = v
		return v, true
	}

	var drop []string
	var keep []polymarket.Activity
	stale := 0
	for _, a := range acts {
		key := polymarket.ActivityKey(a)
		if s.known(key) {
			continue
		}
		if activityUnix(a.Timestamp) < cutoff {
			drop = append(drop, key)
			stale++
			continue
		}
		sports, ok := isSports(a.EventSlug)
		if !ok {
			continue
		}
		if sports {
			drop = append(drop, key)
			continue
		}
		keep = append(keep, a)
	}

	return Plan{
		Alerts:   filterMinUSD(aggregate(keep), minUSD),
		DropKeys: drop,
		Stale:    stale,
	}, nil
}

func filterMinUSD(alerts []Alert, minUSD float64) []Alert {
	if minUSD <= 0 || len(alerts) == 0 {
		return alerts
	}
	out := make([]Alert, 0, len(alerts))
	for _, a := range alerts {
		if a.USDC >= minUSD {
			out = append(out, a)
		}
	}
	return out
}

// CommitDropped marks sports/dust so they aren't reconsidered.
func (s *State) CommitDropped(p Plan) {
	s.mark(p.DropKeys, 0)
}

// CommitSent marks fills that were successfully delivered.
func (s *State) CommitSent(a Alert) {
	s.mark(a.Keys, 0)
}

func aggregate(acts []polymarket.Activity) []Alert {
	grouped := make(map[string]*Alert)
	for _, a := range acts {
		gk := groupKey(a)
		row := grouped[gk]
		if row == nil {
			row = &Alert{
				Wallet:      strings.ToLower(a.ProxyWallet),
				Name:        displayName(a),
				Side:        strings.ToUpper(a.Side),
				Outcome:     a.Outcome,
				Title:       a.Title,
				Slug:        a.Slug,
				EventSlug:   a.EventSlug,
				ConditionID: a.ConditionID,
				Timestamp:   a.Timestamp,
			}
			grouped[gk] = row
		}
		sizeA, sizeB := row.Size, a.Size
		total := sizeA + sizeB
		if total > 0 {
			row.Price = (sizeA*row.Price + sizeB*a.Price) / total
		}
		row.Size = total
		row.USDC += a.USDCSize
		row.Parts++
		row.Keys = append(row.Keys, polymarket.ActivityKey(a))
		if a.Timestamp > row.Timestamp {
			row.Timestamp = a.Timestamp
		}
		if row.Title == "" {
			row.Title = a.Title
		}
	}

	out := make([]Alert, 0, len(grouped))
	for _, row := range grouped {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp != out[j].Timestamp {
			return out[i].Timestamp < out[j].Timestamp
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// activityUnix is seconds. The Data API usually sends epoch seconds;
// millisecond values would look "in the future" and bypass the age filter.
func activityUnix(ts int64) int64 {
	if ts > 1e12 {
		return ts / 1000
	}
	return ts
}

func groupKey(a polymarket.Activity) string {
	return strings.ToLower(a.ProxyWallet) + "|" + a.ConditionID + "|" +
		strings.ToUpper(a.Side) + "|" + strings.ToLower(strings.TrimSpace(a.Outcome))
}

func displayName(a polymarket.Activity) string {
	if n := sharps.NameOf(a.ProxyWallet); n != "" {
		return n
	}
	name := strings.TrimSpace(a.Name)
	if name != "" && !strings.HasPrefix(strings.ToLower(name), "0x") {
		return name
	}
	if p := strings.TrimSpace(a.Pseudonym); p != "" {
		return p
	}
	if name != "" {
		return name
	}
	w := a.ProxyWallet
	if len(w) >= 12 {
		return w[:6] + "…" + w[len(w)-4:]
	}
	return w
}

// Format is the Telegram (and dry-run) body for one alert.
//
//	Name BUY NO 32k @ 32c
//	Market Name
//	Position: 27.5k YES
func Format(a Alert) string {
	side := a.Side
	if side == "" {
		side = "TRADE"
	}
	outcome := strings.TrimSpace(a.Outcome)
	if outcome == "" {
		outcome = "—"
	} else {
		outcome = strings.ToUpper(outcome)
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = a.Slug
	}
	if title == "" {
		title = "—"
	}
	body := fmt.Sprintf("%s %s %s %s @ %s\n%s",
		a.Name, side, outcome, formatShares(a.Size), formatCents(a.Price),
		title,
	)
	if a.HasPosition {
		body += "\n" + formatPosition(a.PositionSize, a.PositionOutcome)
	}
	return body
}

func formatPosition(size float64, outcome string) string {
	if size == 0 || strings.TrimSpace(outcome) == "" {
		return "Position: 0"
	}
	return fmt.Sprintf("Position: %s %s", formatShares(size), strings.ToUpper(strings.TrimSpace(outcome)))
}

// AttachNetPositions fills each alert's net Yes/No (or other) holding for
// that wallet+market. Lookup failures leave HasPosition false so the trade
// line still goes out without a Position row.
func AttachNetPositions(ctx context.Context, api positionLookup, alerts []Alert) {
	if api == nil || len(alerts) == 0 {
		return
	}
	type key struct{ wallet, market string }
	cache := make(map[key]polymarket.NetPosition)
	failed := make(map[key]struct{})
	for i := range alerts {
		k := key{
			wallet: strings.ToLower(strings.TrimSpace(alerts[i].Wallet)),
			market: strings.TrimSpace(alerts[i].ConditionID),
		}
		if k.wallet == "" || k.market == "" {
			continue
		}
		if _, ok := failed[k]; ok {
			continue
		}
		np, ok := cache[k]
		if !ok {
			pos, err := api.FetchPositions(ctx, polymarket.FetchPositionsOptions{
				User:   k.wallet,
				Market: k.market,
			})
			if err != nil {
				failed[k] = struct{}{}
				continue
			}
			np = polymarket.NetShares(pos)
			cache[k] = np
		}
		if !np.Known {
			continue
		}
		alerts[i].HasPosition = true
		alerts[i].PositionSize = np.Size
		alerts[i].PositionOutcome = np.Outcome
	}
}

// formatShares is Activity-tab style: 1.4k / 32k above 1000, else a short raw count.
func formatShares(n float64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	if n >= 1000 {
		k := math.Round(n/100) / 10 // one decimal of thousands
		if k == math.Trunc(k) {
			return fmt.Sprintf("%s%.0fk", sign, k)
		}
		return fmt.Sprintf("%s%.1fk", sign, k)
	}
	if n >= 10 {
		return fmt.Sprintf("%s%.0f", sign, n)
	}
	return fmt.Sprintf("%s%.1f", sign, n)
}

func formatUSD(n float64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%s$%.2fM", sign, n/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%s$%.1fk", sign, n/1e3)
	case n >= 10:
		return fmt.Sprintf("%s$%.0f", sign, n)
	default:
		return fmt.Sprintf("%s$%.2f", sign, n)
	}
}

func formatCents(p float64) string {
	return fmt.Sprintf("%.0fc", p*100)
}
