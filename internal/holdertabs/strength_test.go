package holdertabs

import (
	"math"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
)

func ptr(v float64) *float64 { return &v }

func TestComputeStrength(t *testing.T) {
	r := &polymarket.Result{
		FetchedAt: "2026-08-06T00:00:00Z",
		Market: polymarket.Market{
			ConditionID: "0xabc",
			Slug:        "demo",
			Question:    "Demo?",
		},
		Yes: polymarket.HolderSide{
			Outcome: "Yes",
			Holders: []polymarket.HolderRecord{
				{Holder: "0x1", Size: 100, LifetimePnL: ptr(200_000)},
				{Holder: "0x2", Size: 50, LifetimePnL: ptr(50_000)},  // below threshold
				{Holder: "0x3", Size: 25, LifetimePnL: nil},          // missing
			},
		},
		No: polymarket.HolderSide{
			Outcome: "No",
			Holders: []polymarket.HolderRecord{
				{Holder: "0x4", Size: 300, LifetimePnL: ptr(150_000)},
				{Holder: "0x5", Size: 10, LifetimePnL: ptr(100_000)}, // not strictly >
			},
		},
	}

	s := ComputeStrength(r, 100_000)
	if s.Yes.QualifiedCount != 1 || s.Yes.QualifiedSize != 100 {
		t.Fatalf("yes: got count=%d size=%v", s.Yes.QualifiedCount, s.Yes.QualifiedSize)
	}
	if s.No.QualifiedCount != 1 || s.No.QualifiedSize != 300 {
		t.Fatalf("no: got count=%d size=%v", s.No.QualifiedCount, s.No.QualifiedSize)
	}
	if s.TotalQualified != 400 {
		t.Fatalf("total=%v", s.TotalQualified)
	}
	if math.Abs(s.Yes.Strength-0.25) > 1e-12 || math.Abs(s.No.Strength-0.75) > 1e-12 {
		t.Fatalf("strength yes=%v no=%v", s.Yes.Strength, s.No.Strength)
	}
	if s.Stronger != "no" {
		t.Fatalf("stronger=%q", s.Stronger)
	}
}

func TestComputeStrengthTieEmpty(t *testing.T) {
	r := &polymarket.Result{
		Yes: polymarket.HolderSide{Outcome: "Yes"},
		No:  polymarket.HolderSide{Outcome: "No"},
	}
	s := ComputeStrength(r, DefaultMinLifetimePnL)
	if s.Stronger != "tie" || s.Yes.Strength != 0 || s.No.Strength != 0 {
		t.Fatalf("unexpected empty strength: %+v", s)
	}
}
