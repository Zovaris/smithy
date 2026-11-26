package plan

import (
	"path/filepath"
	"testing"

	"github.com/Zovaris/smithy/internal/testkit"
)

func TestStackBaseWithoutOriginHead(t *testing.T) {
	remote := t.TempDir()
	testkit.Git(t, remote, "init", "-q", "--bare")
	dir := newRepo(t)
	testkit.Git(t, dir, "remote", "add", "origin", remote)
	testkit.Git(t, dir, "push", "-q", "origin", "HEAD:main")
	if got := stackMap(Stack(dir))["base"]; got != "main" {
		t.Errorf("base = %q, want main", got)
	}
}

func TestStackGoTestSkipsCache(t *testing.T) {
	dir := newRepo(t)
	write(t, filepath.Join(dir, "go.mod"), "module x\n")
	if got := stackMap(Stack(dir))["test"]; got != "go test -count=1 ./..." {
		t.Errorf("test = %q", got)
	}
}
