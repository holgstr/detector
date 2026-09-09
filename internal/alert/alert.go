package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const maxSeen = 8000

// State is the on-disk checkpoint so restarts don't re-send old fills.
type State struct {
	Seeded         bool             `json:"seeded"`
	ChatID         int64            `json:"chat_id,omitempty"`
	TelegramOffset int64            `json:"telegram_offset,omitempty"`
	Seen           map[string]int64 `json:"seen"`
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
}

type sportsLookup interface {
	EventIsSports(ctx context.Context, eventSlug string) (bool, error)
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
}

// BuildPlan classifies unseen fills. Sports and dust are dropped; lookup
// failures are skipped so the next poll can retry.
func BuildPlan(ctx context.Context, api sportsLookup, s *State, acts []polymarket.Activity, minUSD float64) (Plan, error) {
	if !s.Seeded {
		s.Seed(acts)
		return Plan{FirstRun: true}, nil
	}

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
	for _, a := range acts {
		key := polymarket.ActivityKey(a)
		if s.known(key) {
			continue
		}
		sports, ok := isSports(a.EventSlug)
		if !ok {
			continue
		}
		if sports || a.USDCSize < minUSD {
			drop = append(drop, key)
			continue
		}
		keep = append(keep, a)
	}

	return Plan{
		Alerts:   aggregate(keep),
		DropKeys: drop,
	}, nil
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
func Format(a Alert) string {
	side := a.Side
	if side == "" {
		side = "TRADE"
	}
	outcome := a.Outcome
	if outcome == "" {
		outcome = "—"
	}
	fills := ""
	if a.Parts > 1 {
		fills = fmt.Sprintf(" (%d fills)", a.Parts)
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = a.Slug
	}
	if title == "" {
		title = "—"
	}
	return fmt.Sprintf("%s  %s  %s  %s  @  %s%s\n%s\n%s",
		a.Name, side, outcome, formatUSD(a.USDC), formatCents(a.Price), fills,
		title,
		marketURL(a),
	)
}

func marketURL(a Alert) string {
	switch {
	case a.EventSlug != "" && a.Slug != "":
		return "https://polymarket.com/event/" + a.EventSlug + "/" + a.Slug
	case a.EventSlug != "":
		return "https://polymarket.com/event/" + a.EventSlug
	case a.Slug != "":
		return "https://polymarket.com/market/" + a.Slug
	case a.Wallet != "":
		return "https://polymarket.com/profile/" + a.Wallet
	default:
		return "https://polymarket.com"
	}
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
	return fmt.Sprintf("%.0f¢", p*100)
}
