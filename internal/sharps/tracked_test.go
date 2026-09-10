package sharps

import "testing"

func TestLookup(t *testing.T) {
	all := Lookup("")
	if len(all) != len(Tracked) {
		t.Fatalf("all=%d", len(all))
	}
	got := Lookup("SnowLover7")
	if len(got) != 1 || got[0].Name != "SnowLover7" {
		t.Fatalf("%+v", got)
	}
	got = Lookup("snow")
	if len(got) != 1 || got[0].Name != "SnowLover7" {
		t.Fatalf("prefix %+v", got)
	}
	got = Lookup("0xc8b9a30184244d427169cf62485dde6041b2b836")
	if len(got) != 1 || got[0].Name != "SnowLover7" {
		t.Fatalf("addr %+v", got)
	}
	if len(Lookup("zzzz-nope")) != 0 {
		t.Fatal("miss")
	}
}
