package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseWalletAddress(t *testing.T) {
	addr := "0x448861155279dbf833d041b963e3ac854599e319"
	got, ok := ParseWalletAddress(addr)
	if !ok || got != addr {
		t.Fatalf("%s %v", got, ok)
	}
	got, ok = ParseWalletAddress("https://polymarket.com/profile/" + addr + "?foo=1")
	if !ok || got != addr {
		t.Fatalf("url %s %v", got, ok)
	}
	if _, ok := ParseWalletAddress("Flipadelphia"); ok {
		t.Fatal("name is not a wallet")
	}
	if _, ok := ParseWalletAddress("0x1234"); ok {
		t.Fatal("short")
	}
}

func TestPickUsersExactBeatsPrefix(t *testing.T) {
	hits := []UserProfile{
		{Address: "0x1", Name: "Flipadelphia"},
		{Address: "0x2", Name: "Flip"},
		{Address: "0x3", Name: "kingofcoinflips"},
	}
	got := PickUsers("Flip", hits)
	if len(got) != 1 || got[0].Name != "Flip" {
		t.Fatalf("%+v", got)
	}
	got = PickUsers("flipadel", hits)
	if len(got) != 1 || got[0].Name != "Flipadelphia" {
		t.Fatalf("prefix %+v", got)
	}
}

func TestSearchUsersLeaderboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/leaderboard" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.URL.Query().Get("userName") != "Flipadelphia" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]leaderboardEntry{{
			ProxyWallet: "0x448861155279dbf833d041b963e3ac854599e319",
			UserName:    "Flipadelphia",
		}})
	}))
	defer srv.Close()

	c := NewClient()
	c.DataBase = srv.URL
	got, err := c.SearchUsers(context.Background(), "Flipadelphia")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Flipadelphia" || got[0].Address != "0x448861155279dbf833d041b963e3ac854599e319" {
		t.Fatalf("%+v", got)
	}
}

func TestFetchProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/public-profile" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(publicProfileResponse{
			ProxyWallet: "0x448861155279dbf833d041b963e3ac854599e319",
			Name:        "Flipadelphia",
		})
	}))
	defer srv.Close()

	c := NewClient()
	c.GammaBase = srv.URL
	got, err := c.FetchProfile(context.Background(), "0x448861155279dbf833d041b963e3ac854599e319")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Flipadelphia" {
		t.Fatalf("%+v", got)
	}
}
