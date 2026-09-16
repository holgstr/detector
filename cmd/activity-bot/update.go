package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	updateRemote  = "origin"
	updateBranch  = "main"
	updateBinRel  = "bin/activity-bot"
	updateTimeout = 3 * time.Minute
)

// execSelf replaces the current process. Tests stub this.
var execSelf = syscall.Exec

type cmdRunner func(ctx context.Context, dir, name string, args ...string) (string, error)

func runCmd(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		if s == "" {
			return "", err
		}
		return s, fmt.Errorf("%s", s)
	}
	return s, nil
}

type updateResult struct {
	From    string
	To      string
	Changed bool
	Bin     string
}

var (
	updateMu      sync.Mutex
	updateRunning bool
)

func tryBeginUpdate() bool {
	updateMu.Lock()
	defer updateMu.Unlock()
	if updateRunning {
		return false
	}
	updateRunning = true
	return true
}

func endUpdate() {
	updateMu.Lock()
	updateRunning = false
	updateMu.Unlock()
}

func prepareUpdate(ctx context.Context) (updateResult, error) {
	return prepareUpdateWith(ctx, runCmd)
}

func prepareUpdateWith(ctx context.Context, run cmdRunner) (updateResult, error) {
	root, err := findRepoRoot()
	if err != nil {
		return updateResult{}, err
	}
	from, to, changed, err := syncRepo(ctx, root, run)
	if err != nil {
		return updateResult{}, err
	}
	res := updateResult{From: from, To: to, Changed: changed}
	if !changed {
		return res, nil
	}
	bin, err := buildBot(ctx, root, run)
	if err != nil {
		return res, err
	}
	res.Bin = bin
	return res, nil
}

func syncRepo(ctx context.Context, root string, run cmdRunner) (from, to string, changed bool, err error) {
	if err := assertCleanForUpdate(ctx, root, run); err != nil {
		return "", "", false, err
	}
	from, err = run(ctx, root, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", "", false, fmt.Errorf("git rev-parse: %w", err)
	}
	if _, err := run(ctx, root, "git", "fetch", updateRemote, updateBranch); err != nil {
		return from, "", false, fmt.Errorf("git fetch: %w", err)
	}
	branch, err := run(ctx, root, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return from, "", false, fmt.Errorf("git branch: %w", err)
	}
	if branch != updateBranch {
		if _, err := run(ctx, root, "git", "checkout", updateBranch); err != nil {
			return from, "", false, fmt.Errorf("git checkout %s: %w", updateBranch, err)
		}
	}
	if _, err := run(ctx, root, "git", "merge", "--ff-only", updateRemote+"/"+updateBranch); err != nil {
		return from, "", false, fmt.Errorf("git merge: %w", err)
	}
	to, err = run(ctx, root, "git", "rev-parse", "HEAD")
	if err != nil {
		return from, "", false, fmt.Errorf("git rev-parse: %w", err)
	}
	return from, to, from != to, nil
}

func buildBot(ctx context.Context, root string, run cmdRunner) (string, error) {
	bin := filepath.Join(root, filepath.FromSlash(updateBinRel))
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return "", err
	}
	if _, err := run(ctx, root, "go", "build", "-o", bin, "./cmd/activity-bot"); err != nil {
		return "", fmt.Errorf("go build: %w", err)
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func restartWithBinary(bin, announce string) error {
	args := restartArgs(bin, announce)
	env := os.Environ()
	if err := execSelf(bin, args, env); err != nil {
		return fmt.Errorf("exec %s: %w", bin, err)
	}
	return nil
}

func restartArgs(bin, announce string) []string {
	args := []string{bin}
	skip := false
	for i := 1; i < len(os.Args); i++ {
		if skip {
			skip = false
			continue
		}
		a := os.Args[i]
		if a == "-announce-update" {
			skip = true
			continue
		}
		if strings.HasPrefix(a, "-announce-update=") {
			continue
		}
		args = append(args, a)
	}
	if announce != "" {
		args = append(args, "-announce-update", announce)
	}
	return args
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && st.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not inside a git checkout (cwd %s); /update needs ~/detector", dir)
		}
		dir = parent
	}
}

func assertCleanForUpdate(ctx context.Context, root string, run cmdRunner) error {
	out, err := run(ctx, root, "git", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	var blocked []string
	for _, line := range strings.Split(out, "\n") {
		path := porcelainPath(line)
		if path == "" || isUpdateNoise(path) {
			continue
		}
		blocked = append(blocked, path)
	}
	if len(blocked) > 0 {
		return fmt.Errorf("working tree has local changes (%s); not pulling", strings.Join(blocked, ", "))
	}
	return nil
}

func porcelainPath(line string) string {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return ""
	}
	if len(line) < 2 {
		return ""
	}
	path := strings.TrimSpace(line[2:])
	if i := strings.Index(path, " -> "); i >= 0 {
		path = strings.TrimSpace(path[i+4:])
	}
	return strings.Trim(path, `"`)
}

func isUpdateNoise(path string) bool {
	path = filepath.ToSlash(path)
	switch {
	case path == "bot.log", strings.HasPrefix(path, "bot.log."):
		return true
	case path == "data", strings.HasPrefix(path, "data/"):
		return true
	case path == "bin", strings.HasPrefix(path, "bin/"):
		return true
	case path == "activity-bot", path == "./activity-bot":
		return true
	default:
		return false
	}
}

func shortSHA(rev string) string {
	rev = strings.TrimSpace(rev)
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}

func clipErr(err error) string {
	s := strings.TrimSpace(err.Error())
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}
