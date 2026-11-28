package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/apiloop/tools"
)

func TestAPIShellSandboxOffIsUnchanged(t *testing.T) {
	if w, err := APIShell("off", HostProbe(), SandboxPaths{}); w != nil || err != nil {
		t.Fatalf("off: wrap=%v err=%v", w != nil, err)
	}
	p := Probe{GOOS: "plan9", Look: exec.LookPath}
	if _, err := APIShell("native", p, SandboxPaths{}); err == nil {
		t.Fatal("native on an unsupported OS must fail closed")
	}
}

func TestSeatbeltProfileAndBwrapArgs(t *testing.T) {
	fs := map[string][]string{"allowWrite": {"/w"}, "denyWrite": {"/w/.claude"}, "denyRead": {"/h/.ssh"}}
	prof := SeatbeltProfile(fs)
	for _, want := range []string{"(deny network*)", "(deny file-write*)\n", `(allow file-write* (subpath "/w"))`, `(deny file-write* (subpath "/w/.claude"))`, `(deny file-read* (subpath "/h/.ssh"))`} {
		if !strings.Contains(prof, want) {
			t.Errorf("profile lacks %s:\n%s", want, prof)
		}
	}
	a := strings.Join(BwrapArgs(fs, func(string) bool { return true }), " ")
	for _, want := range []string{"--ro-bind / /", "--unshare-net", "--bind /w /w", "--ro-bind /w/.claude /w/.claude", "--tmpfs /h/.ssh"} {
		if !strings.Contains(a, want) {
			t.Errorf("bwrap args lack %s: %s", want, a)
		}
	}
}

func TestAPINativeSandboxConfinesShellWrites(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt check runs on macOS")
	}
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		t.Skip("no sandbox-exec")
	}
	t.Setenv("SMITHY_HOME", t.TempDir())
	home, _ := os.UserHomeDir()
	cwd, outside := t.TempDir(), home
	t.Cleanup(func() { os.Remove(filepath.Join(home, ".smithy-sandbox-test-out")) })
	wrap, err := APIShell("native", HostProbe(), SandboxPaths{Cwd: RealPath(cwd)})
	if err != nil {
		t.Fatal(err)
	}
	s := &tools.Set{Cwd: cwd, Allow: []string{"Bash"}, Wrap: wrap}
	defer s.Close()
	in := filepath.Join(cwd, "in")
	out := filepath.Join(outside, ".smithy-sandbox-test-out")
	if _, err := s.Call(context.Background(), apiloop.ToolCall{Name: "shell", Args: []byte(`{"command":"touch ` + in + `"}`)}); err != nil {
		t.Fatalf("write inside the worktree: %v", err)
	}
	if _, err := s.Call(context.Background(), apiloop.ToolCall{Name: "shell", Args: []byte(`{"command":"touch ` + out + `"}`)}); err == nil {
		if _, serr := os.Stat(out); serr == nil {
			t.Fatal("the sandboxed shell wrote outside the worktree")
		}
	}
}
