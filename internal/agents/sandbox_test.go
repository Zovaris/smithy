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

func withCred(p Probe) Probe {
	p.Secret = func(svc string) string {
		if svc == "claude" {
			return "tok"
		}
		return ""
	}
	return p
}

func setNetwork(t *testing.T, n string) {
	t.Helper()
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	if n != "" {
		if err := config.Set(config.UserFile(), "auto.sandbox_network", n); err != nil {
			t.Fatal(err)
		}
	}
}

func TestContainerCheckNetworkAndCredential(t *testing.T) {
	for _, n := range []string{"", "none"} {
		setNetwork(t, n)
		if err := SandboxCheck("container", "claude", withCred(probe("linux", "docker"))); err == nil || err.Error() != ContainerNoNetwork {
			t.Errorf("network %q: want refusal, got %v", n, err)
		}
	}
	setNetwork(t, "bridge")
	if err := SandboxCheck("container", "claude", probe("linux", "docker")); err == nil || !strings.Contains(err.Error(), "claude setup-token, then smithy auth set claude") {
		t.Errorf("no credential: %v", err)
	}
	p := probe("linux", "docker")
	p.Getenv = func(k string) string {
		if k == "ANTHROPIC_API_KEY" {
			return "k"
		}
		return ""
	}
	if err := SandboxCheck("container", "claude", p); err != nil {
		t.Errorf("env credential: %v", err)
	}
}

func TestContainerSecrets(t *testing.T) {
	sec := func(svc string) string { return map[string]string{"claude": "c", "anthropic": "a", "openai": "o"}[svc] }
	host := []string{"PATH=/bin", "ANTHROPIC_API_KEY=env", "OPENAI_API_KEY=hostopenai"}
	cases := []struct {
		kind, model string
		want        []string
	}{
		{"claude", "", []string{"PATH=/bin", "ANTHROPIC_API_KEY=env", "CLAUDE_CODE_OAUTH_TOKEN=c"}},
		{"codex", "", []string{"PATH=/bin", "OPENAI_API_KEY=hostopenai"}},
		{"opencode", "opencode/big-pickle", []string{"PATH=/bin"}},
		{"opencode", "anthropic/claude-x", []string{"PATH=/bin", "ANTHROPIC_API_KEY=env"}},
		{"gemini", "", []string{"PATH=/bin"}},
	}
	for _, c := range cases {
		if got := ContainerSecrets(host, ContainerCreds(c.kind, c.model), sec); !slices.Equal(got, c.want) {
			t.Errorf("%s %s: env = %v, want %v", c.kind, c.model, got, c.want)
		}
	}
	if got := ContainerSecrets([]string{"OPENAI_API_KEY="}, ContainerCreds("codex", ""), sec); !slices.Equal(got, []string{"OPENAI_API_KEY=", "OPENAI_API_KEY=o"}) {
		t.Errorf("empty env value is filled from smithy auth: %v", got)
	}
	got := ContainerSecrets([]string{"PATH=/bin"}, ContainerCreds("claude", ""), sec)
	env, names := ContainerEnv(got)
	c := Container{Engine: "docker", Image: "i", Cwd: "/w", Env: names}
	argv := strings.Join(c.Argv([]string{"claude"}), " ")
	if strings.Contains(argv, "=c") || strings.Contains(argv, "=o") || !strings.Contains(argv, "-e CLAUDE_CODE_OAUTH_TOKEN ") {
		t.Errorf("secret values must stay off argv: %s", argv)
	}
	if !slices.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=c") {
		t.Errorf("value must be in the engine env: %v", env)
	}
}

func TestContainerEngineKiri(t *testing.T) {
	p := probe("darwin")
	p.Home = "/h"
	p.Exists = func(f string) bool { return f == "/h/.kiri/bin/docker" }
	if e := ContainerEngine(p); e != "/h/.kiri/bin/docker" {
		t.Errorf("engine = %q", e)
	}
}

func TestDockerHost(t *testing.T) {
	socks := map[string]bool{}
	p := Probe{Home: "/h", Exists: func(f string) bool { return socks[f] }, Getenv: func(string) string { return "" },
		Context: func(string) string { return "unix:///h/.orbstack/run/docker.sock" }}
	if set, ep := DockerHost("docker", p); set != "" || ep != "" {
		t.Errorf("no socket anywhere: %q %q", set, ep)
	}
	socks["/h/.kiri/run/docker.sock"], socks["/var/run/docker.sock"] = true, true
	if set, _ := DockerHost("docker", p); set != "unix:///h/.kiri/run/docker.sock" {
		t.Errorf("dead context falls back to kiri first: %q", set)
	}
	socks["/h/.orbstack/run/docker.sock"] = true
	if set, ep := DockerHost("docker", p); set != "" || ep != "unix:///h/.orbstack/run/docker.sock" {
		t.Errorf("live context is kept: %q %q", set, ep)
	}
	p.Getenv = func(string) string { return "tcp://x" }
	if set, ep := DockerHost("docker", p); set != "" || ep != "tcp://x" {
		t.Errorf("DOCKER_HOST wins: %q %q", set, ep)
	}
	if set, _ := DockerHost("podman", p); set != "" {
		t.Errorf("podman unchanged: %q", set)
	}
}

func TestSandboxCheck(t *testing.T) {
	setNetwork(t, "bridge")
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
		{"container", "claude", withCred(probe("linux", "podman")), true},
		{"container", "claude", probe("linux", "podman"), false},
		{"container", "codex", probe("linux", "podman"), false},
		{"container", "opencode", probe("linux", "podman"), true},
		{"container", "claude", probe("linux"), false},
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

func TestContainerEngine(t *testing.T) {
	if e := ContainerEngine(probe("linux", "docker", "podman")); e != "docker" {
		t.Errorf("engine = %q, want docker first", e)
	}
	if e := ContainerEngine(probe("linux", "podman")); e != "podman" {
		t.Errorf("engine = %q, want podman", e)
	}
	if e := ContainerEngine(probe("linux")); e != "" {
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

func TestOverlaps(t *testing.T) {
	dirs := []string{"/h/.smithy/accounts", "/acct/b"}
	for d, want := range map[string]bool{"/acct/b": true, "/acct/b/x": true, "/acct": true, "/acct/bb": false, "/w": false} {
		if overlaps(d, dirs) != want {
			t.Errorf("overlaps(%s) = %v", d, !want)
		}
	}
}

func TestContainerEnvNoPoolLogin(t *testing.T) {
	_, names := ContainerEnv([]string{"CLAUDE_CONFIG_DIR=/acct/a", "CODEX_HOME=/acct/b", "AGENT_MODEL=m"})
	if slices.Contains(names, "CLAUDE_CONFIG_DIR") || slices.Contains(names, "CODEX_HOME") {
		t.Errorf("pool account dirs never pass into a container: %v", names)
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
	c := Container{Engine: "docker", Image: "smithy-sandbox", Cwd: "/w/repo-feat", User: "501:20",
		Mounts: []string{"/w/repo-feat", "/w/repo/.git", "/h/.smithy/plans"}, ReadOnly: []string{"/w/repo/.git/hooks", "/w/repo/.git/config"},
		Env: []string{"AGENT_MODEL", "GH_TOKEN"}}
	got := c.Argv([]string{"claude", "-p", "--output-format", "json"})
	want := []string{"docker", "run", "--rm", "-i", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--read-only", "--tmpfs", "/tmp", "--pids-limit", "512", "--memory", "4g", "--network", "none", "--user", "501:20",
		"-w", "/w/repo-feat", "-v", "/w/repo-feat:/w/repo-feat", "-v", "/w/repo/.git:/w/repo/.git", "-v", "/h/.smithy/plans:/h/.smithy/plans",
		"-v", "/w/repo/.git/hooks:/w/repo/.git/hooks:ro", "-v", "/w/repo/.git/config:/w/repo/.git/config:ro",
		"-e", "HOME=/tmp", "-e", "AGENT_MODEL", "-e", "GH_TOKEN", "smithy-sandbox", "claude", "-p", "--output-format", "json"}
	if !slices.Equal(got, want) {
		t.Errorf("argv =\n%v\nwant\n%v", got, want)
	}
	c.Engine, c.Network, c.Memory = "podman", "bridge", "8g"
	got = c.Argv([]string{"claude"})
	for _, pair := range [][2]string{{"--network", "bridge"}, {"--memory", "8g"}} {
		if i := slices.Index(got, pair[0]); i < 0 || got[i+1] != pair[1] {
			t.Errorf("%s: %v", pair[0], got)
		}
	}
	if !slices.Contains(got, "--userns=keep-id") || slices.Contains(got, "--user") {
		t.Errorf("podman keeps the host uid with --userns=keep-id: %v", got)
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
		for _, want := range []string{"gemini", "host", "auto.sandbox container"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: refusal %q lacks %q", goos, err, want)
			}
		}
	}
}

func TestSandboxCheckAgentPoolContainer(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	setNetwork(t, "bridge")
	if err := os.WriteFile(pool.File(), []byte(`{"accounts":[{"name":"a","harness":"claude","dir":"/acct/a"},{"name":"b","harness":"codex","dir":"/acct/b"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := probe("linux", "docker")
	if err := SandboxCheckAgent("container", "pool", p); err == nil || !strings.Contains(err.Error(), "smithy auth set claude:a") {
		t.Errorf("a claude account needs its own token: %v", err)
	}
	p.Secret = func(svc string) string {
		if svc == "claude" {
			return "shared"
		}
		return ""
	}
	if err := SandboxCheckAgent("container", "pool", p); err == nil {
		t.Error("the shared claude token is not the account's login")
	}
	p.Secret = func(svc string) string {
		if svc == "claude:a" {
			return "tok"
		}
		return ""
	}
	if err := SandboxCheckAgent("container", "pool", p); err != nil {
		t.Errorf("every account can run: %v", err)
	}
	setNetwork(t, "none")
	if err := SandboxCheckAgent("container", "pool", p); err == nil || err.Error() != ContainerNoNetwork {
		t.Errorf("network still checked: %v", err)
	}
}

func TestPoolContainerCreds(t *testing.T) {
	if got := PoolContainerCreds(pool.Account{Name: "w", Harness: "claude"}); !slices.Equal(got, [][2]string{{"claude:w", "CLAUDE_CODE_OAUTH_TOKEN"}}) {
		t.Errorf("claude = %v", got)
	}
	if got := PoolContainerCreds(pool.Account{Name: "w", Harness: "codex"}); got != nil {
		t.Errorf("codex logs in from its dir: %v", got)
	}
}

func TestContainerMountsPoolAccount(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	setNetwork(t, "bridge")
	cwd, a, b := t.TempDir(), t.TempDir(), t.TempDir()
	body := `{"accounts":[{"name":"a","harness":"codex","dir":"` + a + `"},{"name":"b","harness":"codex","dir":"` + b + `"}]}`
	if err := os.WriteFile(pool.File(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := container(cwd, "", pool.Account{Name: "a", Harness: "codex", Dir: a})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.Mounts, a) || slices.Contains(c.Mounts, b) {
		t.Errorf("only the run's account dir is mounted: %v", c.Mounts)
	}
	if !slices.Contains(c.Env, "CODEX_HOME") {
		t.Errorf("the account's env var passes in: %v", c.Env)
	}
	for _, bad := range []string{filepath.Dir(a), os.Getenv("SMITHY_HOME")} {
		if _, err := container(cwd, "", pool.Account{Name: "x", Harness: "codex", Dir: bad}); err == nil {
			t.Errorf("a dir holding other accounts or smithy home must refuse: %s", bad)
		}
	}
	c, err = container(cwd, "", pool.Account{})
	if err != nil || slices.Contains(c.Mounts, a) || len(c.Env) != 0 {
		t.Errorf("no account, no account dir: %v %v %v", c.Mounts, c.Env, err)
	}
}

func TestContainerCheckDefaultImageHarnesses(t *testing.T) {
	setNetwork(t, "bridge")
	p := withCred(probe("linux", "docker"))
	for _, cli := range []string{"codex", "gemini", "cursor", "pi", "dsh"} {
		if err := SandboxCheck("container", cli, p); err == nil || !strings.Contains(err.Error(), "auto.sandbox_image") {
			t.Errorf("%s on the default image: want refusal naming auto.sandbox_image, got %v", cli, err)
		}
	}
	if err := SandboxCheck("container", "opencode", p); err != nil {
		t.Errorf("opencode is in the default image: %v", err)
	}
}

func TestContainerRefusesHostNetwork(t *testing.T) {
	setNetwork(t, "host")
	if err := SandboxCheck("container", "opencode", withCred(probe("linux", "docker"))); err == nil || !strings.Contains(err.Error(), "host") {
		t.Errorf("host network shares the host's: want refusal, got %v", err)
	}
}
