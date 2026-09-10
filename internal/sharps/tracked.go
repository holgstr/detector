package sharps

import (
	"strings"
)

// Wallet is a tracked sharp from the detector activity/portfolio tabs.
type Wallet struct {
	Address string
	Name    string
}

// Tracked is the same list as docs/sharps.js TRACKED. Keep the two in sync.
var Tracked = []Wallet{
	{Address: "0x23d81ba9371e576015c1e562db09c689f56b0288", Name: "flawfence"},
	{Address: "0x614dc8d3542c12103d2c6a3553fd761e391d1546", Name: "mr.ozi"},
	{Address: "0x7bc14171ccb0d3e6bac219ec6a76211826e28db4", Name: "coali10"},
	{Address: "0x2b9dbf4b6e0e11309a9d6d2a09b72f65f652adc0", Name: "seal7"},
	{Address: "0xc851cd9bee7d262afd78674f861f9f576a12cd2a", Name: "betwick"},
	{Address: "0x1cc16713196d456f86fa9c7387dd326a7f73b8df", Name: "Wickier"},
	{Address: "0xde7be6d489bce070a959e0cb813128ae659b5f4b", Name: "wan123"},
	{Address: "0xc8b9a30184244d427169cf62485dde6041b2b836", Name: "SnowLover7"},
	{Address: "0x55291dc2069439a6de5c93a9bec8da2215a9e5b9", Name: "i2dt"},
	{Address: "0xbaa2bcb5439e985ce4ccf815b4700027d1b92c73", Name: "denizz"},
	{Address: "0xd24b95551eb288ff82bb625dcd7f32f62abdef76", Name: "BiDiFakePolls"},
	{Address: "0x8a4c788f043023b8b28a762216d037e9f148532b", Name: "occasionalAwareness"},
	{Address: "0x448861155279dbf833d041b963e3ac854599e319", Name: "Flipadelphia"},
	{Address: "0xf1f9a438f2697381b4ac8eb283d6f4dd772d1245", Name: "tunatyler"},
	{Address: "0xa43da0aab839cf651a70e0e310462bb53a3b00f4", Name: "Wforkf"},
	{Address: "0x6640bd87f6e4b6e8d62457448bd1b3a4711a2202", Name: "Jellow2"},
}

// Addresses returns lowercase proxy wallets.
func Addresses() []string {
	out := make([]string, len(Tracked))
	for i, w := range Tracked {
		out[i] = strings.ToLower(w.Address)
	}
	return out
}

// NameOf returns the seeded display name, or empty if unknown.
func NameOf(wallet string) string {
	want := strings.ToLower(strings.TrimSpace(wallet))
	for _, w := range Tracked {
		if strings.ToLower(w.Address) == want {
			return w.Name
		}
	}
	return ""
}

// Lookup finds tracked wallets by display name or address prefix.
// A unique exact (case-insensitive) name or full address wins first.
func Lookup(query string) []Wallet {
	q := strings.TrimSpace(query)
	if q == "" {
		return append([]Wallet(nil), Tracked...)
	}
	ql := strings.ToLower(q)
	var exact, prefix []Wallet
	for _, w := range Tracked {
		name := strings.ToLower(w.Name)
		addr := strings.ToLower(w.Address)
		if name == ql || addr == ql {
			exact = append(exact, w)
			continue
		}
		if strings.HasPrefix(name, ql) || strings.HasPrefix(addr, ql) ||
			strings.Contains(name, ql) {
			prefix = append(prefix, w)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return prefix
}
