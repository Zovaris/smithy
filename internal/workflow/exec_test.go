//go:build !windows

package workflow

import (
	"context"
	"testing"
	"time"
)

func TestShell(t *testing.T) {
	out, exit, err := Shell(context.Background(), t.TempDir(), "echo hi; echo err >&2; exit 3")
	if err != nil || exit != 3 || out != "hi\nerr\n" {
		t.Fatalf("%q %d %v", out, exit, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, _, err := Shell(ctx, "", "sleep 5"); err == nil {
		t.Fatal("want timeout")
	}
}

func TestShellCapsOutputWhileRunning(t *testing.T) {
	out, _, err := Shell(context.Background(), "", "head -c 300000 /dev/zero | tr '\\0' x")
	if err != nil || len(out) > MaxOutput+100 {
		t.Fatalf("%d bytes, %v", len(out), err)
	}
}

func TestGuardDefaults(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	if Guard("git reset --hard HEAD~1") == "" {
		t.Fatal("guard should block git reset --hard")
	}
	if Guard("gh api repos/x/y/dependabot/alerts") != "" {
		t.Fatal("guard blocked a harmless command")
	}
}
