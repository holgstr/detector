package polymarket

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

const defaultUserSearchLimit = 25

// UserProfile is a Polymarket username bound to a proxy wallet.
type UserProfile struct {
	Address string
	Name    string
}

type publicProfileResponse struct {
	ProxyWallet string `json:"proxyWallet"`
	Name        string `json:"name"`
	Pseudonym   string `json:"pseudonym"`
}

type publicSearchProfiles struct {
	Profiles []publicSearchProfile `json:"profiles"`
}

type publicSearchProfile struct {
	Name        string `json:"name"`
	Pseudonym   string `json:"pseudonym"`
	ProxyWallet string `json:"proxyWallet"`
	Wallet      string `json:"wallet"`
}

func (c *Client) dataAPI() string {
	if c != nil && strings.TrimSpace(c.DataBase) != "" {
		return strings.TrimRight(c.DataBase, "/")
	}
	return dataBase
}

func (c *Client) gammaAPI() string {
	if c != nil && strings.TrimSpace(c.GammaBase) != "" {
		return strings.TrimRight(c.GammaBase, "/")
	}
	return gammaBase
}

// SearchUsers finds leaderboard profiles whose username matches query.
// Identity is always the proxy wallet; names are only a lookup key.
func (c *Client) SearchUsers(ctx context.Context, query string) ([]UserProfile, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty user query")
	}

	u := fmt.Sprintf(
		"%s/v1/leaderboard?userName=%s&timePeriod=ALL&orderBy=PNL&limit=%d&category=OVERALL",
		c.dataAPI(),
		url.QueryEscape(query),
		defaultUserSearchLimit,
	)
	var entries []leaderboardEntry
	if err := c.getJSON(ctx, u, &entries); err != nil {
		return nil, err
	}
	out := profilesFromLeaderboard(entries)
	if len(out) > 0 {
		return out, nil
	}

	q := url.Values{}
	q.Set("q", query)
	q.Set("limit_per_type", fmt.Sprintf("%d", defaultUserSearchLimit))
	q.Set("search_profiles", "true")
	q.Set("search_tags", "false")
	var resp publicSearchProfiles
	if err := c.getJSON(ctx, c.gammaAPI()+"/public-search?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	return profilesFromSearch(resp.Profiles), nil
}

// FetchProfile returns the current public name for a proxy wallet.
func (c *Client) FetchProfile(ctx context.Context, address string) (UserProfile, error) {
	addr, ok := ParseWalletAddress(address)
	if !ok {
		return UserProfile{}, fmt.Errorf("invalid wallet %q", address)
	}

	var p publicProfileResponse
	u := c.gammaAPI() + "/public-profile?address=" + url.QueryEscape(addr)
	if err := c.getJSON(ctx, u, &p); err == nil {
		got := strings.ToLower(strings.TrimSpace(p.ProxyWallet))
		if got == "" {
			got = addr
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = strings.TrimSpace(p.Pseudonym)
		}
		return UserProfile{Address: got, Name: name}, nil
	}

	lb := fmt.Sprintf(
		"%s/v1/leaderboard?user=%s&timePeriod=ALL&orderBy=PNL&limit=1&category=OVERALL",
		c.dataAPI(),
		url.QueryEscape(addr),
	)
	var entries []leaderboardEntry
	if err := c.getJSON(ctx, lb, &entries); err != nil {
		return UserProfile{Address: addr}, nil
	}
	if len(entries) == 0 {
		return UserProfile{Address: addr}, nil
	}
	name := strings.TrimSpace(entries[0].UserName)
	got := strings.ToLower(strings.TrimSpace(entries[0].ProxyWallet))
	if got == "" {
		got = addr
	}
	return UserProfile{Address: got, Name: name}, nil
}

// ParseWalletAddress accepts a 0x proxy wallet or a polymarket.com/profile URL.
func ParseWalletAddress(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'`“”„")
	if s == "" {
		return "", false
	}
	low := strings.ToLower(s)
	if i := strings.Index(low, "polymarket.com/profile/"); i >= 0 {
		rest := s[i+len("polymarket.com/profile/"):]
		rest = strings.SplitN(rest, "?", 2)[0]
		rest = strings.SplitN(rest, "/", 2)[0]
		s = rest
	}
	s = strings.TrimSpace(s)
	if !isProxyWallet(s) {
		return "", false
	}
	return strings.ToLower(s), true
}

func isProxyWallet(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(strings.ToLower(s), "0x") {
		return false
	}
	for _, r := range s[2:] {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !ok {
			return false
		}
	}
	return true
}

// PickUsers ranks username hits the same way tracked-wallet Lookup does:
// exact name/address, then prefix, then substring.
func PickUsers(query string, hits []UserProfile) []UserProfile {
	q := strings.ToLower(strings.TrimSpace(query))
	q = strings.Trim(q, "\"'`“”„")
	if q == "" {
		return nil
	}
	var exact, prefix, contain []UserProfile
	seen := make(map[string]struct{})
	for _, u := range hits {
		addr := strings.ToLower(strings.TrimSpace(u.Address))
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		name := strings.ToLower(strings.TrimSpace(u.Name))
		w := UserProfile{Address: addr, Name: strings.TrimSpace(u.Name)}
		switch {
		case name == q || addr == q:
			exact = append(exact, w)
		case strings.HasPrefix(name, q) || (strings.HasPrefix(q, "0x") && strings.HasPrefix(addr, q)):
			prefix = append(prefix, w)
		case strings.Contains(name, q):
			contain = append(contain, w)
		}
	}
	switch {
	case len(exact) > 0:
		return exact
	case len(prefix) > 0:
		return prefix
	default:
		return contain
	}
}

func profilesFromLeaderboard(entries []leaderboardEntry) []UserProfile {
	out := make([]UserProfile, 0, len(entries))
	seen := make(map[string]struct{})
	for _, e := range entries {
		addr := strings.ToLower(strings.TrimSpace(e.ProxyWallet))
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, UserProfile{Address: addr, Name: strings.TrimSpace(e.UserName)})
	}
	return out
}

func profilesFromSearch(rows []publicSearchProfile) []UserProfile {
	out := make([]UserProfile, 0, len(rows))
	seen := make(map[string]struct{})
	for _, p := range rows {
		addr := strings.ToLower(strings.TrimSpace(p.ProxyWallet))
		if addr == "" {
			addr = strings.ToLower(strings.TrimSpace(p.Wallet))
		}
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = strings.TrimSpace(p.Pseudonym)
		}
		out = append(out, UserProfile{Address: addr, Name: name})
	}
	return out
}
