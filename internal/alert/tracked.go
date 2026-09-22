package alert

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

const (
	AddUsage   = "Usage: /add <wallet or name> — names resolve to the current Polymarket wallet id."
	UnaddUsage = "Usage: /unadd <wallet or name> — removes that wallet id (names can change)."
)

type userLookup interface {
	SearchUsers(ctx context.Context, query string) ([]polymarket.UserProfile, error)
	FetchProfile(ctx context.Context, address string) (polymarket.UserProfile, error)
}

// ApplyTracked installs the checkpoint's extra/untracked overlay as the live list.
func ApplyTracked(s *State) {
	if s == nil {
		sharps.Reset()
		return
	}
	sharps.SetList(s.ActiveWallets())
}

// ActiveWallets is the seed list minus /unadd, plus /add, keyed by address.
func (s *State) ActiveWallets() []sharps.Wallet {
	removed := s.untrackedSet()
	extras := make(map[string]sharps.Wallet, len(s.ExtraWallets))
	for _, w := range s.ExtraWallets {
		addr := strings.ToLower(strings.TrimSpace(w.Address))
		if addr == "" {
			continue
		}
		extras[addr] = sharps.Wallet{Address: addr, Name: strings.TrimSpace(w.Name)}
	}

	out := make([]sharps.Wallet, 0, len(sharps.Tracked)+len(extras))
	seen := make(map[string]struct{}, len(sharps.Tracked)+len(extras))
	for _, w := range sharps.Tracked {
		addr := strings.ToLower(strings.TrimSpace(w.Address))
		if _, skip := removed[addr]; skip {
			continue
		}
		name := w.Name
		if extra, ok := extras[addr]; ok && extra.Name != "" {
			name = extra.Name
		}
		out = append(out, sharps.Wallet{Address: addr, Name: name})
		seen[addr] = struct{}{}
	}
	var extraOnly []sharps.Wallet
	for addr, w := range extras {
		if _, ok := seen[addr]; ok {
			continue
		}
		extraOnly = append(extraOnly, w)
	}
	sort.Slice(extraOnly, func(i, j int) bool {
		ni, nj := strings.ToLower(extraOnly[i].Name), strings.ToLower(extraOnly[j].Name)
		if ni != nj {
			return ni < nj
		}
		return extraOnly[i].Address < extraOnly[j].Address
	})
	return append(out, extraOnly...)
}

// AddWallet tracks addr (name is display-only). already is true if it was live.
func (s *State) AddWallet(w sharps.Wallet) (already bool) {
	addr := strings.ToLower(strings.TrimSpace(w.Address))
	if addr == "" {
		return true
	}
	w.Address = addr
	w.Name = strings.TrimSpace(w.Name)

	already = s.liveHas(addr)
	s.dropUntracked(addr)
	if isSeedAddress(addr) {
		return already
	}
	s.upsertExtra(w)
	return already
}

// UnaddWallet drops addr from the live set. ok is false if it was not tracked.
func (s *State) UnaddWallet(addr string) (sharps.Wallet, bool) {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if addr == "" {
		return sharps.Wallet{}, false
	}
	cur := s.walletByAddress(addr)
	if !s.liveHas(addr) {
		return cur, false
	}
	s.dropExtra(addr)
	if isSeedAddress(addr) {
		s.addUntracked(addr)
	}
	return cur, true
}

func (s *State) liveHas(addr string) bool {
	for _, w := range s.ActiveWallets() {
		if w.Address == addr {
			return true
		}
	}
	return false
}

func (s *State) walletByAddress(addr string) sharps.Wallet {
	for _, w := range s.ActiveWallets() {
		if w.Address == addr {
			return w
		}
	}
	for _, w := range sharps.Tracked {
		if strings.ToLower(w.Address) == addr {
			return sharps.Wallet{Address: addr, Name: w.Name}
		}
	}
	return sharps.Wallet{Address: addr}
}

func (s *State) untrackedSet() map[string]struct{} {
	out := make(map[string]struct{}, len(s.Untracked))
	for _, a := range s.Untracked {
		a = strings.ToLower(strings.TrimSpace(a))
		if a != "" {
			out[a] = struct{}{}
		}
	}
	return out
}

func (s *State) dropUntracked(addr string) {
	if len(s.Untracked) == 0 {
		return
	}
	next := s.Untracked[:0]
	for _, a := range s.Untracked {
		if strings.ToLower(strings.TrimSpace(a)) == addr {
			continue
		}
		next = append(next, a)
	}
	if len(next) == 0 {
		s.Untracked = nil
		return
	}
	s.Untracked = next
}

func (s *State) addUntracked(addr string) {
	if _, ok := s.untrackedSet()[addr]; ok {
		return
	}
	s.Untracked = append(s.Untracked, addr)
}

func (s *State) upsertExtra(w sharps.Wallet) {
	for i, e := range s.ExtraWallets {
		if strings.ToLower(e.Address) == w.Address {
			s.ExtraWallets[i] = w
			return
		}
	}
	s.ExtraWallets = append(s.ExtraWallets, w)
}

func (s *State) dropExtra(addr string) {
	if len(s.ExtraWallets) == 0 {
		return
	}
	next := s.ExtraWallets[:0]
	for _, w := range s.ExtraWallets {
		if strings.ToLower(w.Address) == addr {
			continue
		}
		next = append(next, w)
	}
	if len(next) == 0 {
		s.ExtraWallets = nil
		return
	}
	s.ExtraWallets = next
}

func isSeedAddress(addr string) bool {
	for _, w := range sharps.Tracked {
		if strings.ToLower(w.Address) == addr {
			return true
		}
	}
	return false
}

// FormatTrackedList is the /tracked reply (names only).
func FormatTrackedList(wallets []sharps.Wallet) string {
	if len(wallets) == 0 {
		return "Not tracking any wallets."
	}
	names := make([]string, 0, len(wallets))
	for _, w := range wallets {
		n := strings.TrimSpace(w.Name)
		if n == "" {
			n = shortWallet(w.Address)
		}
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return strings.Join(names, "\n")
}

func shortWallet(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if len(addr) >= 12 {
		return addr[:6] + "…" + addr[len(addr)-4:]
	}
	return addr
}

// ResolveAddWallet maps a name or wallet id to the canonical proxy address.
func ResolveAddWallet(ctx context.Context, api userLookup, query string) (sharps.Wallet, string) {
	q := strings.TrimSpace(query)
	if q == "" {
		return sharps.Wallet{}, AddUsage
	}
	if addr, ok := polymarket.ParseWalletAddress(q); ok {
		p, err := api.FetchProfile(ctx, addr)
		if err != nil {
			return sharps.Wallet{Address: addr, Name: shortWallet(addr)}, ""
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = shortWallet(addr)
		}
		if p.Address != "" {
			addr = p.Address
		}
		return sharps.Wallet{Address: addr, Name: name}, ""
	}
	w, errMsg := resolveNameToWallet(ctx, api, q)
	return w, errMsg
}

// ResolveUnaddWallet maps a name or wallet id to a currently tracked address.
// Local stored names are tried first so a rename on Polymarket still unadds the id.
func ResolveUnaddWallet(ctx context.Context, api userLookup, query string) (sharps.Wallet, string) {
	q := strings.TrimSpace(query)
	if q == "" {
		return sharps.Wallet{}, UnaddUsage
	}
	if addr, ok := polymarket.ParseWalletAddress(q); ok {
		hits := sharps.Lookup(addr)
		if len(hits) == 1 {
			return hits[0], ""
		}
		return sharps.Wallet{Address: addr}, ""
	}
	hits := sharps.Lookup(q)
	if len(hits) == 1 {
		return hits[0], ""
	}
	if len(hits) > 1 {
		return sharps.Wallet{}, fmt.Sprintf("Several tracked traders match %q: %s", q, joinWalletNames(hits))
	}
	w, errMsg := resolveNameToWallet(ctx, api, q)
	if errMsg != "" {
		if strings.HasPrefix(errMsg, "No Polymarket") {
			return sharps.Wallet{}, fmt.Sprintf("No tracked trader matching %q.", q)
		}
		return sharps.Wallet{}, errMsg
	}
	return w, ""
}

// ResolveAnyTrader maps a name or wallet id to one wallet.
// Tracked names (including short unique prefixes) win; otherwise the
// current Polymarket profile is used even if we are not watching them.
func ResolveAnyTrader(ctx context.Context, api userLookup, query string) (sharps.Wallet, string) {
	q := strings.TrimSpace(query)
	if q == "" || strings.EqualFold(q, "all") {
		return sharps.Wallet{}, "Usage: <trader> — a Polymarket name or wallet id."
	}
	hits := sharps.Lookup(q)
	if len(hits) == 1 {
		return hits[0], ""
	}
	if len(hits) > 1 {
		return sharps.Wallet{}, fmt.Sprintf("Several traders match %q: %s", q, joinWalletNames(hits))
	}
	if addr, ok := polymarket.ParseWalletAddress(q); ok {
		if api == nil {
			return sharps.Wallet{Address: addr, Name: shortWallet(addr)}, ""
		}
		p, err := api.FetchProfile(ctx, addr)
		if err != nil {
			return sharps.Wallet{Address: addr, Name: shortWallet(addr)}, ""
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = shortWallet(addr)
		}
		if p.Address != "" {
			addr = p.Address
		}
		return sharps.Wallet{Address: addr, Name: name}, ""
	}
	if api == nil {
		return sharps.Wallet{}, fmt.Sprintf("No Polymarket user matching %q.", q)
	}
	return resolveNameToWallet(ctx, api, q)
}

func resolveNameToWallet(ctx context.Context, api userLookup, query string) (sharps.Wallet, string) {
	hits, err := api.SearchUsers(ctx, query)
	if err != nil {
		return sharps.Wallet{}, fmt.Sprintf("Couldn't look up %q: %v", query, err)
	}
	picked := polymarket.PickUsers(query, hits)
	if len(picked) == 0 {
		return sharps.Wallet{}, fmt.Sprintf("No Polymarket user matching %q.", query)
	}
	if len(picked) > 1 {
		names := make([]string, len(picked))
		for i, p := range picked {
			n := strings.TrimSpace(p.Name)
			if n == "" {
				n = shortWallet(p.Address)
			}
			names[i] = n
		}
		return sharps.Wallet{}, fmt.Sprintf("Several users match %q: %s — use a wallet id.", query, strings.Join(names, ", "))
	}
	name := strings.TrimSpace(picked[0].Name)
	if name == "" {
		name = query
	}
	return sharps.Wallet{Address: picked[0].Address, Name: name}, ""
}

func joinWalletNames(wallets []sharps.Wallet) string {
	names := make([]string, len(wallets))
	for i, w := range wallets {
		n := strings.TrimSpace(w.Name)
		if n == "" {
			n = shortWallet(w.Address)
		}
		names[i] = n
	}
	return strings.Join(names, ", ")
}
