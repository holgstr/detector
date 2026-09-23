package polymarket

import (
	"encoding/json"
	"testing"
)

func TestFlattenHoldersLabelsSides(t *testing.T) {
	raw := []byte(`[
		{"token":"yes","holders":[
			{"proxyWallet":"0xAAA","name":"holder4","amount":10,"outcomeIndex":0},
			{"proxyWallet":"0xCCC","amount":0,"outcomeIndex":0}
		]},
		{"token":"no","holders":[
			{"proxyWallet":"0xBBB","pseudonym":"quiet","amount":4,"outcomeIndex":1}
		]}
	]`)
	var groups []holdersResponse
	if err := json.Unmarshal(raw, &groups); err != nil {
		t.Fatal(err)
	}
	got := flattenHolders(groups)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].Wallet != "0xaaa" || got[0].Name != "holder4" || got[0].Outcome != "YES" || got[0].Size != 10 {
		t.Fatalf("%+v", got[0])
	}
	if got[1].Wallet != "0xbbb" || got[1].Name != "quiet" || got[1].Outcome != "NO" {
		t.Fatalf("%+v", got[1])
	}
}
