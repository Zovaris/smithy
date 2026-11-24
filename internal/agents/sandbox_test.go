package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/config"
)

func probe(goos string, have ...string) Probe {
	return Probe{GOOS: goos, Look: func(b string) (string, error) {
		if slices.Contains(have, b) {
			return "/usr/bin/" + b, nil
		}
		return "", errors.New("not found")
	}}
}

func TestSandboxCheck(t *testing.T) {
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
		{"container", "claude", probe("linux", "podman"), true},
		{"container", "claude", probe("linux"), false},
		{"bogus", "claude", probe("linux"), false},
	}
	for _, c := range cases {
		if err := SandboxCheck(c.mode, c.cli, c.p); (err == nil) != c.ok {
			t.Errorf("SandboxCheck(%s, %s, %s) = %v, want ok=%v", c.mode, c.cli, c.p.GOOS, err, c.ok)
		}
	}
}

func TestContainerEngine(t *testing.T) {
	if e := ContainerEngine(probe("linux", "docker", "podman").Look); e != "docker" {
		t.Errorf("engine = %q, want docker first", e)
	}
	if e := ContainerEngine(probe("linux", "podman").Look); e != "podman" {
		t.Errorf("engine = %q, want podman", e)
	}
	if e := ContainerEngine(probe("linux").Look); e != "" {
		t.Errorf("engine = %q, want none", e)
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
	raw := NativeSettings(p, []string{"github.com"}, "darwin")
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
	if strings.Contains(NativeSettings(p, nil, "linux"), "enableWeakerNetworkIsolation") {
		t.Error("enableWeakerNetworkIsolation is macOS only")
	}
	if strings.Contains(NativeSettings(SandboxPaths{Cwd: "/w", Home: "/h", SmithyHome: "/h/.smithy"}, nil, "linux"), `""`) {
		t.Error("empty paths must be left out")
	}
}

func TestNativeSettingsSigning(t *testing.T) {
	p := SandboxPaths{Cwd: "/w", Home: "/h", SmithyHome: "/h/.smithy", SigningKey: "/h/.ssh/k.pub", AgentSocket: "/tmp/agent.sock"}
	raw := NativeSettings(p, nil, "darwin")
	if !strings.Contains(raw, `"allowRead":["/h/.ssh/k.pub"]`) || !strings.Contains(raw, `"allowUnixSockets":["/tmp/agent.sock"]`) {
		t.Errorf("signing key or agent socket missing: %s", raw)
	}
	p.SigningKey = "/h/.ssh/k"
	if strings.Contains(NativeSettings(p, nil, "linux"), "allowRead") || strings.Contains(NativeSettings(p, nil, "linux"), "allowUnixSockets") {
		t.Error("a private key path is never opened; unix sockets are a macOS key")
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

func TestContainerArgv(t *testing.T) {
	c := Container{Engine: "podman", Image: "smithy-sandbox", Network: "", Cwd: "/w/repo-feat",
		Mounts: []string{"/w/repo-feat", "/w/repo/.git", "/h/.smithy/plans"}, Env: []string{"AGENT_MODEL", "GH_TOKEN"}, User: "501:20"}
	got := c.Argv([]string{"claude", "-p", "--output-format", "json"})
	want := []string{"podman", "run", "--rm", "-i", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "501:20",
		"-w", "/w/repo-feat", "-v", "/w/repo-feat:/w/repo-feat", "-v", "/w/repo/.git:/w/repo/.git", "-v", "/h/.smithy/plans:/h/.smithy/plans",
		"-e", "HOME=/tmp", "-e", "AGENT_MODEL", "-e", "GH_TOKEN", "smithy-sandbox", "claude", "-p", "--output-format", "json"}
	if !slices.Equal(got, want) {
		t.Errorf("argv =\n%v\nwant\n%v", got, want)
	}
	c.Network, c.User = "none", ""
	got = c.Argv([]string{"claude"})
	if i := slices.Index(got, "--network"); i < 0 || got[i+1] != "none" || slices.Contains(got, "--user") {
		t.Errorf("network/user: %v", got)
	}
	for _, a := range got {
		if strings.Contains(a, "=") && strings.HasPrefix(a, "GH_TOKEN") {
			t.Errorf("a value leaked into argv: %s", a)
		}
	}
}

func TestContainerEnv(t *testing.T) {
	base := []string{"PATH=/bin", "AGENT_MODEL=m", "GH_TOKEN=host-token", "LINEAR_API_KEY=k", "AWS_SECRET_ACCESS_KEY=s", "HTTPS_PROXY=http://p:3128",
		"ANTHROPIC_API_KEY=a", ProfileEnv + "=auto", "SMITHY_SANDBOX_GH_TOKEN=scoped"}
	env, names := ContainerEnv(base)
	for _, n := range []string{"AGENT_MODEL", "HTTPS_PROXY", "ANTHROPIC_API_KEY", ProfileEnv, "GH_TOKEN"} {
		if !slices.Contains(names, n) {
			t.Errorf("names misses %s: %v", n, names)
		}
	}
	for _, n := range []string{"PATH", "LINEAR_API_KEY", "AWS_SECRET_ACCESS_KEY", "SMITHY_SANDBOX_GH_TOKEN"} {
		if slices.Contains(names, n) {
			t.Errorf("names must not pass %s: %v", n, names)
		}
	}
	if !slices.Contains(env, "GH_TOKEN=scoped") || slices.Contains(env, "GH_TOKEN=host-token") {
		t.Errorf("GH_TOKEN must be the scoped token: %v", env)
	}
	_, names = ContainerEnv([]string{"GH_TOKEN=host-token", "GITHUB_TOKEN=x"})
	if slices.Contains(names, "GH_TOKEN") || slices.Contains(names, "GITHUB_TOKEN") {
		t.Errorf("host GitHub tokens never pass without SMITHY_SANDBOX_GH_TOKEN: %v", names)
	}
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
}
