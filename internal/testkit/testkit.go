// Package testkit holds helpers shared by tests only.
package testkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Git runs git in dir with a fixed identity and main as the default branch, failing the test on error.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "init.defaultBranch=main"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// CommitFile writes text to dir/name and commits it with name as the message.
func CommitFile(t testing.TB, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "add", name)
	Git(t, dir, "commit", "-q", "-m", name)
}

// Repo returns a fresh repository in a symlink-resolved temp dir.
func Repo(t testing.TB, initArgs ...string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	Git(t, dir, append([]string{"init", "-q"}, initArgs...)...)
	return dir
}
