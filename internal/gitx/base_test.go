package gitx

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/testkit"
)

func TestResolveBasePrefersOrigin(t *testing.T) {
	up, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testkit.Git(t, up, "init", "-q")
	testkit.CommitFile(t, up, "a.txt", "a.txt\n")
	dir := filepath.Join(t.TempDir(), "c")
	testkit.Git(t, up, "clone", "-q", up, dir)
	testkit.CommitFile(t, up, "b.txt", "b.txt\n")
	testkit.Git(t, dir, "fetch", "-q")
	testkit.Git(t, dir, "switch", "-q", "-c", "feat", "origin/main")
	sha, label := ResolveBase(dir, "main")
	want, _ := exec.Command("git", "-C", dir, "rev-parse", "origin/main").Output()
	if sha != strings.TrimSpace(string(want)) || !strings.HasPrefix(label, "origin/main (merge-base ") {
		t.Fatalf("main: %s %q", sha, label)
	}
	if _, label := ResolveBase(dir, ""); !strings.HasPrefix(label, "origin/main (merge-base ") {
		t.Fatalf("default: %q", label)
	}
	if sha, label := ResolveBase(dir, "HEAD~1"); sha != "HEAD~1" || label != "HEAD~1" {
		t.Fatalf("revision not used as given: %s %q", sha, label)
	}
}

func TestResolveBaseWithoutRemoteIsWholeHistory(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testkit.Git(t, dir, "init", "-q")
	testkit.CommitFile(t, dir, "a.txt", "a.txt\n")
	if sha, _ := ResolveBase(dir, ""); sha != EmptyTree {
		t.Fatalf("no origin, no upstream: %s", sha)
	}
}

func TestResolveBaseRejectsOptionLikeBase(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testkit.Git(t, dir, "init", "-q")
	testkit.CommitFile(t, dir, "a.txt", "a.txt\n")
	if sha, label := ResolveBase(dir, "--output=x"); strings.HasPrefix(sha, "-") || !strings.Contains(label, "not a revision") {
		t.Fatalf("option-like base passed through: %s %q", sha, label)
	}
}

func TestResolveBaseLocalBranchWithoutOrigin(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testkit.Git(t, dir, "init", "-q")
	testkit.CommitFile(t, dir, "a.txt", "a.txt\n")
	testkit.Git(t, dir, "switch", "-q", "-c", "feat")
	testkit.CommitFile(t, dir, "b.txt", "b.txt\n")
	want, _ := exec.Command("git", "-C", dir, "rev-parse", "main").Output()
	if sha, label := ResolveBase(dir, "main"); sha != strings.TrimSpace(string(want)) || !strings.HasPrefix(label, "main (merge-base ") {
		t.Fatalf("local branch: %s %q", sha, label)
	}
}

func TestResolveBaseFallsBackToUpstream(t *testing.T) {
	up, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testkit.Git(t, up, "init", "-q")
	testkit.CommitFile(t, up, "a.txt", "a.txt\n")
	dir := filepath.Join(t.TempDir(), "c")
	testkit.Git(t, up, "clone", "-q", up, dir)
	testkit.Git(t, dir, "remote", "set-head", "origin", "--delete")
	testkit.CommitFile(t, dir, "b.txt", "b.txt\n")
	if _, label := ResolveBase(dir, ""); !strings.HasPrefix(label, "origin/main (merge-base ") {
		t.Fatalf("upstream fallback: %q", label)
	}
}
