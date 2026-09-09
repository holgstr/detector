package polymarket

import (
	"context"
	"net/url"
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
	Slug  string `json:"slug"`
	Label string `json:"label"`
}

// League tags Gamma sometimes emits without the parent "sports" slug
// (e.g. NFL awards). The activity tab only checks "sports"; the bot is
// slightly stricter so those still stay off the ping list.
var sportsTagSlugs = map[string]struct{}{
	"sports":   {},
	"nfl":      {},
	"nba":      {},
	"mlb":      {},
	"nhl":      {},
	"wnba":     {},
	"ncaa":     {},
	"cfb":      {},
	"cbb":      {},
	"soccer":   {},
	"football": {},
	"ufc":      {},
	"mma":      {},
}

func tagsAreSports(tags []eventTag) bool {
	for _, t := range tags {
		if _, ok := sportsTagSlugs[strings.ToLower(strings.TrimSpace(t.Slug))]; ok {
			return true
		}
	}
	return false
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

	meta := EventMeta{Slug: slug}
	if len(events) > 0 {
		e := events[0]
		meta.Title = e.Title
		meta.Icon = strings.TrimSpace(e.Icon)
		if meta.Icon == "" {
			meta.Icon = strings.TrimSpace(e.Image)
		}
		meta.IsSports = tagsAreSports(e.Tags)
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
