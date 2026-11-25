package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func testKit(t *testing.T) Kit {
	skill := filepath.Join(t.TempDir(), "hush")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: hush\n---\n"), 0o644)
	return Kit{
		MCP:          map[string]MCPServer{"smithy": {Command: "/bin/smithy", Args: []string{"mcp"}, Env: map[string]string{"A": "1"}}},
		Skills:       []string{skill},
		SystemPrompt: "be brief",
		Settings:     `{"model":"x"}`,
	}
}

func readJSON(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return m
}

func argAfter(a []string, flag string) string {
	if i := slices.Index(a, flag); i >= 0 && i+1 < len(a) {
		return a[i+1]
	}
	return ""
}

func TestInjectClaude(t *testing.T) {
	dir := t.TempDir()
	in, err := Inject("claude", testKit(t), dir, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Unsupported) != 0 || len(in.Env) != 0 {
		t.Fatalf("claude supports every piece: %+v", in)
	}
	if !slices.Contains(in.Args, "--strict-mcp-config") || argAfter(in.Args, "--append-system-prompt") != "be brief" || readJSON(t, argAfter(in.Args, "--settings"))["model"] != "x" {
		t.Fatalf("args %q", in.Args)
	}
	m := readJSON(t, argAfter(in.Args, "--mcp-config"))
	srv := m["mcpServers"].(map[string]any)["smithy"].(map[string]any)
	if srv["command"] != "/bin/smithy" || !strings.HasPrefix(argAfter(in.Args, "--mcp-config"), dir) {
		t.Fatalf("mcp config %v", m)
	}
	plug := argAfter(in.Args, "--plugin-dir")
	if readJSON(t, filepath.Join(plug, ".claude-plugin", "plugin.json"))["name"] != "smithy-kit" {
		t.Fatal("plugin manifest")
	}
	if _, err := os.Stat(filepath.Join(plug, "skills", "hush", "SKILL.md")); err != nil {
		t.Fatal("skill not in plugin:", err)
	}
}

func TestInjectCodex(t *testing.T) {
	dir, src := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(src, "auth.json"), []byte("{}"), 0o600)
	in, err := Inject("codex", testKit(t), dir, env(map[string]string{"CODEX_HOME": src}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.Args, []string{"--profile", "smithy"}) || !reflect.DeepEqual(in.Unsupported, []string{"settings"}) {
		t.Fatalf("%+v", in)
	}
	home := filepath.Join(dir, "codex-home")
	if !slices.Contains(in.Env, "CODEX_HOME="+home) {
		t.Fatalf("env %q", in.Env)
	}
	b, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	for _, w := range []string{"[profiles.smithy]", `developer_instructions = "be brief"`, "[mcp_servers.smithy]", `command = "/bin/smithy"`, `args = ["mcp"]`, "[mcp_servers.smithy.env]", `A = "1"`} {
		if !strings.Contains(string(b), w) {
			t.Errorf("config.toml lacks %q:\n%s", w, b)
		}
	}
	if l, _ := os.Readlink(filepath.Join(home, "auth.json")); l != filepath.Join(src, "auth.json") {
		t.Errorf("auth not linked from the user's home: %q", l)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "hush", "SKILL.md")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(src, "config.toml")); err == nil {
		t.Error("touched the user's CODEX_HOME")
	}
}

func TestInjectOpenCode(t *testing.T) {
	dir := t.TempDir()
	in, err := Inject("opencode", testKit(t), dir, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "opencode")
	if len(in.Args) != 0 || !slices.Contains(in.Env, "OPENCODE_CONFIG_DIR="+cfg) || !reflect.DeepEqual(in.Unsupported, []string{"settings"}) {
		t.Fatalf("%+v", in)
	}
	m := readJSON(t, filepath.Join(cfg, "opencode.json"))
	srv := m["mcp"].(map[string]any)["smithy"].(map[string]any)
	if srv["type"] != "local" || !reflect.DeepEqual(srv["command"], []any{"/bin/smithy", "mcp"}) || srv["environment"].(map[string]any)["A"] != "1" {
		t.Fatalf("mcp %v", srv)
	}
	ins := m["instructions"].([]any)
	if b, _ := os.ReadFile(ins[0].(string)); string(b) != "be brief" {
		t.Fatalf("instructions %v", ins)
	}
	if _, err := os.Stat(filepath.Join(cfg, "skills", "hush", "SKILL.md")); err != nil {
		t.Error(err)
	}
}

func TestInjectGemini(t *testing.T) {
	dir := t.TempDir()
	in, err := Inject("gemini", testKit(t), dir, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "gemini-settings.json")
	if len(in.Args) != 0 || !slices.Contains(in.Env, "GEMINI_CLI_SYSTEM_SETTINGS_PATH="+p) || !reflect.DeepEqual(in.Unsupported, []string{"skills", "system-prompt"}) {
		t.Fatalf("%+v", in)
	}
	m := readJSON(t, p)
	if m["model"] != "x" || m["mcpServers"].(map[string]any)["smithy"].(map[string]any)["command"] != "/bin/smithy" {
		t.Fatalf("settings %v", m)
	}
}

func TestInjectPi(t *testing.T) {
	k := testKit(t)
	in, err := Inject("pi", k, t.TempDir(), env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--skill", k.Skills[0], "--append-system-prompt", "be brief"}
	if !reflect.DeepEqual(in.Args, want) || !reflect.DeepEqual(in.Unsupported, []string{"mcp", "settings"}) {
		t.Fatalf("%+v", in)
	}
}

func TestInjectOthersReportEverything(t *testing.T) {
	for _, cli := range []string{"cursor", "dsh", "custom"} {
		in, err := Inject(cli, testKit(t), t.TempDir(), env(nil))
		if err != nil || len(in.Args)+len(in.Env) != 0 || !reflect.DeepEqual(in.Unsupported, []string{"mcp", "settings", "skills", "system-prompt"}) {
			t.Errorf("%s: %+v %v", cli, in, err)
		}
	}
	in, _ := Inject("claude", Kit{}, t.TempDir(), env(nil))
	if len(in.Args)+len(in.Env)+len(in.Unsupported) != 0 {
		t.Errorf("empty kit adds %+v", in)
	}
}

func TestInjectRejectsBadNames(t *testing.T) {
	k := Kit{MCP: map[string]MCPServer{"a]\nx": {Command: "c"}}}
	if _, err := Inject("codex", k, t.TempDir(), env(nil)); err == nil {
		t.Fatal("unsafe server name accepted")
	}
	if _, err := Inject("claude", Kit{Settings: "{"}, t.TempDir(), env(nil)); err == nil {
		t.Fatal("bad settings JSON accepted")
	}
}

func TestBuildAppliesInjection(t *testing.T) {
	o := Options{Inject: &Injection{Args: []string{"--strict-mcp-config"}, Env: []string{"CODEX_HOME=/k"}}}
	s, _ := Build("claude", o, "P", []string{"--x"}, "", env(nil))
	if !slices.Contains(s.Argv, "--strict-mcp-config") || s.Argv[len(s.Argv)-1] != "--x" {
		t.Fatalf("%q", s.Argv)
	}
	s, _ = Build("codex", o, "P", nil, "", env(nil))
	if s.Argv[len(s.Argv)-1] != "-" || !slices.Contains(s.Argv, "--strict-mcp-config") {
		t.Fatalf("%q", s.Argv)
	}
	if e := Env(nil, s, o); !slices.Contains(e, "CODEX_HOME=/k") {
		t.Fatalf("env %q", e)
	}
}

func TestPoolGetenvPrefersPoolAccount(t *testing.T) {
	g := PoolGetenv(Options{PoolEnv: []string{"CODEX_HOME=/pool/b"}}, env(map[string]string{"CODEX_HOME": "/default", "X": "1"}))
	if g("CODEX_HOME") != "/pool/b" || g("X") != "1" {
		t.Fatal(g("CODEX_HOME"), g("X"))
	}
}

func TestMergeSettingsReadsKitFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(p, []byte(`{"model":"x","sandbox":1}`), 0o600)
	var m map[string]any
	json.Unmarshal([]byte(mergeSettings(p, `{"sandbox":2}`)), &m)
	if m["model"] != "x" || m["sandbox"] != 2.0 {
		t.Fatal(m)
	}
}

func TestAgentRemovesTheTempKitAfterTheRun(t *testing.T) {
	old := KitMCP
	t.Cleanup(func() { KitMCP = old })
	KitMCP = []string{"/bin/smithy", "mcp"}
	p, _, _ := fakePoolClaude(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if rc := AgentMain([]string{"--timeout", "0", p}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if left, _ := filepath.Glob(filepath.Join(tmp, "smithy-kit-*")); len(left) != 0 {
		t.Fatalf("temp kit left behind: %v", left)
	}
}

func TestRunKit(t *testing.T) {
	old := KitMCP
	t.Cleanup(func() { KitMCP = old })
	KitMCP = nil
	if in, err := RunKit("claude", Options{}, os.Getenv); in != nil || err != nil {
		t.Fatalf("skills build injected: %v %v", in, err)
	}
	KitMCP = []string{"/bin/smithy", "mcp"}
	home := t.TempDir()
	t.Setenv("WARDEN_HOME", home)
	log := filepath.Join(home, "b1", "i1.log")
	os.MkdirAll(filepath.Dir(log), 0o755)
	in, err := RunKit("claude", Options{Log: log, ReadOnly: true}, os.Getenv)
	if err != nil || in == nil {
		t.Fatalf("RunKit: %v %v", in, err)
	}
	b, err := os.ReadFile(filepath.Join(log+".kit", "claude-mcp.json"))
	var cfg struct {
		MCPServers map[string]struct {
			Args []string          `json:"args"`
			Env  map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	json.Unmarshal(b, &cfg)
	if s := cfg.MCPServers["smithy"]; err != nil || s.Env["SMITHY_MCP_CARD"] != "b1/i1" || !slices.Contains(s.Args, "--readonly") {
		t.Fatalf("mcp config: %s %v", b, err)
	}
	oldGuard := KitGuard
	t.Cleanup(func() { KitGuard = oldGuard })
	KitGuard = func(exe string) string {
		return `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"` + exe + ` guard hook"}]}]}}`
	}
	in, err = RunKit("claude", Options{Log: log}, os.Getenv)
	if b, _ := os.ReadFile(filepath.Join(log+".kit", "claude-settings.json")); err != nil || !strings.Contains(string(b), "/bin/smithy guard hook") || !slices.Contains(in.Args, "--settings") {
		t.Fatalf("guard hook not injected: %s %v", b, err)
	}
	if in, _ := RunKit("codex", Options{Log: log}, os.Getenv); in != nil && slices.Contains(in.Unsupported, "settings") {
		t.Fatal("claude-only hook offered to codex")
	}
	KitGuard = oldGuard
	if in, _ := RunKit("claude", Options{Sandbox: "native"}, os.Getenv); in == nil {
		t.Fatal("native sandbox run lost its kit")
	}
	if in, _ := RunKit("custom", Options{}, os.Getenv); in != nil {
		t.Fatal("injected into a custom AGENT_CMD")
	}
}
