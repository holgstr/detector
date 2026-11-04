package polymarket

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchCash(t *testing.T) {
	const addr = "0x448861155279dbf833d041b963e3ac854599e319"
	var gotTo, gotData string
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Method != "eth_call" || len(req.Params) == 0 {
			t.Fatalf("%+v", req)
		}
		var call struct {
			To   string `json:"to"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(req.Params[0], &call); err != nil {
			t.Fatal(err)
		}
		gotTo = call.To
		gotData = call.Data
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "0x0f4240",
		})
	}))
	defer ok.Close()

	c := NewClient()
	c.PolygonRPCs = []string{ok.URL}
	usd, err := c.FetchCash(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(usd-1) > 1e-9 {
		t.Fatalf("usd %v", usd)
	}
	if gotTo != pUSDAddress {
		t.Fatalf("to %s", gotTo)
	}
	want := balanceOfData(addr)
	if gotData != want {
		t.Fatalf("data %s want %s", gotData, want)
	}
}

func TestFetchCashFallsBack(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"result": "0x0"})
	}))
	defer good.Close()

	c := NewClient()
	c.PolygonRPCs = []string{bad.URL, good.URL}
	usd, err := c.FetchCash(context.Background(), "0x448861155279dbf833d041b963e3ac854599e319")
	if err != nil {
		t.Fatal(err)
	}
	if usd != 0 {
		t.Fatalf("usd %v", usd)
	}
}

func TestFetchCashRejectsName(t *testing.T) {
	c := NewClient()
	if _, err := c.FetchCash(context.Background(), "Flipadelphia"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseTokenAmount(t *testing.T) {
	usd, err := parseTokenAmount("0xf543399a2b", 6)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(usd-1053394.836011) > 1e-6 {
		t.Fatalf("%v", usd)
	}
}
