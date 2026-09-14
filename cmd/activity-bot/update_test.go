package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRestartArgsStripsAnnounce(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"old-bin", "-interval", "20s", "-announce-update", "deadbeef", "-state", "data/x.json"}
	got := restartArgs("/repo/bin/activity-bot", "abc1234")
	want := []string{"/repo/bin/activity-bot", "-interval", "20s", "-state", "data/x.json", "-announce-update", "abc1234"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestIsUpdateNoise(t *testing.T) {
	if !isUpdateNoise("bot.log") || !isUpdateNoise("data/activity-bot-state.json") || !isUpdateNoise("bin/activity-bot") {
		t.Fatal("expected noise paths")
	}
	if isUpdateNoise("cmd/activity-bot/main.go") {
		t.Fatal("source is not noise")
	}
}

func TestPorcelainPath(t *testing.T) {
	if got := porcelainPath(" M cmd/activity-bot/main.go"); got != "cmd/activity-bot/main.go" {
		t.Fatal(got)
	}
	if got := porcelainPath("M  main.go"); got != "main.go" {
		t.Fatal(got)
	}
	if got := porcelainPath("M main.go"); got != "main.go" {
		t.Fatal(got)
	}
	if got := porcelainPath(`R  old.go -> new.go`); got != "new.go" {
		t.Fatal(got)
	}
}

func TestSyncRepoFastForward(t *testing.T) {
	gitOk(t)
	origin := t.TempDir()
	runGit(t, origin, "init", "-b", "main")
	runGit(t, origin, "config", "user.email", "t@example.com")
	runGit(t, origin, "config", "user.name", "t")
	write(t, filepath.Join(origin, "README"), "one\n")
	runGit(t, origin, "add", "README")
	runGit(t, origin, "commit", "-m", "one")

	work := t.TempDir()
	runGit(t, "", "clone", origin, work)
	from := runGit(t, work, "rev-parse", "HEAD")

	write(t, filepath.Join(origin, "README"), "two\n")
	runGit(t, origin, "add", "README")
	runGit(t, origin, "commit", "-m", "two")
	want := runGit(t, origin, "rev-parse", "HEAD")

	gotFrom, gotTo, changed, err := syncRepo(context.Background(), work, runCmd)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || gotFrom != from || gotTo != want {
		t.Fatalf("from=%s to=%s changed=%v", gotFrom, gotTo, changed)
	}

	_, _, changed, err = syncRepo(context.Background(), work, runCmd)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("second sync should be a no-op")
	}
}

func TestSyncRepoBlocksDirtySource(t *testing.T) {
	gitOk(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "t@example.com")
	runGit(t, dir, "config", "user.name", "t")
	write(t, filepath.Join(dir, "main.go"), "package main\n")
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "commit", "-m", "init")
	write(t, filepath.Join(dir, "main.go"), "package main\n// dirty\n")
	write(t, filepath.Join(dir, "bot.log"), "noise\n")

	_, _, _, err := syncRepo(context.Background(), dir, runCmd)
	if err == nil || !strings.Contains(err.Error(), "main.go") {
		t.Fatalf("want dirty main.go, got %v", err)
	}
	if strings.Contains(err.Error(), "bot.log") {
		t.Fatalf("bot.log should be ignored: %v", err)
	}
}

func gitOk(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s (%v)", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
