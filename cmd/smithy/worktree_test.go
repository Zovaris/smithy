package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeAdd(t *testing.T) {
	d := t.TempDir()
	gitIn := func(dir string, a ...string) {
		c := exec.Command("git", a...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", a, err, out)
		}
	}
	origin := filepath.Join(d, "origin.git")
	gitIn(d, "init", "-q", "--bare", "-b", "main", origin)
	repo := filepath.Join(d, "repo")
	gitIn(d, "clone", "-q", origin, repo)
	gitIn(repo, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "init")
	gitIn(repo, "push", "-q", "origin", "HEAD:main")
	env := append(os.Environ(), "SMITHY_HOME="+t.TempDir())
	out, errOut, code := run(t, repo, env, "worktree", "add", repo, "feat/TES-3-mul", "main")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	p := strings.TrimSpace(out)
	if !strings.HasSuffix(p, filepath.Join("worktrees", "repo-feat-TES-3-mul")) {
		t.Fatalf("path %q not under the worktrees dir", p)
	}
	if _, err := os.Stat(filepath.Join(p, ".git")); err != nil {
		t.Fatalf("no worktree at %s", p)
	}
	if _, _, code := run(t, repo, env, "worktree", "add", repo, "--upload-pack=x", "main"); code != 2 {
		t.Fatalf("dash branch accepted: %d", code)
	}
}
