package polymarket

import "testing"

func TestExtractMarketSlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "https://polymarket.com/event/which-party-will-win-the-senate-in-2026/will-the-democratic-party-control-the-senate-after-the-2026-midterm-elections",
			want: "will-the-democratic-party-control-the-senate-after-the-2026-midterm-elections",
		},
		{
			in:   "will-the-democratic-party-control-the-senate-after-the-2026-midterm-elections",
			want: "will-the-democratic-party-control-the-senate-after-the-2026-midterm-elections",
		},
		{
			in:   "polymarket.com/market/foo-bar",
			want: "foo-bar",
		},
	}
	for _, tc := range cases {
		got, err := extractMarketSlug(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsConditionID(t *testing.T) {
	ok := "0x307a1ed89d60b61002dd5bbf00e1408c5ed2ab3fcdb056191ca7ef9bc34d38f3"
	if !isConditionID(ok) {
		t.Fatal("expected valid condition id")
	}
	if isConditionID("not-a-condition") {
		t.Fatal("expected invalid")
	}
}
