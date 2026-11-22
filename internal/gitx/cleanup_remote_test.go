package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A merged branch's remote copy is deleted; an unmerged one is kept.
func TestDropRemoteDeletesOnlyMergedBranches(t *testing.T) {
	bare := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", bare).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v\n%s", err, out)
	}
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "remote", "add", "origin", bare)
	commitFile(t, repo, "a.txt")
	gitIn(t, repo, "push", "-q", "origin", "main")

	gitIn(t, repo, "checkout", "-q", "-b", "feat/done")
	commitFile(t, repo, "b.txt")
	gitIn(t, repo, "push", "-q", "origin", "feat/done")
	gitIn(t, repo, "checkout", "-q", "main")
	gitIn(t, repo, "merge", "-q", "--ff-only", "feat/done")
	gitIn(t, repo, "push", "-q", "origin", "main")

	gitIn(t, repo, "checkout", "-q", "-b", "feat/open")
	commitFile(t, repo, "c.txt")
	gitIn(t, repo, "push", "-q", "origin", "feat/open")
	gitIn(t, repo, "checkout", "-q", "main")
	gitIn(t, repo, "fetch", "-q", "origin")
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
