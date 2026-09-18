package alert

import (
	"context"
	"strings"
	"testing"

	"github.com/holgstr/detector/internal/polymarket"
	"github.com/holgstr/detector/internal/sharps"
)

func TestAddUnaddByAddress(t *testing.T) {
	t.Cleanup(sharps.Reset)
	s := &State{}
	extra := sharps.Wallet{
		Address: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:    "newbie",
	}
	if s.AddWallet(extra) {
		t.Fatal("new extra should not be already tracked")
	}
	ApplyTracked(s)
	if len(sharps.Lookup("newbie")) != 1 {
		t.Fatal("live list missing extra")
	}

	got, ok := s.UnaddWallet(extra.Address)
	if !ok || got.Name != "newbie" {
		t.Fatalf("unadd extra %+v %v", got, ok)
	}
	ApplyTracked(s)
	if len(sharps.Lookup("newbie")) != 0 {
		t.Fatal("extra still live")
	}
}

func TestUnaddSeedThenAddBack(t *testing.T) {
	t.Cleanup(sharps.Reset)
	s := &State{}
	flip := sharps.Lookup("Flipadelphia")
	if len(flip) != 1 {
		t.Fatal(flip)
	}
	if _, ok := s.UnaddWallet(flip[0].Address); !ok {
		t.Fatal("unadd seed")
	}
	ApplyTracked(s)
	if len(sharps.Lookup("Flipadelphia")) != 0 {
		t.Fatal("seed still live")
	}
	if s.AddWallet(flip[0]) {
		t.Fatal("re-add should be new")
	}
	ApplyTracked(s)
	if len(sharps.Lookup("Flipadelphia")) != 1 {
		t.Fatal("seed not restored")
	}
}

func TestFormatTrackedListNamesOnly(t *testing.T) {
	got := FormatTrackedList([]sharps.Wallet{
		{Address: "0x1", Name: "Beta"},
		{Address: "0x2", Name: "alpha"},
	})
	if !strings.Contains(got, "Tracking 2 wallets") || !strings.Contains(got, "• alpha") || !strings.Contains(got, "• Beta") {
		t.Fatal(got)
	}
	if strings.Contains(got, "0x") {
		t.Fatal("wallet id leaked", got)
	}
	if !strings.Contains(FormatTrackedList(nil), "Not tracking") {
		t.Fatal("empty")
	}
}

type fakeUsers struct {
	byName map[string][]polymarket.UserProfile
	byAddr map[string]polymarket.UserProfile
}

func (f fakeUsers) SearchUsers(_ context.Context, query string) ([]polymarket.UserProfile, error) {
	return f.byName[strings.ToLower(strings.TrimSpace(query))], nil
}

func (f fakeUsers) FetchProfile(_ context.Context, address string) (polymarket.UserProfile, error) {
	addr := strings.ToLower(address)
	if p, ok := f.byAddr[addr]; ok {
		return p, nil
	}
	return polymarket.UserProfile{Address: addr}, nil
}

func TestResolveAddByNameUsesWalletID(t *testing.T) {
	api := fakeUsers{
		byName: map[string][]polymarket.UserProfile{
			"newsharp": {{Address: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Name: "NewSharp"}},
		},
	}
	w, errMsg := ResolveAddWallet(context.Background(), api, "NewSharp")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if w.Address != "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || w.Name != "NewSharp" {
		t.Fatalf("%+v", w)
	}
}

func TestResolveAddByAddressFetchesCurrentName(t *testing.T) {
	addr := "0xcccccccccccccccccccccccccccccccccccccccc"
	api := fakeUsers{
		byAddr: map[string]polymarket.UserProfile{
			addr: {Address: addr, Name: "CurrentName"},
		},
	}
	w, errMsg := ResolveAddWallet(context.Background(), api, "https://polymarket.com/profile/"+addr)
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if w.Address != addr || w.Name != "CurrentName" {
		t.Fatalf("%+v", w)
	}
}

func TestResolveUnaddPrefersLocalNameOverRenamedAPI(t *testing.T) {
	t.Cleanup(sharps.Reset)
	addr := "0xdddddddddddddddddddddddddddddddddddddddd"
	s := &State{}
	s.AddWallet(sharps.Wallet{Address: addr, Name: "OldName"})
	ApplyTracked(s)

	api := fakeUsers{
		byName: map[string][]polymarket.UserProfile{
			"oldname": {{Address: "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Name: "OldName"}},
			"newname": {{Address: addr, Name: "NewName"}},
		},
	}
	w, errMsg := ResolveUnaddWallet(context.Background(), api, "OldName")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if w.Address != addr {
		t.Fatalf("local name should map to stored wallet id, got %+v", w)
	}
	w, errMsg = ResolveUnaddWallet(context.Background(), api, "NewName")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if w.Address != addr {
		t.Fatalf("current Polymarket name should map to same id, got %+v", w)
	}
}

func TestHelpTextListsTrackedCommands(t *testing.T) {
	h := HelpText(100)
	for _, s := range []string{"/tracked", "/add", "/unadd"} {
		if !strings.Contains(h, s) {
			t.Fatal(h)
		}
	}
}
