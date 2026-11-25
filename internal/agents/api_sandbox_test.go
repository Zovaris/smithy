package agents

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/apiloop/tools"
	"github.com/Zovaris/smithy/internal/config"
)

func mustAPITools(t *testing.T, o Options, model string) *tools.Set {
	t.Helper()
	s, err := apiTools(o, model, "off", HostProbe())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAPIReadOnlyOffersOnlyReadTools(t *testing.T) {
	fake := &apiFake{}
	if _, _, rc, _ := apiRun(t, fake, "--backend", "api", "--readonly", "--model", "anthropic/claude-sonnet-4"); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var names []string
	for _, s := range fake.got.Tools {
		names = append(names, s.Name)
	}
	for _, n := range names {
		if n != "read" && n != "glob" && n != "grep" {
			t.Fatalf("read-only run offers %v", names)
		}
	}
	set := mustAPITools(t, Options{Cwd: t.TempDir(), ReadOnly: true}, "gpt-5")
	for _, c := range []string{"write", "edit", "shell", "apply_patch", "fetch"} {
		if ok, _ := set.Permit(apiloop.ToolCall{Name: c, Args: []byte(`{"path":"a","command":"git status","url":"https://github.com/"}`)}); ok {
			t.Errorf("%s permitted in a read-only run", c)
		}
	}
}

func TestAPIContainerSandboxRefused(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "c.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	if err := config.Set(cfg, "auto.sandbox", "container"); err != nil {
		t.Fatal(err)
	}
	fake := &apiFake{}
	if _, _, rc, _ := apiRun(t, fake, "--backend", "api", "--model", "anthropic/claude-sonnet-4"); rc != 1 || fake.got.Model != "" {
		t.Fatalf("container mode: rc=%d, loop ran=%v", rc, fake.got.Model != "")
	}
	if _, err := apiShellSandbox("container", HostProbe(), SandboxPaths{}); !errors.Is(err, ErrContainerRemoved) {
		t.Fatalf("err %v", err)
	}
}

func TestAPIShellSandboxOffIsUnchanged(t *testing.T) {
	if w, err := apiShellSandbox("off", HostProbe(), SandboxPaths{}); w != nil || err != nil {
		t.Fatalf("off: wrap=%v err=%v", w != nil, err)
	}
	p := Probe{GOOS: "plan9", Look: exec.LookPath}
	if _, err := apiShellSandbox("native", p, SandboxPaths{}); err == nil {
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
	wrap, err := apiShellSandbox("native", HostProbe(), SandboxPaths{Cwd: realPath(cwd)})
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
