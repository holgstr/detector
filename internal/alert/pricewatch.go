package alert

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/holgstr/detector/internal/polymarket"
)

// PriceWatch pings when the watched price moves by Delta from Anchor,
// then re-anchors at the new price. The watched price is the midpoint
// when both sides are quoted, otherwise the only inside quote. A fill
// or a wider spread that leaves that price inside the band is not a move.
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
	Book  polymarket.OutcomeBook
}

type priceWatchAPI interface {
	FetchOutcomeBook(ctx context.Context, conditionID, outcome string) (polymarket.OutcomeBook, error)
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
	fmt.Fprintf(&b, "%s %s\n%s · %s or %s",
		title, side,
		formatWatchPrice(w.Anchor),
		formatWatchPrice(w.Anchor-w.Delta),
		formatWatchPrice(w.Anchor+w.Delta),
	)
	if u := strings.TrimSpace(w.URL); u != "" {
		b.WriteByte('\n')
		b.WriteString(u)
	}
	return b.String()
}

// PriceWatchPingText is the Telegram body for one meaningful move.
// It leads with the market name and the same ladder /ob prints.
func PriceWatchPingText(h PriceWatchHit) string {
	return FormatOBReport(OBReport{
		Title: priceWatchTitle(h.Watch),
		Books: []polymarket.OutcomeBook{bookForDisplay(h.Book)},
	})
}

// FormatPriceWatchList is /pricewatch with no args.
func FormatPriceWatchList(watches []PriceWatch) string {
	if len(watches) == 0 {
		return "No price watches."
	}
	var b strings.Builder
	for i, w := range watches {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s %s — %s ± %s", priceWatchTitle(w), strings.ToUpper(w.Outcome), formatWatchPrice(w.Anchor), formatWatchCents(w.Delta))
	}
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

// EvaluatePriceWatch compares the watched price with the anchor.
// Quotes already outside the band stay quiet until they come back inside.
// A book that drops a side and then reprints the same prices is not a move.
func EvaluatePriceWatch(w PriceWatch, book polymarket.OutcomeBook) (*PriceWatchHit, PriceWatch) {
	next := w
	bid, ask, bidSz, askSz, hasBid, hasAsk := insideQuote(book)
	mid, hasMid := watchMid(hasBid, hasAsk, bid, ask)

	var moves []PriceWatchMove
	if kind, price, size, ok := watchedPrice(hasBid, hasAsk, hasMid, bid, ask, bidSz, askSz, mid); ok &&
		priceMoved(price, w.Anchor, w.Delta) && watchedPriceChanged(w, kind, price) {
		moves = append(moves, PriceWatchMove{Kind: kind, Price: price, Size: size})
	}

	anchor := w.Anchor
	var hit *PriceWatchHit
	if len(moves) > 0 {
		to := pickWatchAnchor(w.Anchor, w.Delta, mid, moves)
		anchor = to
		updated := next
		updated.Anchor = to
		stampWatchQuote(&updated, bid, ask, mid, hasBid, hasAsk, hasMid, anchor)
		hit = &PriceWatchHit{Watch: updated, From: w.Anchor, To: to, Moves: moves, Book: book}
		next = updated
		return hit, next
	}
	stampWatchQuote(&next, bid, ask, mid, hasBid, hasAsk, hasMid, anchor)
	return nil, next
}

// CheckPriceWatches loads books and returns pings.
func CheckPriceWatches(ctx context.Context, api priceWatchAPI, watches []PriceWatch, _ time.Time) (fired []PriceWatchHit, next []PriceWatch) {
	next = make([]PriceWatch, 0, len(watches))
	for _, w := range watches {
		book, err := api.FetchOutcomeBook(ctx, w.ConditionID, w.Outcome)
		if err != nil {
			next = append(next, w)
			continue
		}
		hit, updated := EvaluatePriceWatch(w, book)
		if hit != nil {
			fired = append(fired, *hit)
		}
		next = append(next, updated)
	}
	return fired, next
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
	// Keep the last price when a side is missing so a gap cannot look like
	// a new quote when the same level comes back.
	if hasBid {
		w.Bid = bid
	}
	if hasAsk {
		w.Ask = ask
	}
	if hasMid {
		w.Mid = mid
	}
	w.HasBid, w.HasAsk, w.HasMid = hasBid, hasAsk, hasMid
	w.BidLatched = w.Bid > 0 && priceMoved(w.Bid, anchor, w.Delta) && !samePrice(w.Bid, anchor)
	w.AskLatched = w.Ask > 0 && priceMoved(w.Ask, anchor, w.Delta) && !samePrice(w.Ask, anchor)
	w.MidLatched = w.Mid > 0 && priceMoved(w.Mid, anchor, w.Delta) && !samePrice(w.Mid, anchor)
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

// watchedPrice is the midpoint, or the only inside quote when the other side is missing.
func watchedPrice(hasBid, hasAsk, hasMid bool, bid, ask, bidSz, askSz, mid float64) (kind string, price, size float64, ok bool) {
	if hasMid {
		return "mid", mid, 0, true
	}
	if hasBid && !hasAsk {
		return "bid", bid, bidSz, true
	}
	if hasAsk && !hasBid {
		return "ask", ask, askSz, true
	}
	return "", 0, 0, false
}

// watchedPriceChanged is false when this poll's price is the same level we
// already stored for that kind. A switch between midpoint and a one-sided
// quote still counts, so a one-sided spike can alert on the way back.
func watchedPriceChanged(w PriceWatch, kind string, price float64) bool {
	var prev float64
	switch kind {
	case "mid":
		prev = w.Mid
	case "bid":
		prev = w.Bid
	case "ask":
		prev = w.Ask
	}
	if prev > 0 && samePrice(prev, price) {
		prevKind, _, _, prevOK := watchedPrice(w.HasBid, w.HasAsk, w.HasMid, w.Bid, w.Ask, 0, 0, w.Mid)
		return prevOK && prevKind != kind
	}
	return true
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
