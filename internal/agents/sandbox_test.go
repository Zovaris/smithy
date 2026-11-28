package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/agents/sandbox"
	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/apiloop/tools"
	"github.com/Zovaris/smithy/internal/config"
)

func mustAPITools(t *testing.T, o Options, model string) *tools.Set {
	t.Helper()
	s, err := apiTools(o, model, "off", sandbox.HostProbe())
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
	if _, err := sandbox.APIShell("container", sandbox.HostProbe(), sandbox.SandboxPaths{}); !errors.Is(err, sandbox.ErrContainerRemoved) {
		t.Fatalf("err %v", err)
	}
}

func TestOpenCodePermissionAuto(t *testing.T) {
	p := OpenCodePermission(Options{Profile: "auto"}, "/home/dev")
	var m map[string]any
	if err := json.Unmarshal([]byte(p), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, p)
	}
	if m["webfetch"] != "deny" || m["external_directory"] != "deny" {
		t.Errorf("webfetch/external_directory not denied: %s", p)
	}
	bash := m["bash"].(map[string]any)
	if bash["*"] != "deny" || bash["git status *"] != "allow" || bash["curl *"] != "deny" || bash["*auth.json*"] != "deny" {
		t.Errorf("bash rules: %v", bash)
	}
	if strings.Index(p, `"*":"deny"`) > strings.Index(p, `"git status *":"allow"`) || strings.Index(p, `"git status *":"allow"`) > strings.Index(p, `"*auth.json*":"deny"`) {
		t.Errorf("order must be catch-all deny, allows, then denies (last match wins): %s", p)
	}
	read := m["read"].(map[string]any)
	if read["*"] != "allow" || read["/home/dev/.smithy/auth.json"] != "deny" || read["~/.smithy/auth.json"] != "deny" {
		t.Errorf("read rules: %v", read)
	}
	edit := m["edit"].(map[string]any)
	if edit["/home/dev/.smithy/auth.json"] != "deny" || edit["opencode.json"] != "deny" || edit[".opencode/**"] != "deny" || edit["**/.git/hooks/**"] != "deny" || m["websearch"] != "deny" {
		t.Errorf("edit rules: %v", m["edit"])
	}
}

func TestOpenCodePermissionReadOnly(t *testing.T) {
	p := OpenCodePermission(Options{ReadOnly: true}, "/home/dev")
	var m map[string]any
	if err := json.Unmarshal([]byte(p), &m); err != nil {
		t.Fatal(err)
	}
	if m["read"].(map[string]any)["/home/dev/.ssh/**"] != "deny" || m["edit"] != "deny" {
		t.Errorf("readonly rules: %s", p)
	}
	if OpenCodePermission(Options{}, "/home/dev") != "" {
		t.Error("an interactive run gets no permission override")
	}
}

func TestEnvOpenCodePermission(t *testing.T) {
	env := Env(nil, Spec{Harness: "opencode"}, Options{Profile: "auto"})
	if !hasPrefix(env, "OPENCODE_PERMISSION={") {
		t.Errorf("opencode auto run has no OPENCODE_PERMISSION: %v", env)
	}
	env = Env([]string{"OPENCODE_CONFIG_CONTENT={}", "OPENCODE_CONFIG=/x"}, Spec{Harness: "opencode"}, Options{ReadOnly: true})
	if hasPrefix(env, "OPENCODE_CONFIG_CONTENT=") || hasPrefix(env, "OPENCODE_CONFIG=") {
		t.Errorf("parent opencode config env kept: %v", env)
	}
	if hasPrefix(Env(nil, Spec{Harness: "codex"}, Options{Profile: "auto"}), "OPENCODE_PERMISSION=") {
		t.Error("codex run got OPENCODE_PERMISSION")
	}
}

func TestBuildNativeSandbox(t *testing.T) {
	env := func(string) string { return "" }
	o := Options{Profile: "auto", Sandbox: "native", SandboxSettings: `{"sandbox":{"enabled":true}}`}
	s, err := Build("claude", o, "p", nil, "", env)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(s.Argv, "--settings")
	if i < 0 || s.Argv[i+1] != o.SandboxSettings {
		t.Errorf("claude argv lacks --settings: %v", s.Argv)
	}
	s, _ = Build("codex", o, "p", nil, "", env)
	if !slices.Contains(s.Argv, "sandbox_workspace_write.network_access=false") || slices.Contains(s.Argv, "sandbox_workspace_write.network_access=true") {
		t.Errorf("codex native must turn network off: %v", s.Argv)
	}
	o.Sandbox = "off"
	s, _ = Build("claude", o, "p", nil, "", env)
	if slices.Contains(s.Argv, "--settings") {
		t.Errorf("off must not add --settings: %v", s.Argv)
	}
	s, _ = Build("codex", o, "p", nil, "", env)
	if !slices.Contains(s.Argv, "sandbox_workspace_write.network_access=true") {
		t.Errorf("codex off keeps network: %v", s.Argv)
	}
}

func TestEnvDropsSandboxRole(t *testing.T) {
	env := Env([]string{sandbox.SandboxRoleEnv + "=orchestrator", "A=1"}, Spec{Harness: "claude-cli"}, Options{})
	for _, kv := range env {
		if strings.HasPrefix(kv, sandbox.SandboxRoleEnv+"=") {
			t.Errorf("workers must not inherit %s", kv)
		}
	}
}

func TestRunSandboxFailsClosed(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	if err := config.Set(config.UserFile(), "auto.sandbox", "native"); err != nil {
		t.Fatal(err)
	}
	p, log := timeoutPrompt(t)
	t.Setenv("AGENT_CMD", "echo ran > "+log+".ran")
	if rc := AgentMain([]string{"--profile", "auto", "--log", log, p}); rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if _, err := os.Stat(log + ".ran"); err == nil {
		t.Error("worker ran without its sandbox")
	}
	t.Setenv(sandbox.SandboxRoleEnv, "orchestrator")
	if rc := AgentMain([]string{"--profile", "auto", "--log", log, p}); rc != 0 {
		t.Errorf("orchestrator rc = %d, want 0", rc)
	}
	t.Setenv(sandbox.SandboxRoleEnv, "")
	if err := config.Set(config.UserFile(), "auto.sandbox", "off"); err != nil {
		t.Fatal(err)
	}
	if rc := AgentMain([]string{"--profile", "auto", "--log", log, p}); rc != 0 {
		t.Errorf("off rc = %d, want 0", rc)
	}
	if err := os.WriteFile(config.UserFile(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Remove(log + ".ran")
	if rc := AgentMain([]string{"--profile", "auto", "--log", log, p}); rc != 1 {
		t.Errorf("unreadable settings rc = %d, want 1 (fails closed)", rc)
	}
	if _, err := os.Stat(log + ".ran"); err == nil {
		t.Error("worker ran with unreadable sandbox settings")
	}
}

func hasPrefix(env []string, p string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, p) {
			return true
		}
	}
	return false
}
