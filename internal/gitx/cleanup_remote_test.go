package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/testkit"
)

// A merged branch's remote copy is deleted; an unmerged one is kept.
func TestDropRemoteDeletesOnlyMergedBranches(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", bare).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v\n%s", err, out)
	}
	repo := t.TempDir()
	testkit.Git(t, repo, "init", "-q", "-b", "main")
	testkit.Git(t, repo, "remote", "add", "origin", bare)
	testkit.CommitFile(t, repo, "a.txt", "a.txt\n")
	testkit.Git(t, repo, "push", "-q", "origin", "main")

	testkit.Git(t, repo, "checkout", "-q", "-b", "feat/done")
	testkit.CommitFile(t, repo, "b.txt", "b.txt\n")
	testkit.Git(t, repo, "push", "-q", "origin", "feat/done")
	testkit.Git(t, repo, "checkout", "-q", "main")
	testkit.Git(t, repo, "merge", "-q", "--ff-only", "feat/done")
	testkit.Git(t, repo, "push", "-q", "origin", "main")

	testkit.Git(t, repo, "checkout", "-q", "-b", "feat/open")
	testkit.CommitFile(t, repo, "c.txt", "c.txt\n")
	testkit.Git(t, repo, "push", "-q", "origin", "feat/open")
	testkit.Git(t, repo, "checkout", "-q", "main")
	testkit.Git(t, repo, "fetch", "-q", "origin")
	wd, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	dropRemote("feat/done", "origin/main")
	dropRemote("feat/open", "origin/main")

	out, _ := exec.Command("git", "-C", bare, "branch", "--list").CombinedOutput()
	got := string(out)
	if strings.Contains(got, "feat/done") {
		t.Errorf("merged remote branch still there: %s", got)
	}
	if !strings.Contains(got, "feat/open") {
		t.Errorf("unmerged remote branch was deleted: %s", got)
	}
}

// A clean local base behind origin is fast-forwarded; a dirty checkout is left alone.
func TestFastForwardCleanBase(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", bare).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v\n%s", err, out)
	}
	repo := t.TempDir()
	testkit.Git(t, repo, "init", "-q", "-b", "main")
	testkit.Git(t, repo, "remote", "add", "origin", bare)
	testkit.CommitFile(t, repo, "a.txt", "a.txt\n")
	testkit.Git(t, repo, "push", "-q", "origin", "main")
	other := t.TempDir()
	testkit.Git(t, other, "clone", "-q", bare, ".")
	testkit.CommitFile(t, other, "b.txt", "b.txt\n")
	testkit.Git(t, other, "push", "-q", "origin", "main")
	testkit.Git(t, repo, "fetch", "-q", "origin")
	t.Chdir(repo)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	fastForward("origin/main")
	if git("rev-parse", "main") == git("rev-parse", "origin/main") {
		t.Fatal("dirty checkout was fast-forwarded")
	}
	testkit.Git(t, repo, "checkout", "-q", "--", "a.txt")
	fastForward("origin/main")
	if git("rev-parse", "main") != git("rev-parse", "origin/main") {
		t.Fatal("clean base not fast-forwarded")
	}
}
