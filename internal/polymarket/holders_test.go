package polymarket

import (
	"encoding/json"
	"testing"
)

func TestFlattenHoldersLabelsSides(t *testing.T) {
	raw := []byte(`[
		{"token_id":"yes","holders":[
			{"proxy_wallet":"0xAAA","name":"holder4","amount":10,"outcome_index":0},
			{"proxy_wallet":"0xCCC","amount":0,"outcome_index":0}
		]},
		{"token_id":"no","holders":[
			{"proxy_wallet":"0xBBB","pseudonym":"quiet","amount":4,"outcome_index":1}
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
