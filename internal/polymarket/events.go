package polymarket

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
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
	Sport        string `json:"sport"`
	PrimaryTagID int    `json:"primaryTagId"`
	Tags         string `json:"tags"`
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
	// Polymarket game event slugs use these league prefixes (bun-elv-bay-2026-09-13).
	"bun":                   {},
	"lal":                   {},
	"fl1":                   {},
	"sea":                   {},
	"ucl":                   {},
	"uel":                   {},
	"ere":                   {},
	"ufl":                   {},
	"ahl":                   {},
	"cfl":                   {},
	"khl":                   {},
	"npb":                   {},
	"kbo":                   {},
	"wsl":                   {},
	"t20":                   {},
	"odi":                   {},
	"mlb-gameday":           {},
	"nfl-gameday":           {},
	"premier-league":        {},
	"japan-j2-league":       {},
	"international-cricket": {},
}

// gameDateInSlug matches Polymarket match slugs like epl-mun-mac-2026-09-13
// and epl-mun-mac-2026-09-13-more-markets.
var gameDateInSlug = regexp.MustCompile(`(?:^|-)\d{4}-\d{2}-\d{2}(?:-|$)`)

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

func slugFirstToken(slug string) string {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return ""
	}
	i := strings.IndexByte(slug, '-')
	if i <= 0 {
		return slug
	}
	return slug[:i]
}

func addTagID(ids map[int]struct{}, id int) {
	if id > 0 && id != gamesTagID {
		ids[id] = struct{}{}
	}
}

func parseSportsTagIDs(csv string, ids map[int]struct{}) {
	for _, part := range strings.Split(csv, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil {
			addTagID(ids, n)
		}
	}
}

// LooksLikeSportsSlug is true when a Gamma event or market slug is a
// sports league/game id (nfl-atl-pit-2026-09-13, epl-mun-mac-2026-09-13-more-markets).
func LooksLikeSportsSlug(slugs ...string) bool {
	return looksLikeSportsSlug(nil, slugs...)
}

func looksLikeSportsSlug(extraCodes map[string]struct{}, slugs ...string) bool {
	for _, raw := range slugs {
		slug := strings.ToLower(strings.TrimSpace(raw))
		if slug == "" {
			continue
		}
		tok := slugFirstToken(slug)
		if tok == "" {
			continue
		}
		if _, ok := sportsTagSlugs[tok]; ok {
			return true
		}
		if strings.HasPrefix(slug, "pro-football") || strings.HasPrefix(slug, "pro-basketball") ||
			strings.HasPrefix(slug, "pro-baseball") || strings.HasPrefix(slug, "pro-hockey") {
			return true
		}
		if extraCodes != nil {
			if _, ok := extraCodes[tok]; ok && gameDateInSlug.MatchString(slug) {
				return true
			}
		}
	}
	return false
}

func tagsAreSports(tags []eventTag, extraIDs map[int]struct{}, extraCodes map[string]struct{}) bool {
	for _, t := range tags {
		slug := strings.ToLower(strings.TrimSpace(t.Slug))
		if slug == "games" || slug == "all" {
			continue
		}
		if _, ok := sportsTagSlugs[slug]; ok {
			return true
		}
		if extraCodes != nil {
			if _, ok := extraCodes[slug]; ok {
				return true
			}
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

func (c *Client) sportsCatalog(ctx context.Context) (map[int]struct{}, map[string]struct{}) {
	if c == nil {
		return nil, nil
	}
	c.sportsMu.Lock()
	if c.sportsIDsOK {
		ids, codes := c.sportsTagIDs, c.sportsCodes
		c.sportsMu.Unlock()
		return ids, codes
	}
	c.sportsMu.Unlock()

	ids := map[int]struct{}{1: {}} // Gamma "sports" tag id
	codes := map[string]struct{}{}
	var sports []gammaSport
	if err := c.getJSON(ctx, gammaBase+"/sports", &sports); err == nil {
		for _, s := range sports {
			addTagID(ids, s.PrimaryTagID)
			parseSportsTagIDs(s.Tags, ids)
			if code := strings.ToLower(strings.TrimSpace(s.Sport)); code != "" {
				codes[code] = struct{}{}
			}
		}
	}
	var related []sportsRelatedTag
	if err := c.getJSON(ctx, gammaBase+"/tags/slug/sports/related-tags", &related); err == nil {
		for _, r := range related {
			addTagID(ids, r.RelatedTagID)
		}
	}

	c.sportsMu.Lock()
	if !c.sportsIDsOK {
		c.sportsTagIDs = ids
		c.sportsCodes = codes
		c.sportsIDsOK = true
	}
	ids, codes = c.sportsTagIDs, c.sportsCodes
	c.sportsMu.Unlock()
	return ids, codes
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

	ids, codes := c.sportsCatalog(ctx)
	if looksLikeSportsSlug(codes, slug) {
		meta := EventMeta{Slug: slug, IsSports: true}
		c.eventMu.Lock()
		c.eventCache[slug] = meta
		c.eventMu.Unlock()
		return meta, nil
	}

	u := gammaBase + "/events?slug=" + url.QueryEscape(slug)
	var events []gammaEvent
	if err := c.getJSON(ctx, u, &events); err != nil {
		return EventMeta{}, err
	}

	meta := EventMeta{Slug: slug}
	if len(events) > 0 {
		e := events[0]
		meta.Title = e.Title
		meta.Icon = strings.TrimSpace(e.Icon)
		if meta.Icon == "" {
			meta.Icon = strings.TrimSpace(e.Image)
		}
		meta.IsSports = tagsAreSports(e.Tags, ids, codes)
	}

	// Don't cache "not sports" when Gamma returned no tags — a new match
	// market can show up before league tags are attached.
	if !meta.IsSports && (len(events) == 0 || (len(events) > 0 && len(events[0].Tags) == 0)) {
		return meta, nil
	}

	c.eventMu.Lock()
	c.eventCache[slug] = meta
	c.eventMu.Unlock()
	return meta, nil
}

// EventIsSports reports whether the event is sports (Gamma sports/league tags
// or a sports game slug). Lookup failures return an error so callers can retry
// instead of paging the fill.
func (c *Client) EventIsSports(ctx context.Context, eventSlug string) (bool, error) {
	if LooksLikeSportsSlug(eventSlug) {
		return true, nil
	}
	meta, err := c.EventMeta(ctx, eventSlug)
	if err != nil {
		return false, err
	}
	return meta.IsSports, nil
}
