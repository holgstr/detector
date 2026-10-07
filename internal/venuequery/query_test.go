package venuequery

import "testing"

func TestSubjectQueryUsesPolymarketEventSlug(t *testing.T) {
	got := SubjectQuery("https://polymarket.com/event/south-carolina-senate-2026/will-the-democrats-win-the-south-carolina-senate-race-in-2026")
	if got != "south carolina senate 2026" {
		t.Fatalf("url %q", got)
	}
	if got := SearchQuery(got); got != "south carolina senate" {
		t.Fatalf("search from slug %q", got)
	}
	if got := SubjectQuery("South Carolina Senate"); got != "South Carolina Senate" {
		t.Fatalf("words %q", got)
	}
}

func TestSearchQueryDropsQuestionAndParty(t *testing.T) {
	got := SearchQuery("Will the Republicans win the Nevada governor race")
	if got != "nevada governor" {
		t.Fatalf("search %q", got)
	}
	if got := SearchQuery("florida governor"); got != "florida governor" {
		t.Fatalf("plain %q", got)
	}
	toks := MatchTokens("Will the Republicans win the Nevada governor race in 2026")
	if stringsJoin(toks) != "republicans nevada governor" {
		t.Fatalf("tokens %v", toks)
	}
}

func TestPartyHintAndPenalty(t *testing.T) {
	if PartyHint("GOVPARTYNV-26-R") != "republican" || PartyHint("NV_GOV_2026.DEM") != "democrat" {
		t.Fatal(PartyHint("GOVPARTYNV-26-R"), PartyHint("NV_GOV_2026.DEM"))
	}
	if PartyHint("KXMIDTERMMOV-NVGOVR") != "" {
		t.Fatal(PartyHint("KXMIDTERMMOV-NVGOVR"))
	}
	q := "Will the Republicans win the Nevada governor race"
	if EventPenalty("Nevada Governor winner?", q) != 0 {
		t.Fatalf("winner %d", EventPenalty("Nevada Governor winner?", q))
	}
	if EventPenalty("Nevada Governor margin of victory", q) <= 0 {
		t.Fatal("margin should cost more than the winner market")
	}
	if EventPenalty("Nevada Lieutenant Governor winner?", "nevada governor") <= 0 {
		t.Fatal("lieutenant should cost more than governor")
	}
	if !ContainsAll("joe lombardo republican", []string{"republicans"}) {
		t.Fatal("party stem")
	}
}

func stringsJoin(toks []string) string {
	out := ""
	for i, t := range toks {
		if i > 0 {
			out += " "
		}
		out += t
	}
	return out
}
