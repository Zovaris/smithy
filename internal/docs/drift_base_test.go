package docs

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/testkit"
)

func staleCloneOnFeature(t *testing.T) string {
	t.Helper()
	up, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testkit.Git(t, up, "init", "-q")
	testkit.CommitFile(t, up, "a.txt", "a\n")
	dir := filepath.Join(t.TempDir(), "c")
	testkit.Git(t, up, "clone", "-q", up, dir)
	testkit.CommitFile(t, up, "up.go", "package x\n\nfunc Upstream() {}\n")
	testkit.Git(t, dir, "fetch", "-q")
	testkit.Git(t, dir, "switch", "-q", "-c", "feat", "origin/main")
	testkit.CommitFile(t, dir, "feat.go", "package x\n\nfunc Feature() {}\n")
	return dir
}

func runDrift(t *testing.T, dir string, args ...string) string {
	t.Helper()
	t.Chdir(dir)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()
	done := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	Drift(args)
	w.Close()
	return string(<-done)
}

func TestDriftBranchBaseUsesOrigin(t *testing.T) {
	out := runDrift(t, staleCloneOnFeature(t), "main")
	if !strings.Contains(out, "base: origin/main (merge-base ") {
		t.Fatalf("base not resolved to origin/main:\n%s", out)
	}
	if strings.Contains(out, "up.go") || !strings.Contains(out, "feat.go: Feature") {
		t.Fatalf("diffed against the stale local main:\n%s", out)
	}
}

func TestDriftDefaultBaseIsOriginDefaultBranch(t *testing.T) {
	out := runDrift(t, staleCloneOnFeature(t))
	if !strings.Contains(out, "base: origin/main (merge-base ") || strings.Contains(out, "up.go") {
		t.Fatalf("default base:\n%s", out)
	}
}
