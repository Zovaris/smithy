package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestBuild(t *testing.T) {
	none := env(nil)
	cases := []struct {
		cli   string
		o     Options
		extra []string
		git   string
		want  []string
		stdin bool
		h     string
	}{
		{"claude", Options{Model: "opus"}, []string{"--x"}, "", []string{"claude", "-p", "--permission-mode", "auto", "--model", "opus", "--x"}, true, "claude-cli"},
		{"claude", Options{ReadOnly: true}, nil, "", []string{"claude", "-p", "--permission-mode", "plan"}, true, "claude-cli"},
		{"codex", Options{}, nil, "/r/.git", []string{"codex", "exec", "--sandbox", "workspace-write", "-c", "sandbox_workspace_write.network_access=true", "--add-dir", "/r/.git", "-"}, true, "codex"},
		{"codex", Options{ReadOnly: true, Model: "m"}, nil, "", []string{"codex", "exec", "--sandbox", "read-only", "-m", "m", "-"}, true, "codex"},
		{"opencode", Options{}, nil, "", []string{"opencode", "run", "--standalone", "--archived", "--auto", "P"}, false, "opencode"},
		{"opencode", Options{ReadOnly: true, Model: "a/b"}, nil, "", []string{"opencode", "run", "--standalone", "--archived", "--agent", "plan", "-m", "a/b", "P"}, false, "opencode"},
		{"gemini", Options{ReadOnly: true}, nil, "", []string{"gemini", "--approval-mode", "plan", "-p", "P"}, false, "gemini"},
		{"cursor", Options{Model: "m"}, nil, "", []string{"agent", "-p", "--force", "--model", "m", "P"}, false, "cursor"},
		{"pi", Options{ReadOnly: true}, nil, "", []string{"pi", "-p", "--tools", "read", "P"}, false, "pi"},
		{"claude", Options{Effort: "medium"}, nil, "", []string{"claude", "-p", "--permission-mode", "auto", "--effort", "medium"}, true, "claude-cli"},
		{"codex", Options{ReadOnly: true, Effort: "low"}, nil, "", []string{"codex", "exec", "--sandbox", "read-only", "-c", "model_reasoning_effort=low", "-"}, true, "codex"},
		{"gemini", Options{Effort: "medium"}, nil, "", []string{"gemini", "--approval-mode", "yolo", "-p", "P"}, false, "gemini"},
		{"dsh", Options{ReadOnly: true, Model: "ignored"}, nil, "", []string{"dsh", "headless", "--approval-policy", "never", "--permission-mode", "read-only", "-"}, true, "dsh"},
	}
	for _, c := range cases {
		s, err := Build(c.cli, c.o, "P", c.extra, c.git, none)
		if err != nil || !reflect.DeepEqual(s.Argv, c.want) || s.PromptStdin != c.stdin || s.Harness != c.h {
			t.Errorf("Build(%s, %+v) = %+v, %v; want %q stdin=%v %s", c.cli, c.o, s, err, c.want, c.stdin, c.h)
		}
	}
	s, _ := Build("claude", Options{}, "", nil, "", env(map[string]string{"AGENT_PERMISSION_MODE": "acceptEdits"}))
	if s.Argv[3] != "acceptEdits" {
		t.Errorf("AGENT_PERMISSION_MODE ignored: %q", s.Argv)
	}
	s, _ = Build("custom", Options{}, "", nil, "", env(map[string]string{"AGENT_CMD": "cat", "WARDEN_HARNESS": "pi"}))
	if !reflect.DeepEqual(s, Spec{[]string{"bash", "-c", "cat"}, true, "pi"}) {
		t.Errorf("custom = %+v", s)
	}
	if _, err := Build("", Options{}, "", nil, "", none); err != ErrNoCLI {
		t.Errorf("no CLI: %v", err)
	}
}

func TestClampEffort(t *testing.T) {
	for in, want := range map[string][2]any{
		"": {"medium", false}, "low": {"low", false}, "minimal": {"low", false}, " Medium ": {"medium", false},
		"high": {"medium", true}, "xhigh": {"medium", true}, "max": {"medium", true}, "bogus": {"medium", true},
	} {
		if e, c := ClampEffort(in); e != want[0] || c != want[1] {
			t.Errorf("ClampEffort(%q) = %q, %v; want %v", in, e, c, want)
		}
	}
}

func TestDefaultEffort(t *testing.T) {
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	t.Setenv("AGENT_EFFORT", "high")
	if e := DefaultEffort(); e != "medium" {
		t.Errorf("env high → %q", e)
	}
	t.Setenv("AGENT_EFFORT", "low")
	if e := DefaultEffort(); e != "low" {
		t.Errorf("env low → %q", e)
	}
	t.Setenv("AGENT_EFFORT", "")
	if e := DefaultEffort(); e != "medium" {
		t.Errorf("default → %q", e)
	}
}

func TestResolveCLI(t *testing.T) {
	only := func(bins ...string) func(string) bool {
		return func(b string) bool {
			for _, x := range bins {
				if x == b {
					return true
				}
			}
			return false
		}
	}
	if c := ResolveCLI(env(nil), only("agent", "pi")); c != "cursor" {
		t.Errorf("first installed = %q", c)
	}
	if c := ResolveCLI(env(map[string]string{"AGENT_CLI": "gemini"}), only()); c != "gemini" {
		t.Errorf("AGENT_CLI = %q", c)
	}
	if c := ResolveCLI(env(map[string]string{"AGENT_CLI": "gemini", "AGENT_CMD": "x"}), only()); c != "custom" {
		t.Errorf("AGENT_CMD = %q", c)
	}
}

func TestEnv(t *testing.T) {
	got := Env([]string{"CLAUDECODE=1", "AI_AGENT=x", "PATH=/bin", "WARDEN_HARNESS=old"}, Spec{Harness: "pi"}, Options{PromptFile: "/p", ReadOnly: true, Effort: "low"})
	want := []string{"PATH=/bin", "AGENT_PROMPT_FILE=/p", "AGENT_MODEL=", "AGENT_EFFORT=low", "AGENT_READONLY=1", "WARDEN_HARNESS=pi"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Env = %q", got)
	}
}

func TestSplitArgs(t *testing.T) {
	got := SplitArgs(`--a 'b c' "d \"e\"" f\ g  `)
	want := []string{"--a", "b c", `d "e"`, "f g"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitArgs = %q", got)
	}
}

func TestRoute(t *testing.T) {
	cases := []struct{ text, kind, speed, want string }{
		{"fix typo in README", "ship", "normal", "cheap"},
		{"regenerate the lockfile after the upgrade", "ship", "normal", "mid"},
		{"add OAuth login with tokens", "ship", "normal", "strong"},
		{"add CSV export to reports", "ship", "normal", "mid"},
		{"add CSV export", "ship", "full", "strong"},
		{"", "review", "normal", "cheap"},
		{"refactor the parser", "ship", "normal", "strong"},
		{"refactor the parser", "ship", "fast", "mid"},
		{"fix typo", "fix", "normal", "mid"},
		{"add a page\n## Files\n- a.go\n- b.go\n- c.go\n- d.go\n- e.go\n- f.go\n- g.go\n- h.go\n- i.go\n", "ship", "normal", "strong"},
		{"x\n- [ ] 1\n- [ ] 2\n- [x] 3\n- [ ] 4\n- [ ] 5\n- [ ] 6\n- [ ] 7\n", "ship", "normal", "strong"},
	}
	for _, c := range cases {
		if got, r := Route(c.text, c.kind, c.speed); got != c.want {
			t.Errorf("Route(%q, %s, %s) = %s %q, want %s", c.text, c.kind, c.speed, got, r, c.want)
		}
	}
	if _, r := Route("add CSV export", "ship", "normal"); !reflect.DeepEqual(r, []string{"ordinary feature work"}) {
		t.Errorf("reasons = %q", r)
	}
}

func TestRouteMainRejectsBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--text", "x", "--kind", "bogus"},
		{"--text", "x", "--speed", "turbo"},
		{"--text", "x", "--kind"},
		{"--text"},
		{"--text", "x", "--speed", "--json"},
	} {
		if got := RouteMain(args); got != 2 {
			t.Errorf("RouteMain(%q) = %d, want 2", args, got)
		}
	}
}

func TestConsultCLI(t *testing.T) {
	have := func(b string) bool { return b == "codex" || b == "agent" }
	for h, want := range map[string]string{"claude-cli": "codex", "cursor": "cursor", "pi": "codex", "unknown": "codex"} {
		if got := ConsultCLI(h, have); got != want {
			t.Errorf("ConsultCLI(%s) = %q, want %q", h, got, want)
		}
	}
	if got := ConsultCLI("pi", func(string) bool { return false }); got != "" {
		t.Errorf("nothing installed = %q", got)
	}
}

func TestConsultPrompt(t *testing.T) {
	p := ConsultPrompt("Q1", "A; B", "ctx-line")
	if !strings.Contains(p, "Question: Q1\nOptions (neutral order): A; B\nContext:\nctx-line\nPick one") || !strings.HasSuffix(p, "Do not use tools.\n") {
		t.Errorf("prompt = %q", p)
	}
	if strings.Contains(ConsultPrompt("q", "o", ""), "Context:") {
		t.Error("empty context printed")
	}
}

func TestSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SMITHY_SKILL_DIR", "")
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, "pi"))
	t.Chdir(home)
	dir := filepath.Join(home, "pi", "skills", "scout")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "SKILL.md"), nil, 0o644)
	os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0o755)
	os.Symlink(dir, filepath.Join(home, ".claude", "skills", "scout"))
	real, _ := filepath.EvalSymlinks(dir)
	if p, ok := FindSkill("scout"); !ok || p != filepath.Join(real, "SKILL.md") {
		t.Errorf("FindSkill(scout) = %q %v", p, ok)
	}
	if _, ok := FindSkill("tend"); ok {
		t.Error("tend found")
	}
	c := SkillCandidates("x", "/s/use-smithy/scripts", "/top", "/h", env(nil))
	if c[0] != "/s/x/SKILL.md" || c[1] != "/top/.agents/skills/x/SKILL.md" || c[len(c)-1] != "/h/.dsh/skills/x/SKILL.md" {
		t.Errorf("candidates = %q", c)
	}
}

func TestBlock(t *testing.T) {
	blk := "<!-- use-smithy:start -->\nhi\n<!-- use-smithy:end -->\n"
	once := AddBlock("# Rules\n", blk)
	if twice := AddBlock(once, blk); twice != once || strings.Count(twice, "use-smithy:start") != 1 {
		t.Errorf("add not idempotent: %q", twice)
	}
	if once != "# Rules\n\n"+blk {
		t.Errorf("add = %q", once)
	}
	if got := StripBlock(once); got != "# Rules\n" || HasBlock(got) {
		t.Errorf("strip = %q", got)
	}
	if AddBlock("", blk) != blk {
		t.Error("add to empty file")
	}
	if f := DefaultFile("codex", "/h", "/w", env(map[string]string{"CODEX_HOME": "/c"})); f != "/c/AGENTS.md" {
		t.Errorf("codex file = %s", f)
	}
	if f := DefaultFile("cursor", "/h", "/w", env(nil)); f != "/w/AGENTS.md" {
		t.Errorf("cursor file = %s", f)
	}
}

func TestAgentKind(t *testing.T) {
	for in, want := range map[string]string{
		"spec-check": "review", "skills/smelt/agents/security.md": "review",
		"hostile.md": "test", "adversary": "test", "implementer": "ship",
		"skills/tend/SKILL.md": "tend", "scout": "scout",
	} {
		if k, ok := AgentKind(in); !ok || k != want {
			t.Errorf("AgentKind(%q) = %q, %v; want %q", in, k, ok, want)
		}
	}
	if _, ok := AgentKind("nope"); ok {
		t.Error("unknown agent accepted")
	}
	if tier, _ := Route("", "test", "normal"); tier != "mid" {
		t.Errorf("test agent tier %s, want mid", tier)
	}
}

func TestParseTimeout(t *testing.T) {
	for in, want := range map[string]time.Duration{"90": 90 * time.Minute, "0": 0, "0.5": 30 * time.Second, "": 90 * time.Minute, "x": 90 * time.Minute, "Inf": 90 * time.Minute} {
		if got := ParseTimeout(in); got != want {
			t.Errorf("ParseTimeout(%q) = %v, want %v", in, got, want)
		}
	}
}

func timeoutPrompt(t *testing.T) (string, string) {
	t.Helper()
	d := t.TempDir()
	p := filepath.Join(d, "prompt.md")
	os.WriteFile(p, []byte("x"), 0o644)
	t.Setenv("AGENT_CMD", "sleep 30 & echo started; wait")
	return p, filepath.Join(d, "agent.log")
}

func TestAgentTimeout(t *testing.T) {
	p, log := timeoutPrompt(t)
	start := time.Now()
	rc := AgentMain([]string{"--timeout", "0.02", "--log", log, p})
	if rc != 124 {
		t.Fatalf("timed-out run exited %d, want 124", rc)
	}
	if el := time.Since(start); el > 10*time.Second {
		t.Fatalf("timeout not enforced: ran %v", el)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "timed out after 0.02m") || !strings.Contains(string(b), "started") {
		t.Errorf("log = %q", b)
	}
}

func TestAgentNoTimeoutWhenZero(t *testing.T) {
	p, log := timeoutPrompt(t)
	t.Setenv("AGENT_CMD", "echo ok")
	if rc := AgentMain([]string{"--timeout", "0", "--log", log, p}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
}

func TestAgentTimeoutBackground(t *testing.T) {
	p, log := timeoutPrompt(t)
	t.Setenv("SMITHY_TEST_AGENT_MAIN", "1")
	start := time.Now()
	if rc := AgentMain([]string{"--bg", "--timeout", "0.02", "--log", log, p}); rc != 0 {
		t.Fatalf("bg rc = %d", rc)
	}
	for time.Since(start) < 15*time.Second {
		if b, _ := os.ReadFile(log); strings.Contains(string(b), "timed out after 0.02m") {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	b, _ := os.ReadFile(log)
	t.Fatalf("background run not timed out; log = %q", b)
}

func advPrompt(t *testing.T) (string, string, string) {
	t.Helper()
	d := t.TempDir()
	p := filepath.Join(d, "prompt.md")
	os.WriteFile(p, []byte("x"), 0o644)
	return d, p, filepath.Join(d, "agent.log")
}

func TestConsultPrefersJudgeTier(t *testing.T) {
	if !strings.Contains(consultUsage, "judge tier") {
		t.Fatal("consult usage must name the judge tier")
	}
}

func TestRouteProseTaskIsCheapEvenWhenItMentionsMoney(t *testing.T) {
	tier, why := Route("Write CONTRIBUTING doc\nExplain that money is integer cents.", "ship", "fast")
	if tier != "cheap" {
		t.Fatalf("tier = %s (%v)", tier, why)
	}
	if tier, _ := Route("Add payment retries", "ship", "normal"); tier != "strong" {
		t.Fatalf("risky title routed %s", tier)
	}
}

func TestAgentBackgroundReportsAnImmediateFailure(t *testing.T) {
	p, log := timeoutPrompt(t)
	t.Setenv("AGENT_CMD", "sh -c 'echo Not logged in; exit 1'")
	old := startCheck
	startCheck = 3 * time.Second
	defer func() { startCheck = old }()
	if rc := AgentMain([]string{"--bg", "--timeout", "0", "--log", log, p}); rc != 1 {
		t.Fatalf("rc = %d, want 1", rc)
	}
}

func TestRouteDocWordsAreWholeWords(t *testing.T) {
	if tier, why := Route("Fix doctor exit code on docker hosts", "ship", "normal"); tier == "cheap" {
		t.Fatalf("doctor/docker routed cheap: %v", why)
	}
}

func TestRouteReadsNegationAndTokenMeaning(t *testing.T) {
	for text, want := range map[string]string{
		"Add a CI workflow. No secrets needed.":           "mid",
		"Context meter shows stale context tokens":        "mid",
		"Rotate the API token used for deploys":           "strong",
		"Fix race condition in the queue worker shutdown": "strong",
	} {
		if tier, why := Route(text, "ship", "normal"); tier != want {
			t.Errorf("%q → %s %v, want %s", text, tier, why, want)
		}
	}
	_, why := Route("handle concurrent writes", "ship", "normal")
	if !strings.Contains(strings.Join(why, " "), "concurrent") {
		t.Errorf("reason cut off: %v", why)
	}
}

func TestRouteNotDoesNotNegateRisk(t *testing.T) {
	for _, text := range []string{"Do not store the secret in logs", "Ensure the change does not break auth"} {
		if tier, why := Route(text, "ship", "normal"); tier != "strong" {
			t.Errorf("%q → %s %v", text, tier, why)
		}
	}
}
