package alert

import (
	"math"
	"strings"
	"testing"
)

func TestComputeKellyBuyYes(t *testing.T) {
	r := ComputeKelly(0.40, 0.50)
	if r.Side != "YES" {
		t.Fatalf("side %s", r.Side)
	}
	want := (0.50 - 0.40) / (1 - 0.40)
	if math.Abs(r.Full-want) > 1e-9 {
		t.Fatalf("full=%v want %v", r.Full, want)
	}
	if math.Abs(r.Half-want/2) > 1e-9 || math.Abs(r.Third-want/3) > 1e-9 || math.Abs(r.Fourth-want/4) > 1e-9 {
		t.Fatalf("%+v", r)
	}
}

func TestComputeKellyBuyNo(t *testing.T) {
	r := ComputeKelly(0.60, 0.50)
	if r.Side != "NO" {
		t.Fatalf("side %s", r.Side)
	}
	want := (0.60 - 0.50) / 0.60
	if math.Abs(r.Full-want) > 1e-9 {
		t.Fatalf("full=%v want %v", r.Full, want)
	}
}

func TestComputeKellyNoEdge(t *testing.T) {
	r := ComputeKelly(0.42, 0.42)
	if r.Full != 0 || r.Side != "NONE" {
		t.Fatalf("%+v", r)
	}
	text := KellyText(0.42, 0.42)
	if !strings.Contains(text, "No edge") {
		t.Fatalf("%q", text)
	}
}

func TestKellyText(t *testing.T) {
	text := KellyText(0.40, 0.50)
	if strings.Contains(text, "Kelly") || strings.Contains(text, "40¢") {
		t.Fatalf("headline leaked: %q", text)
	}
	for _, s := range []string{"Full", "Half", "1/3", "1/4", "16.7%"} {
		if !strings.Contains(text, s) {
			t.Fatalf("missing %q in %q", s, text)
		}
	}
}

func TestParseProb(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0.42", 0.42},
		{"42", 0.42},
		{"42c", 0.42},
		{"42¢", 0.42},
		{"55%", 0.55},
		{"0.055", 0.055},
	}
	for _, tc := range cases {
		got, ok := parseProb(tc.in)
		if !ok || math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("%q got %v ok=%v", tc.in, got, ok)
		}
	}
	if _, ok := parseProb("nope"); ok {
		t.Fatal("nope")
	}
}
