package agents

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/pool"
)

func probe(goos string, have ...string) Probe {
	return Probe{GOOS: goos, Look: func(b string) (string, error) {
		if slices.Contains(have, b) {
			return "/usr/bin/" + b, nil
		}
		return "", errors.New("not found")
	}, Version: func(string) (string, error) { return "2.1.331 (Claude Code)\n", nil }}
}

func withVersion(p Probe, v string, err error) Probe {
	p.Version = func(string) (string, error) { return v, err }
	return p
}

func TestSandboxCheck(t *testing.T) {
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	cases := []struct {
		mode, cli string
		p         Probe
		ok        bool
	}{
		{"off", "claude", probe("linux"), true},
		{"native", "claude", probe("darwin", "/usr/bin/sandbox-exec"), true},
		{"native", "claude", probe("darwin"), false},
		{"native", "claude", probe("linux", "bwrap", "socat"), true},
		{"native", "claude", probe("linux", "bwrap"), false},
		{"native", "claude", probe("windows"), false},
		{"native", "/opt/bin/claude", probe("darwin", "/usr/bin/sandbox-exec"), true},
		{"native", "codex", probe("darwin"), true},
		{"native", "opencode", probe("darwin", "/usr/bin/sandbox-exec"), false},
		{"native", "custom", probe("darwin", "/usr/bin/sandbox-exec"), false},
		{"container", "claude", probe("linux", "docker", "podman"), false},
		{"container", "opencode", probe("linux", "docker", "podman"), false},
		{"bogus", "claude", probe("linux"), false},
		{"native", "claude", withVersion(probe("darwin", "/usr/bin/sandbox-exec"), "2.1.330 (Claude Code)", nil), false},
		{"native", "claude", withVersion(probe("darwin", "/usr/bin/sandbox-exec"), "2.2.0 (Claude Code)", nil), true},
		{"native", "claude", withVersion(probe("darwin", "/usr/bin/sandbox-exec"), "garbage", nil), false},
		{"native", "claude", withVersion(probe("darwin", "/usr/bin/sandbox-exec"), "", errors.New("boom")), false},
		{"native", "claude", Probe{GOOS: "darwin", Look: probe("darwin", "/usr/bin/sandbox-exec").Look}, false},
	}
	for _, c := range cases {
		if err := SandboxCheck(c.mode, c.cli, c.p); (err == nil) != c.ok {
			t.Errorf("SandboxCheck(%s, %s, %s) = %v, want ok=%v", c.mode, c.cli, c.p.GOOS, err, c.ok)
		}
	}
}

func TestSandboxDomains(t *testing.T) {
	got := SandboxDomains(" registry.npmjs.org, github.com,,proxy.golang.org ")
	want := []string{"github.com", "api.github.com", "api.anthropic.com", "api.linear.app", "registry.npmjs.org", "proxy.golang.org"}
	if !slices.Equal(got, want) {
		t.Errorf("domains = %v, want %v", got, want)
	}
}

func TestNativeSettings(t *testing.T) {
	p := SandboxPaths{Cwd: "/w/repo-feat", GitDir: "/w/repo/.git", Home: "/h", SmithyHome: "/h/.smithy", Warden: "/h/.smithy/warden",
		GoCache: "/h/Library/Caches/go-build", ReadOnly: []string{"/skills"}}
	raw := NativeSettings(p, []string{"github.com"}, "darwin", true)
	var s struct {
		Sandbox struct {
			Enabled, FailIfUnavailable, AllowUnsandboxedCommands, AutoAllowBashIfSandboxed, EnableWeakerNetworkIsolation *bool
			Network                                                                                                      struct {
				StrictAllowlist *bool
				AllowedDomains  []string
			}
			Filesystem struct{ AllowWrite, DenyWrite, DenyRead []string }
		}
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	b := s.Sandbox
	for name, v := range map[string]*bool{"enabled": b.Enabled, "failIfUnavailable": b.FailIfUnavailable, "strictAllowlist": b.Network.StrictAllowlist, "enableWeakerNetworkIsolation": b.EnableWeakerNetworkIsolation} {
		if v == nil || !*v {
			t.Errorf("%s must be true: %s", name, raw)
		}
	}
	for name, v := range map[string]*bool{"allowUnsandboxedCommands": b.AllowUnsandboxedCommands, "autoAllowBashIfSandboxed": b.AutoAllowBashIfSandboxed} {
		if v == nil || *v {
			t.Errorf("%s must be false: %s", name, raw)
		}
	}
	if !slices.Equal(b.Network.AllowedDomains, []string{"github.com"}) {
		t.Errorf("domains = %v", b.Network.AllowedDomains)
	}
	wantW := []string{"/w/repo-feat", "/w/repo/.git", "/h/.smithy/plans", "/h/.smithy/warden", "/h/Library/Caches/go-build"}
	if !slices.Equal(b.Filesystem.AllowWrite, wantW) {
		t.Errorf("allowWrite = %v, want %v", b.Filesystem.AllowWrite, wantW)
	}
	for _, d := range []string{"/h/.smithy/auth.json", "/h/.ssh", "/h/.aws", "/h/.netrc", "/h/.git-credentials"} {
		if !slices.Contains(b.Filesystem.DenyRead, d) {
			t.Errorf("denyRead misses %s: %v", d, b.Filesystem.DenyRead)
		}
	}
	for _, d := range []string{"/h/.smithy/auth.json", "/h/.smithy/config.json", "/w/repo/.git/hooks", "/w/repo/.git/config", "/w/repo-feat/.claude", "/skills"} {
		if !slices.Contains(b.Filesystem.DenyWrite, d) {
			t.Errorf("denyWrite misses %s: %v", d, b.Filesystem.DenyWrite)
		}
	}
	if strings.Contains(NativeSettings(p, nil, "linux", true), "enableWeakerNetworkIsolation") {
		t.Error("enableWeakerNetworkIsolation is macOS only")
	}
	if strings.Contains(NativeSettings(p, nil, "darwin", false), "enableWeakerNetworkIsolation") {
		t.Error("auto.sandbox_weaker_net false must leave enableWeakerNetworkIsolation out")
	}
	if strings.Contains(NativeSettings(SandboxPaths{Cwd: "/w", Home: "/h", SmithyHome: "/h/.smithy"}, nil, "linux", true), `""`) {
		t.Error("empty paths must be left out")
	}
}

func TestNativeSettingsSigning(t *testing.T) {
	p := SandboxPaths{Cwd: "/w", Home: "/h", SmithyHome: "/h/.smithy", SigningKey: "/h/.ssh/k.pub", AgentSocket: "/tmp/agent.sock"}
	raw := NativeSettings(p, nil, "darwin", true)
	if !strings.Contains(raw, `"allowRead":["/h/.ssh/k.pub"]`) || !strings.Contains(raw, `"allowUnixSockets":["/tmp/agent.sock"]`) {
		t.Errorf("signing key or agent socket missing: %s", raw)
	}
	p.SigningKey = "/h/.ssh/k"
	if strings.Contains(NativeSettings(p, nil, "linux", true), "allowRead") || strings.Contains(NativeSettings(p, nil, "linux", true), "allowUnixSockets") {
		t.Error("a private key path is never opened; unix sockets are a macOS key")
	}
}

func TestNativeSettingsPoolDirs(t *testing.T) {
	raw := NativeSettings(SandboxPaths{Cwd: "/w", Home: "/h", SmithyHome: "/h/.smithy", Secret: []string{"/h/.smithy/accounts", "/acct/b"}}, nil, "linux", true)
	var s struct {
		Sandbox struct {
			Filesystem struct{ DenyRead, DenyWrite []string }
		}
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"/h/.smithy/accounts", "/acct/b"} {
		if !slices.Contains(s.Sandbox.Filesystem.DenyRead, d) || !slices.Contains(s.Sandbox.Filesystem.DenyWrite, d) {
			t.Errorf("pool dir %s must be read- and write-denied: %s", d, raw)
		}
	}
}

func TestNativeDirs(t *testing.T) {
	add, ro := NativeDirs([]string{"/h/.smithy", "/skills", "/w"}, "/h/.smithy", "/h/.smithy/plans", "/h/.smithy/warden", "/w")
	if !slices.Equal(add, []string{"/h/.smithy/plans", "/h/.smithy/warden", "/skills"}) || !slices.Equal(ro, []string{"/skills"}) {
		t.Errorf("add = %v, readOnly = %v", add, ro)
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

// auto.sandbox* come from the user's settings only: a repo's .smithy/config.json can't turn the
// sandbox off, and an unreadable user file fails closed.
func TestSandboxModeUserOnly(t *testing.T) {
	user := filepath.Join(t.TempDir(), "c.json")
	t.Setenv("SMITHY_CONFIG", user)
	repo := t.TempDir()
	t.Chdir(repo)
	if _, err := ghxRun("git", "init", "-q"); err != nil {
		t.Skip("git not available")
	}
	if err := config.Set(user, "auto.sandbox", "native"); err != nil {
		t.Fatal(err)
	}
	if err := config.Set(filepath.Join(repo, ".smithy", "config.json"), "auto.sandbox", "off"); err != nil {
		t.Fatal(err)
	}
	if m, err := SandboxMode(); err != nil || m != "native" {
		t.Errorf("mode = %q, %v; want native (repo file ignored)", m, err)
	}
	if v, _ := SandboxSetting("auto.sandbox_domains", ""); v != "" {
		t.Errorf("domains = %q", v)
	}
	if err := os.WriteFile(user, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m, err := SandboxMode(); err == nil {
		t.Errorf("unreadable user settings must be an error, got mode %q", m)
	}
}

func ghxRun(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func TestEnvDropsSandboxRole(t *testing.T) {
	env := Env([]string{SandboxRoleEnv + "=orchestrator", "A=1"}, Spec{Harness: "claude-cli"}, Options{})
	for _, kv := range env {
		if strings.HasPrefix(kv, SandboxRoleEnv+"=") {
			t.Errorf("workers must not inherit %s", kv)
		}
	}
}

// A worker whose sandbox cannot start is not started (fails closed), unless it is the orchestrator.
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
	t.Setenv(SandboxRoleEnv, "orchestrator")
	if rc := AgentMain([]string{"--profile", "auto", "--log", log, p}); rc != 0 {
		t.Errorf("orchestrator rc = %d, want 0", rc)
	}
	t.Setenv(SandboxRoleEnv, "")
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

func TestSandboxCheckAgentPool(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	ok := probe("darwin", "/usr/bin/sandbox-exec")
	if err := SandboxCheckAgent("native", "pool", ok); err == nil {
		t.Error("an empty pool has nothing to sandbox")
	}
	write := func(body string) {
		if err := os.WriteFile(pool.File(), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"accounts":[{"name":"a","harness":"claude","dir":"/acct/a"},{"name":"b","harness":"codex","dir":"/acct/b"}]}`)
	if err := SandboxCheckAgent("native", "pool", ok); err != nil {
		t.Errorf("claude+codex pool: %v", err)
	}
	if err := SandboxCheckAgent("native", "pool", withVersion(ok, "2.0.0", nil)); err == nil || !strings.Contains(err.Error(), "older") {
		t.Errorf("claude accounts get the version floor: %v", err)
	}
	if err := SandboxCheckAgent("off", "pool", ok); err != nil {
		t.Errorf("off: %v", err)
	}
	if dirs, err := PoolDirs(); err != nil || !slices.Contains(dirs, "/acct/a") {
		t.Errorf("PoolDirs = %v, %v", dirs, err)
	}
	write("{broken")
	if _, err := PoolDirs(); err == nil {
		t.Error("unreadable pool.json must be an error")
	}
	if err := SandboxCheckAgent("native", "pool", ok); err == nil {
		t.Error("unreadable pool.json must refuse")
	}
}

func TestNativeGitEnvUsesHTTPS(t *testing.T) {
	env := NativeGitEnv([]string{"GIT_CONFIG_COUNT=1"})
	got := strings.Join(env, "\n")
	for _, want := range []string{"GIT_CONFIG_KEY_1=url.https://github.com/.insteadOf", "GIT_CONFIG_VALUE_1=git@github.com:", "GIT_CONFIG_VALUE_4=!gh auth git-credential", "GIT_CONFIG_COUNT=5"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
}

func TestSandboxCheckGeminiNativeRefused(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		err := SandboxCheck("native", "gemini", probe(goos, "/usr/bin/sandbox-exec", "gemini"))
		if err == nil {
			t.Fatalf("%s: gemini native must be refused", goos)
		}
		for _, want := range []string{"gemini", "host"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: refusal %q lacks %q", goos, err, want)
			}
		}
	}
}

func TestSandboxModeContainerRemoved(t *testing.T) {
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	if err := config.Set(config.UserFile(), "auto.sandbox", "container"); err != nil {
		t.Fatal(err)
	}
	if m, err := SandboxMode(); !errors.Is(err, ErrContainerRemoved) || m != "" {
		t.Fatalf("SandboxMode() = %q, %v; want ErrContainerRemoved", m, err)
	}
}
