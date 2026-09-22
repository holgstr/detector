package alert

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

// PriceWatch pings when one outcome's price moves by Delta from Anchor,
// then re-anchors at the new price. A move is a new midpoint, an inside
// bid or ask, or a fill at least Delta away from the anchor.
type PriceWatch struct {
	Title       string   `json:"title,omitempty"`
	Slug        string   `json:"slug,omitempty"`
	URL         string   `json:"url,omitempty"`
	ConditionID string   `json:"condition_id"`
	Outcome     string   `json:"outcome"`
	Delta       float64  `json:"delta"`
	Anchor      float64  `json:"anchor"`
	SetUnix     int64    `json:"set_unix,omitempty"`
	HasBid      bool     `json:"has_bid,omitempty"`
	Bid         float64  `json:"bid,omitempty"`
	HasAsk      bool     `json:"has_ask,omitempty"`
	Ask         float64  `json:"ask,omitempty"`
	HasMid      bool     `json:"has_mid,omitempty"`
	Mid         float64  `json:"mid,omitempty"`
	BidLatched  bool     `json:"bid_latched,omitempty"`
	AskLatched  bool     `json:"ask_latched,omitempty"`
	MidLatched  bool     `json:"mid_latched,omitempty"`
	TradeUnix   int64    `json:"trade_unix,omitempty"`
	TradeKeys   []string `json:"trade_keys,omitempty"`
}

// PriceWatchMove is one observation that crossed the band.
type PriceWatchMove struct {
	Kind  string
	Price float64
	Size  float64
	Side  string
}

// PriceWatchHit is one ping from a price watch poll.
type PriceWatchHit struct {
	Watch PriceWatch
	From  float64
	To    float64
	Moves []PriceWatchMove
}

// WatchFill is a tape print used to evaluate a price watch.
type WatchFill struct {
	Key       string
	Price     float64
	Size      float64
	Side      string
	Timestamp int64
}

type priceWatchAPI interface {
	FetchOutcomeBook(ctx context.Context, conditionID, outcome string) (polymarket.OutcomeBook, error)
	FetchTrades(ctx context.Context, opt polymarket.FetchTradesOptions) ([]polymarket.Trade, bool, error)
}

// NewPriceWatch anchors a watch on the current midpoint (or the only inside quote).
func NewPriceWatch(draft PriceAlertDraft, outcome string, delta float64, book polymarket.OutcomeBook, now time.Time) (PriceWatch, string) {
	outcome = strings.TrimSpace(outcome)
	if outcome == "" || delta <= 0 || delta >= 1 {
		return PriceWatch{}, "Usage: /pricewatch <market> <YES|NO> <cents> — e.g. /pricewatch Merz December NO 3"
	}
	if strings.TrimSpace(draft.ConditionID) == "" {
		return PriceWatch{}, "No CLOB market for that query."
	}
	bid, ask, _, _, hasBid, hasAsk := insideQuote(book)
	mid, hasMid := watchMid(hasBid, hasAsk, bid, ask)
	anchor, ok := watchAnchor(hasBid, hasAsk, hasMid, bid, ask, mid)
	if !ok {
		return PriceWatch{}, fmt.Sprintf("No bid or ask on %s.", outcome)
	}
	if now.IsZero() {
		now = time.Now()
	}
	w := PriceWatch{
		Title:       strings.TrimSpace(draft.Title),
		Slug:        draft.Slug,
		URL:         draft.URL,
		ConditionID: strings.TrimSpace(draft.ConditionID),
		Outcome:     outcome,
		Delta:       delta,
		Anchor:      anchor,
		SetUnix:     now.Unix(),
		HasBid:      hasBid,
		Bid:         bid,
		HasAsk:      hasAsk,
		Ask:         ask,
		HasMid:      hasMid,
		Mid:         mid,
		TradeUnix:   now.Unix(),
	}
	w.BidLatched = hasBid && priceMoved(bid, anchor, delta)
	w.AskLatched = hasAsk && priceMoved(ask, anchor, delta)
	w.MidLatched = hasMid && priceMoved(mid, anchor, delta)
	return w, ""
}

// PriceWatchSetText confirms a live watch.
func PriceWatchSetText(w PriceWatch) string {
	title := priceWatchTitle(w)
	side := strings.ToUpper(strings.TrimSpace(w.Outcome))
	var b strings.Builder
	fmt.Fprintf(&b, "Price watch · %s %s\n", title, side)
	fmt.Fprintf(&b, "Anchored at %s. Ping on a %s move (%s or %s) — a fill, or a bid/ask — then re-anchor.",
		formatWatchPrice(w.Anchor),
		formatWatchCents(w.Delta),
		formatWatchPrice(w.Anchor-w.Delta),
		formatWatchPrice(w.Anchor+w.Delta),
	)
	if u := strings.TrimSpace(w.URL); u != "" {
		b.WriteByte('\n')
		b.WriteString(u)
	}
	b.WriteString("\n/unpricewatch ")
	b.WriteString(title)
	b.WriteString(" to stop.")
	return b.String()
}

// PriceWatchPingText is the Telegram body for one meaningful move.
func PriceWatchPingText(h PriceWatchHit) string {
	w := h.Watch
	title := priceWatchTitle(w)
	side := strings.ToUpper(strings.TrimSpace(w.Outcome))
	sign := "+"
	if h.To < h.From {
		sign = "-"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s · %s\n", side, formatWatchPrice(h.To), title)
	fmt.Fprintf(&b, "%s%s from %s", sign, formatWatchCents(math.Abs(h.To-h.From)), formatWatchPrice(h.From))
	if via := moveVia(h.Moves); via != "" {
		b.WriteString(" via ")
		b.WriteString(via)
	}
	b.WriteByte('\n')
	b.WriteString(formatWatchQuote(w))
	fmt.Fprintf(&b, "\nnext %s or %s", formatWatchPrice(h.To-w.Delta), formatWatchPrice(h.To+w.Delta))
	nFill := 0
	for _, m := range h.Moves {
		if m.Kind == "fill" {
			nFill++
		}
	}
	shown := 0
	for _, m := range h.Moves {
		if m.Kind != "fill" {
			continue
		}
		if shown == 3 {
			fmt.Fprintf(&b, "\n+%d more fills", nFill-shown)
			break
		}
		side := strings.ToUpper(strings.TrimSpace(m.Side))
		if side == "" {
			side = "FILL"
		}
		fmt.Fprintf(&b, "\n%s %s @ %s", side, formatShares(m.Size), formatWatchPrice(m.Price))
		shown++
	}
	return b.String()
}

// FormatPriceWatchList is /pricewatch with no args.
func FormatPriceWatchList(watches []PriceWatch) string {
	if len(watches) == 0 {
		return "No price watches. /pricewatch <market> <YES|NO> <cents> — e.g. /pricewatch Merz December NO 3"
	}
	var b strings.Builder
	b.WriteString("Price watches:")
	for _, w := range watches {
		fmt.Fprintf(&b, "\n%s %s — %s ± %s", priceWatchTitle(w), strings.ToUpper(w.Outcome), formatWatchPrice(w.Anchor), formatWatchCents(w.Delta))
	}
	b.WriteString("\n/unpricewatch <market> to stop one. /pricewatch <market> <YES|NO> <cents> to add.")
	return b.String()
}

// UpsertPriceWatch replaces a watch on the same condition id and outcome.
func (s *State) UpsertPriceWatch(w PriceWatch) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(w.ConditionID)
	side := strings.ToLower(strings.TrimSpace(w.Outcome))
	kept := s.PriceWatches[:0]
	for _, old := range s.PriceWatches {
		if strings.TrimSpace(old.ConditionID) == id && strings.ToLower(strings.TrimSpace(old.Outcome)) == side {
			continue
		}
		kept = append(kept, old)
	}
	s.PriceWatches = append(kept, w)
}

// RemovePriceWatch deletes the watch matching query. Empty query removes the
// only watch. Returns the removed row and whether the match was unique.
func (s *State) RemovePriceWatch(query string) (PriceWatch, bool) {
	if s == nil || len(s.PriceWatches) == 0 {
		return PriceWatch{}, false
	}
	query = strings.TrimSpace(query)
	if query == "" {
		if len(s.PriceWatches) == 1 {
			got := s.PriceWatches[0]
			s.PriceWatches = nil
			return got, true
		}
		return PriceWatch{}, false
	}
	hits := make([]int, 0, len(s.PriceWatches))
	for i, w := range s.PriceWatches {
		if priceWatchMatches(w, query) {
			hits = append(hits, i)
		}
	}
	if len(hits) != 1 {
		return PriceWatch{}, false
	}
	got := s.PriceWatches[hits[0]]
	s.PriceWatches = append(s.PriceWatches[:hits[0]], s.PriceWatches[hits[0]+1:]...)
	return got, true
}

// ApplyPriceWatchPoll writes quote snapshots and anchors from a poll.
// A watch replaced since the poll started (different SetUnix) is left alone.
func (s *State) ApplyPriceWatchPoll(evaluated []PriceWatch) {
	if s == nil || len(evaluated) == 0 || len(s.PriceWatches) == 0 {
		return
	}
	type key struct {
		id, side string
		set      int64
	}
	upd := make(map[key]PriceWatch, len(evaluated))
	for _, w := range evaluated {
		id := strings.TrimSpace(w.ConditionID)
		if id == "" {
			continue
		}
		upd[key{id, strings.ToLower(strings.TrimSpace(w.Outcome)), w.SetUnix}] = w
	}
	for i, cur := range s.PriceWatches {
		n, ok := upd[key{strings.TrimSpace(cur.ConditionID), strings.ToLower(strings.TrimSpace(cur.Outcome)), cur.SetUnix}]
		if !ok || n.Delta != cur.Delta {
			continue
		}
		s.PriceWatches[i] = n
	}
}

// PriceWatchStillArmed reports whether this ping's watch is still the live one.
func PriceWatchStillArmed(s *State, w PriceWatch) bool {
	if s == nil {
		return false
	}
	id := strings.TrimSpace(w.ConditionID)
	side := strings.ToLower(strings.TrimSpace(w.Outcome))
	for _, cur := range s.PriceWatches {
		if strings.TrimSpace(cur.ConditionID) != id || strings.ToLower(strings.TrimSpace(cur.Outcome)) != side {
			continue
		}
		return cur.SetUnix == w.SetUnix && cur.Delta == w.Delta && samePrice(cur.Anchor, w.Anchor)
	}
	return false
}

// EvaluatePriceWatch compares a book and new fills with the anchor.
// Quotes already outside the band stay quiet until they come back inside.
func EvaluatePriceWatch(w PriceWatch, book polymarket.OutcomeBook, fills []WatchFill) (*PriceWatchHit, PriceWatch) {
	next := w
	next.TradeKeys = append([]string(nil), w.TradeKeys...)
	bid, ask, bidSz, askSz, hasBid, hasAsk := insideQuote(book)
	mid, hasMid := watchMid(hasBid, hasAsk, bid, ask)
	fresh := noteWatchFills(&next, fills)

	var moves []PriceWatchMove
	if hasMid && !w.MidLatched && priceMoved(mid, w.Anchor, w.Delta) && quoteChanged(w.HasMid, w.Mid, true, mid) {
		moves = append(moves, PriceWatchMove{Kind: "mid", Price: mid})
	}
	if hasBid && !w.BidLatched && priceMoved(bid, w.Anchor, w.Delta) && quoteChanged(w.HasBid, w.Bid, true, bid) {
		moves = append(moves, PriceWatchMove{Kind: "bid", Price: bid, Size: bidSz})
	}
	if hasAsk && !w.AskLatched && priceMoved(ask, w.Anchor, w.Delta) && quoteChanged(w.HasAsk, w.Ask, true, ask) {
		moves = append(moves, PriceWatchMove{Kind: "ask", Price: ask, Size: askSz})
	}
	for _, f := range fresh {
		if priceMoved(f.Price, w.Anchor, w.Delta) {
			moves = append(moves, PriceWatchMove{Kind: "fill", Price: f.Price, Size: f.Size, Side: f.Side})
		}
	}

	anchor := w.Anchor
	var hit *PriceWatchHit
	if len(moves) > 0 {
		to := pickWatchAnchor(w.Anchor, w.Delta, mid, moves)
		anchor = to
		updated := next
		updated.Anchor = to
		stampWatchQuote(&updated, bid, ask, mid, hasBid, hasAsk, hasMid, anchor)
		hit = &PriceWatchHit{Watch: updated, From: w.Anchor, To: to, Moves: moves}
		next = updated
		return hit, next
	}
	stampWatchQuote(&next, bid, ask, mid, hasBid, hasAsk, hasMid, anchor)
	return nil, next
}

// CheckPriceWatches loads books and recent fills and returns pings.
func CheckPriceWatches(ctx context.Context, api priceWatchAPI, watches []PriceWatch, now time.Time) (fired []PriceWatchHit, next []PriceWatch) {
	if now.IsZero() {
		now = time.Now()
	}
	next = make([]PriceWatch, 0, len(watches))
	for _, w := range watches {
		book, err := api.FetchOutcomeBook(ctx, w.ConditionID, w.Outcome)
		if err != nil {
			next = append(next, w)
			continue
		}
		var fills []WatchFill
		start := w.TradeUnix
		if start <= 0 {
			start = now.Add(-time.Minute).Unix()
		}
		trades, _, err := api.FetchTrades(ctx, polymarket.FetchTradesOptions{
			Market:   w.ConditionID,
			Start:    start,
			PageSize: 100,
		})
		if err == nil {
			fills = watchFillsFromTrades(w.Outcome, trades)
		}
		hit, updated := EvaluatePriceWatch(w, book, fills)
		if hit != nil {
			fired = append(fired, *hit)
		}
		next = append(next, updated)
	}
	return fired, next
}

func watchFillsFromTrades(outcome string, trades []polymarket.Trade) []WatchFill {
	out := make([]WatchFill, 0, len(trades))
	for _, t := range trades {
		if !strings.EqualFold(strings.TrimSpace(t.Outcome), outcome) {
			continue
		}
		if t.Price <= 0 {
			continue
		}
		out = append(out, WatchFill{
			Key:       watchFillKey(t),
			Price:     t.Price,
			Size:      t.Size,
			Side:      t.Side,
			Timestamp: t.Timestamp,
		})
	}
	return out
}

func watchFillKey(t polymarket.Trade) string {
	return t.TransactionHash + "|" + strings.ToLower(t.ProxyWallet) + "|" + t.Asset + "|" +
		strconv.FormatInt(t.Timestamp, 10) + "|" + strconv.FormatFloat(t.Size, 'f', -1, 64)
}

func noteWatchFills(w *PriceWatch, fills []WatchFill) []WatchFill {
	seen := make(map[string]struct{}, len(w.TradeKeys))
	for _, k := range w.TradeKeys {
		if k != "" {
			seen[k] = struct{}{}
		}
	}
	fresh := make([]WatchFill, 0, len(fills))
	for _, f := range fills {
		if f.Key == "" || f.Timestamp <= 0 {
			continue
		}
		if f.Timestamp < w.TradeUnix {
			continue
		}
		if f.Timestamp == w.TradeUnix {
			if _, ok := seen[f.Key]; ok {
				continue
			}
		}
		fresh = append(fresh, f)
	}
	maxTs := w.TradeUnix
	for _, f := range fresh {
		if f.Timestamp > maxTs {
			maxTs = f.Timestamp
		}
	}
	keys := make([]string, 0, len(w.TradeKeys)+len(fresh))
	if maxTs == w.TradeUnix {
		keys = append(keys, w.TradeKeys...)
	}
	have := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		have[k] = struct{}{}
	}
	add := func(k string, ts int64) {
		if k == "" || ts != maxTs {
			return
		}
		if _, ok := have[k]; ok {
			return
		}
		have[k] = struct{}{}
		keys = append(keys, k)
	}
	for _, f := range fills {
		add(f.Key, f.Timestamp)
	}
	for _, f := range fresh {
		add(f.Key, f.Timestamp)
	}
	w.TradeUnix = maxTs
	w.TradeKeys = keys
	return fresh
}

// pickWatchAnchor prefers the new midpoint. A fill or quote takes over only
// when the midpoint did not cross, or when that print is another full delta
// past the new midpoint.
func pickWatchAnchor(anchor, delta, mid float64, moves []PriceWatchMove) float64 {
	to := 0.0
	have := false
	midCrossed := false
	for _, m := range moves {
		if m.Kind == "mid" {
			to = m.Price
			have = true
			midCrossed = true
			break
		}
	}
	for _, m := range moves {
		if m.Kind == "mid" {
			continue
		}
		if !have || math.Abs(m.Price-anchor) > math.Abs(to-anchor)+1e-12 {
			if !midCrossed || math.Abs(m.Price-mid)+1e-9 >= delta {
				to = m.Price
				have = true
			}
		}
	}
	if !have {
		return anchor
	}
	return to
}

func stampWatchQuote(w *PriceWatch, bid, ask, mid float64, hasBid, hasAsk, hasMid bool, anchor float64) {
	w.HasBid, w.Bid = hasBid, bid
	w.HasAsk, w.Ask = hasAsk, ask
	w.HasMid, w.Mid = hasMid, mid
	w.BidLatched = hasBid && priceMoved(bid, anchor, w.Delta) && !samePrice(bid, anchor)
	w.AskLatched = hasAsk && priceMoved(ask, anchor, w.Delta) && !samePrice(ask, anchor)
	w.MidLatched = hasMid && priceMoved(mid, anchor, w.Delta) && !samePrice(mid, anchor)
}

func insideQuote(book polymarket.OutcomeBook) (bid, ask, bidSz, askSz float64, hasBid, hasAsk bool) {
	for _, lv := range book.Bids {
		if lv.Size <= 0 || lv.Price <= 0 {
			continue
		}
		if !hasBid || lv.Price > bid+1e-12 {
			bid, bidSz, hasBid = lv.Price, lv.Size, true
			continue
		}
		if samePrice(lv.Price, bid) {
			bidSz += lv.Size
		}
	}
	for _, lv := range book.Asks {
		if lv.Size <= 0 || lv.Price <= 0 || lv.Price >= 1 {
			continue
		}
		if !hasAsk || lv.Price < ask-1e-12 {
			ask, askSz, hasAsk = lv.Price, lv.Size, true
			continue
		}
		if samePrice(lv.Price, ask) {
			askSz += lv.Size
		}
	}
	return bid, ask, bidSz, askSz, hasBid, hasAsk
}

func watchMid(hasBid, hasAsk bool, bid, ask float64) (float64, bool) {
	if hasBid && hasAsk && ask+1e-12 >= bid {
		return (bid + ask) / 2, true
	}
	return 0, false
}

func watchAnchor(hasBid, hasAsk, hasMid bool, bid, ask, mid float64) (float64, bool) {
	if hasMid {
		return mid, true
	}
	if hasBid {
		return bid, true
	}
	if hasAsk {
		return ask, true
	}
	return 0, false
}

func quoteChanged(had bool, prev float64, has bool, cur float64) bool {
	if had != has {
		return true
	}
	if !has {
		return false
	}
	return !samePrice(prev, cur)
}

func priceMoved(price, anchor, delta float64) bool {
	if delta <= 0 || price <= 0 {
		return false
	}
	return math.Abs(price-anchor)+1e-9 >= delta
}

func samePrice(a, b float64) bool {
	return math.Abs(a-b) < 5e-5
}

func priceWatchTitle(w PriceWatch) string {
	title := strings.TrimSpace(w.Title)
	if title == "" {
		title = strings.TrimSpace(w.Slug)
	}
	if title == "" {
		title = "market"
	}
	return title
}

func priceWatchMatches(w PriceWatch, query string) bool {
	market, outcome := splitWatchQuery(query)
	if outcome != "" && !strings.EqualFold(w.Outcome, outcome) {
		return false
	}
	q := strings.ToLower(strings.TrimSpace(market))
	if q == "" {
		return outcome != ""
	}
	if strings.EqualFold(strings.TrimSpace(w.ConditionID), market) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(w.Slug), market) {
		return true
	}
	title := strings.ToLower(strings.TrimSpace(w.Title))
	slug := strings.ToLower(w.Slug)
	if title == q || strings.Contains(title, q) || slug == q {
		return true
	}
	toks := strings.Fields(q)
	if len(toks) == 0 {
		return false
	}
	for _, t := range toks {
		if !strings.Contains(title, t) && !strings.Contains(slug, t) {
			return false
		}
	}
	return true
}

func splitWatchQuery(query string) (market, outcome string) {
	fields := strings.Fields(strings.TrimSpace(query))
	if len(fields) == 0 {
		return "", ""
	}
	if side, ok := parseSide(fields[len(fields)-1]); ok {
		return strings.TrimSpace(strings.Join(fields[:len(fields)-1], " ")), side
	}
	if side, ok := parseSide(fields[0]); ok {
		return strings.TrimSpace(strings.Join(fields[1:], " ")), side
	}
	return strings.TrimSpace(query), ""
}

func moveVia(moves []PriceWatchMove) string {
	order := []string{"mid", "bid", "ask", "fill"}
	seen := map[string]bool{}
	var parts []string
	for _, kind := range order {
		for _, m := range moves {
			if m.Kind == kind && !seen[kind] {
				seen[kind] = true
				parts = append(parts, kind)
			}
		}
	}
	return strings.Join(parts, ", ")
}

func formatWatchQuote(w PriceWatch) string {
	bid, ask := "—", "—"
	if w.HasBid {
		bid = formatWatchPrice(w.Bid)
	}
	if w.HasAsk {
		ask = formatWatchPrice(w.Ask)
	}
	if w.HasMid {
		return fmt.Sprintf("mid %s · bid %s · ask %s", formatWatchPrice(w.Mid), bid, ask)
	}
	return fmt.Sprintf("bid %s · ask %s", bid, ask)
}

func formatWatchPrice(p float64) string {
	cents := p * 100
	rounded := math.Round(cents*10) / 10
	if math.Abs(rounded-math.Round(rounded)) < 0.05 {
		return fmt.Sprintf("%.0f¢", math.Round(rounded))
	}
	return fmt.Sprintf("%.1f¢", rounded)
}

func formatWatchCents(p float64) string {
	return formatWatchPrice(p)
}
