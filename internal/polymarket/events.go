package polymarket

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// EventMeta is the Gamma event fields we need for sports filtering.
type EventMeta struct {
	Slug     string
	Title    string
	Icon     string
	IsSports bool
}

type gammaEvent struct {
	Slug  string     `json:"slug"`
	Title string     `json:"title"`
	Icon  string     `json:"icon"`
	Image string     `json:"image"`
	Tags  []eventTag `json:"tags"`
}

type eventTag struct {
	ID    json.RawMessage `json:"id"`
	Slug  string          `json:"slug"`
	Label string          `json:"label"`
}

type gammaSport struct {
	PrimaryTagID int `json:"primaryTagId"`
}

type sportsRelatedTag struct {
	RelatedTagID int `json:"relatedTagID"`
}

// gamesTagID is Gamma's "Games" tag. It is related to sports (match
// markets) but also video-game pop-culture events, so it is never
// enough on its own to classify an event as sports.
const gamesTagID = 100639

// League / sport tags Gamma often emits without the parent "sports"
// slug. That parent is forceHide, so tennis, ATP, esports, and many
// soccer leagues show up as league-only — those still must stay off
// the Telegram ping list.
var sportsTagSlugs = map[string]struct{}{
	"sports":            {},
	"todays-sports":     {},
	"nfl":               {},
	"nba":               {},
	"mlb":               {},
	"nhl":               {},
	"wnba":              {},
	"ncaa":              {},
	"ncaaw":             {},
	"cfb":               {},
	"cbb":               {},
	"soccer":            {},
	"football":          {},
	"ufc":               {},
	"mma":               {},
	"tennis":            {},
	"atp":               {},
	"wta":               {},
	"golf":              {},
	"pga":               {},
	"cricket":           {},
	"esports":           {},
	"formula1":          {},
	"f1":                {},
	"boxing":            {},
	"boxingmma":         {},
	"chess":             {},
	"olympics":          {},
	"epl":               {},
	"mls":               {},
	"bundesliga":        {},
	"serie-a":           {},
	"la-liga":           {},
	"laliga":            {},
	"ligue-1":           {},
	"champions-league":  {},
	"europa-league":     {},
	"fantasy-football":  {},
	"lol":               {},
	"league-of-legends": {},
	"counter-strike-2":  {},
	"cs2":               {},
	"csgo":              {},
	"valorant":          {},
	"wimbledon":         {},
	"darts":             {},
	"rugby":             {},
	"basketball":        {},
	"baseball":          {},
	"hockey":            {},
	"march-madness":     {},
	"ipl":               {},
	"motogp":            {},
	"nascar":            {},
}

func parseTagID(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(s))
		return n
	}
	return 0
}

func tagsAreSports(tags []eventTag, extraIDs map[int]struct{}) bool {
	for _, t := range tags {
		slug := strings.ToLower(strings.TrimSpace(t.Slug))
		if slug == "games" || slug == "all" {
			continue
		}
		if _, ok := sportsTagSlugs[slug]; ok {
			return true
		}
		id := parseTagID(t.ID)
		if id != 0 && id != gamesTagID {
			if _, ok := extraIDs[id]; ok {
				return true
			}
		}
	}
	return false
}

func (c *Client) sportsCatalogIDs(ctx context.Context) map[int]struct{} {
	if c == nil {
		return nil
	}
	c.sportsMu.Lock()
	if c.sportsIDsOK {
		ids := c.sportsTagIDs
		c.sportsMu.Unlock()
		return ids
	}
	c.sportsMu.Unlock()

	ids := map[int]struct{}{1: {}} // Gamma "sports" tag id
	var sports []gammaSport
	if err := c.getJSON(ctx, gammaBase+"/sports", &sports); err == nil {
		for _, s := range sports {
			if s.PrimaryTagID > 0 && s.PrimaryTagID != gamesTagID {
				ids[s.PrimaryTagID] = struct{}{}
			}
		}
	}
	var related []sportsRelatedTag
	if err := c.getJSON(ctx, gammaBase+"/tags/slug/sports/related-tags", &related); err == nil {
		for _, r := range related {
			if r.RelatedTagID > 0 && r.RelatedTagID != gamesTagID {
				ids[r.RelatedTagID] = struct{}{}
			}
		}
	}

	c.sportsMu.Lock()
	if !c.sportsIDsOK {
		c.sportsTagIDs = ids
		c.sportsIDsOK = true
	}
	ids = c.sportsTagIDs
	c.sportsMu.Unlock()
	return ids
}

// EventMeta looks up a Gamma event by slug. Results are cached on the client.
// An empty slug is not sports (same as the activity tab).
func (c *Client) EventMeta(ctx context.Context, eventSlug string) (EventMeta, error) {
	slug := strings.TrimSpace(eventSlug)
	if slug == "" {
		return EventMeta{}, nil
	}

	c.eventMu.Lock()
	if c.eventCache == nil {
		c.eventCache = make(map[string]EventMeta)
	}
	if meta, ok := c.eventCache[slug]; ok {
		c.eventMu.Unlock()
		return meta, nil
	}
	c.eventMu.Unlock()

	u := gammaBase + "/events?slug=" + url.QueryEscape(slug)
	var events []gammaEvent
	if err := c.getJSON(ctx, u, &events); err != nil {
		return EventMeta{}, err
	}

	extra := c.sportsCatalogIDs(ctx)
	meta := EventMeta{Slug: slug}
	if len(events) > 0 {
		e := events[0]
		meta.Title = e.Title
		meta.Icon = strings.TrimSpace(e.Icon)
		if meta.Icon == "" {
			meta.Icon = strings.TrimSpace(e.Image)
		}
		meta.IsSports = tagsAreSports(e.Tags, extra)
	}

	c.eventMu.Lock()
	c.eventCache[slug] = meta
	c.eventMu.Unlock()
	return meta, nil
}

// EventIsSports reports whether the event is sports (Gamma sports/league tags).
func (c *Client) EventIsSports(ctx context.Context, eventSlug string) (bool, error) {
	meta, err := c.EventMeta(ctx, eventSlug)
	if err != nil {
		return false, err
	}
	return meta.IsSports, nil
}
