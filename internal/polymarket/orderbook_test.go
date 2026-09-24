package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClosestTicksFromInside(t *testing.T) {
	levels := []BookLevel{
		{Price: 0.001, Size: 100},
		{Price: 0.041, Size: 480},
		{Price: 0.040, Size: 10},
		{Price: 0.038, Size: 25},
	}
	got := ClosestTicks(levels, 0.001, 4, true)
	if len(got) != 4 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if mathAbs(got[0].Price-0.041) > 1e-9 || got[0].Size != 480 {
		t.Fatalf("best bid %+v", got[0])
	}
	if mathAbs(got[1].Price-0.040) > 1e-9 || got[1].Size != 10 {
		t.Fatalf("2nd %+v", got[1])
	}
	if mathAbs(got[2].Price-0.038) > 1e-9 || got[2].Size != 25 {
		t.Fatalf("skip empty 0.039: %+v", got[2])
	}
	if mathAbs(got[3].Price-0.001) > 1e-9 || got[3].Size != 100 {
		t.Fatalf("past the empty gap: %+v", got[3])
	}

	asks := []BookLevel{
		{Price: 0.999, Size: 1},
		{Price: 0.042, Size: 1118},
		{Price: 0.044, Size: 50},
		{Price: 0.050, Size: 7},
		{Price: 0.080, Size: 9},
	}
	got = ClosestTicks(asks, 0.001, 4, false)
	if len(got) != 4 {
		t.Fatalf("asks len=%d %+v", len(got), got)
	}
	if mathAbs(got[0].Price-0.042) > 1e-9 || got[0].Size != 1118 {
		t.Fatalf("best ask %+v", got[0])
	}
	if mathAbs(got[1].Price-0.044) > 1e-9 || got[1].Size != 50 {
		t.Fatalf("skip empty 0.043: %+v", got[1])
	}
	if mathAbs(got[2].Price-0.050) > 1e-9 || got[2].Size != 7 {
		t.Fatalf("3rd %+v", got[2])
	}
	if mathAbs(got[3].Price-0.080) > 1e-9 || got[3].Size != 9 {
		t.Fatalf("4th, not the farther 0.999: %+v", got[3])
	}
}

func TestClosestTicksEmpty(t *testing.T) {
	if ClosestTicks(nil, 0.01, 4, true) != nil {
		t.Fatal("empty")
	}
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestFetchOutcomeBooks(t *testing.T) {
	gamma := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]gammaMarket{{
			ConditionID:  "0xabc",
			Slug:         "aliens",
			Question:     "Aliens?",
			Outcomes:     `["Yes","No"]`,
			ClobTokenIDs: `["tok-yes","tok-no"]`,
			Active:       true,
		}})
	}))
	defer gamma.Close()

	clob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token_id")
		book := clobBookResponse{TickSize: flexNumber{V: 0.01}}
		if tok == "tok-yes" {
			book.Bids = []clobLevel{{Price: flexNumber{V: 0.40}, Size: flexNumber{V: 100}}, {Price: flexNumber{V: 0.42}, Size: flexNumber{V: 50}}}
			book.Asks = []clobLevel{{Price: flexNumber{V: 0.45}, Size: flexNumber{V: 20}}, {Price: flexNumber{V: 0.43}, Size: flexNumber{V: 10}}}
		} else {
			book.Bids = []clobLevel{{Price: flexNumber{V: 0.55}, Size: flexNumber{V: 8}}}
			book.Asks = []clobLevel{{Price: flexNumber{V: 0.57}, Size: flexNumber{V: 9}}}
		}
		_ = json.NewEncoder(w).Encode(book)
	}))
	defer clob.Close()

	c := NewClient()
	c.GammaBase = gamma.URL
	c.ClobBase = clob.URL
	got, err := c.FetchOutcomeBooks(context.Background(), "0xabc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("outcomes=%d", len(got))
	}
	if got[0].Outcome != "Yes" || mathAbs(got[0].Bids[0].Price-0.42) > 1e-9 || got[0].Bids[0].Size != 50 {
		t.Fatalf("yes bids %+v", got[0].Bids)
	}
	if mathAbs(got[0].Asks[0].Price-0.43) > 1e-9 || got[0].Asks[0].Size != 10 {
		t.Fatalf("yes asks %+v", got[0].Asks)
	}
	if got[1].Outcome != "No" || mathAbs(got[1].Bids[0].Price-0.55) > 1e-9 {
		t.Fatalf("no %+v", got[1])
	}
}

func TestSizeAtOrBelow(t *testing.T) {
	levels := []BookLevel{
		{Price: 0.42, Size: 50},
		{Price: 0.40, Size: 100},
		{Price: 0.32, Size: 1000},
		{Price: 0.31, Size: 10},
	}
	if got := SizeAtOrBelow(levels, 0.32); mathAbs(got-1010) > 1e-9 {
		t.Fatalf("32c or lower: %v", got)
	}
	if got := SizeAtOrBelow(levels, 0.40); mathAbs(got-1110) > 1e-9 {
		t.Fatalf("40c or lower: %v", got)
	}
	if got := SizeAtOrBelow(levels, 0.20); got != 0 {
		t.Fatalf("too high: %v", got)
	}
}

func TestFetchYesBookUsesFullDepth(t *testing.T) {
	gamma := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]gammaMarket{{
			ConditionID:  "0xabc",
			Slug:         "aliens",
			Question:     "Aliens?",
			Outcomes:     `["Yes","No"]`,
			ClobTokenIDs: `["tok-yes","tok-no"]`,
			Active:       true,
		}})
	}))
	defer gamma.Close()

	clob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token_id") != "tok-yes" {
			t.Fatalf("unexpected token %s", r.URL.Query().Get("token_id"))
		}
		book := clobBookResponse{
			TickSize: flexNumber{V: 0.01},
			Asks: []clobLevel{
				{Price: flexNumber{V: 0.32}, Size: flexNumber{V: 400}},
				{Price: flexNumber{V: 0.33}, Size: flexNumber{V: 50}},
				{Price: flexNumber{V: 0.34}, Size: flexNumber{V: 10}},
				{Price: flexNumber{V: 0.35}, Size: flexNumber{V: 5}},
				{Price: flexNumber{V: 0.40}, Size: flexNumber{V: 1000}},
			},
		}
		_ = json.NewEncoder(w).Encode(book)
	}))
	defer clob.Close()

	c := NewClient()
	c.GammaBase = gamma.URL
	c.ClobBase = clob.URL
	got, err := c.FetchYesBook(context.Background(), "0xabc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "Yes" || len(got.Asks) != 5 {
		t.Fatalf("%+v", got)
	}
	if mathAbs(SizeAtOrBelow(got.Asks, 0.32)-400) > 1e-9 {
		t.Fatalf("size=%v asks=%+v", SizeAtOrBelow(got.Asks, 0.32), got.Asks)
	}
}

func TestFetchOutcomeBookNo(t *testing.T) {
	gamma := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]gammaMarket{{
			ConditionID:  "0xabc",
			Outcomes:     `["Yes","No"]`,
			ClobTokenIDs: `["tok-yes","tok-no"]`,
			Active:       true,
		}})
	}))
	defer gamma.Close()

	clob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token_id") != "tok-no" {
			t.Fatalf("unexpected token %s", r.URL.Query().Get("token_id"))
		}
		_ = json.NewEncoder(w).Encode(clobBookResponse{
			TickSize: flexNumber{V: 0.01},
			Bids:     []clobLevel{{Price: flexNumber{V: 0.81}, Size: flexNumber{V: 10}}},
			Asks:     []clobLevel{{Price: flexNumber{V: 0.82}, Size: flexNumber{V: 12}}},
		})
	}))
	defer clob.Close()

	c := NewClient()
	c.GammaBase = gamma.URL
	c.ClobBase = clob.URL
	got, err := c.FetchOutcomeBook(context.Background(), "0xabc", "NO")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "No" || mathAbs(got.Bids[0].Price-0.81) > 1e-9 || mathAbs(got.Asks[0].Price-0.82) > 1e-9 {
		t.Fatalf("%+v", got)
	}
	if _, err := c.FetchOutcomeBook(context.Background(), "0xabc", "Maybe"); err == nil {
		t.Fatal("expected missing outcome")
	}
}

func TestFlexNumberUnmarshal(t *testing.T) {
	var b clobBookResponse
	if err := json.Unmarshal([]byte(`{"tick_size":"0.001","bids":[{"price":"0.041","size":"480.45"}]}`), &b); err != nil {
		t.Fatal(err)
	}
	if b.TickSize.V != 0.001 || b.Bids[0].Price.V != 0.041 || b.Bids[0].Size.V != 480.45 {
		t.Fatalf("%+v", b)
	}
	if err := json.Unmarshal([]byte(`{"tick_size":0.01,"asks":[{"price":0.5,"size":10}]}`), &b); err != nil {
		t.Fatal(err)
	}
	if b.TickSize.V != 0.01 || b.Asks[0].Price.V != 0.5 {
		t.Fatalf("%+v", b)
	}
}
