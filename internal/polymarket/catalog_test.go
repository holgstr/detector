package polymarket

import "testing"

func TestIsBinaryYesNo(t *testing.T) {
	if !isBinaryYesNo([]string{"Yes", "No"}) {
		t.Fatal("expected yes/no")
	}
	if isBinaryYesNo([]string{"A", "B"}) {
		t.Fatal("expected non-binary labels rejected")
	}
	if isBinaryYesNo([]string{"Yes"}) {
		t.Fatal("expected single outcome rejected")
	}
}

func TestParseJSONFloatArray(t *testing.T) {
	got := parseJSONFloatArray(`["0.4", "0.6"]`)
	if len(got) != 2 || got[0] != 0.4 || got[1] != 0.6 {
		t.Fatalf("got %#v", got)
	}
	got = parseJSONFloatArray(`[0.1, 0.9]`)
	if len(got) != 2 || got[0] != 0.1 {
		t.Fatalf("got %#v", got)
	}
}
