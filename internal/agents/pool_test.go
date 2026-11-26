package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/pool"
	"github.com/Zovaris/smithy/internal/testkit"
)

// fakePoolClaude is a claude that hits its limit when CLAUDE_CONFIG_DIR ends in "-limited" and
// answers otherwise; it records the dir it ran with.
func fakePoolClaude(t *testing.T) (prompt, log, ran string) {
	t.Helper()
	d := t.TempDir()
	ran = filepath.Join(d, "ran")
	bin := filepath.Join(d, "claude")
	script := "#!/bin/sh\necho \"$CLAUDE_CONFIG_DIR\" >> " + ran + "\n" +
		"echo \"key=$ANTHROPIC_API_KEY oauth=$CLAUDE_CODE_OAUTH_TOKEN\" > " + filepath.Join(d, "env") + "\n" +
		"[ -n \"$FAKE_TOUCH\" ] && echo partial > touched.txt\n" +
		"case \"$CLAUDE_CONFIG_DIR\" in *-limited) echo 'first attempt'; echo '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":true,\"result\":\"You'\"'\"'ve hit your limit · resets 3pm (UTC)\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}'; exit 1;; esac\n" +
		"echo '{\"type\":\"result\",\"result\":\"all done\",\"total_cost_usd\":0.5,\"usage\":{\"input_tokens\":3,\"output_tokens\":7}}'\n"
	os.WriteFile(bin, []byte(script), 0o755)
	t.Setenv("AGENT_CLI", bin)
	t.Setenv("SMITHY_HOME", d)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	prompt = filepath.Join(d, "p.md")
	os.WriteFile(prompt, []byte("x"), 0o644)
	return prompt, filepath.Join(d, "w.log"), ran
}

func TestAgentPoolFailsOverOnceOnALimit(t *testing.T) {
	p, log, ran := fakePoolClaude(t)
	home := os.Getenv("SMITHY_HOME")
	if _, err := pool.Add("a", "claude", filepath.Join(home, "a-limited")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Add("b", "claude", filepath.Join(home, "b")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-pass")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "tok-should-not-pass")
	if rc := AgentMain([]string{"--readonly", "--timeout", "0", "--log", log, p}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	r, _ := os.ReadFile(ran)
	if got := strings.Fields(string(r)); len(got) != 2 || !strings.HasSuffix(got[0], "a-limited") || !strings.HasSuffix(got[1], "/b") {
		t.Fatalf("runs = %q", got)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "first attempt") || !strings.HasSuffix(string(b), "all done\n") {
		t.Fatalf("log not appended: %q", b)
	}
	if e, _ := os.ReadFile(filepath.Join(home, "env")); strings.TrimSpace(string(e)) != "key= oauth=" {
		t.Fatalf("pooled run kept credentials: %q", e)
	}
	if u := pool.State()["a"].LimitedUntil; u.Hour() != 15 || !u.After(time.Now()) {
		t.Fatalf("a limited until %v", u)
	}
	ev := usageEvents(t, home)
	if !strings.Contains(ev, `"kind":"limited"`) || !strings.Contains(ev, `"account":"b"`) {
		t.Fatalf("events = %s", ev)
	}
}

func TestAgentPoolAllLimitedExits75WithoutRunning(t *testing.T) {
	p, log, ran := fakePoolClaude(t)
	if _, err := pool.Add("a", "claude", ""); err != nil {
		t.Fatal(err)
	}
	reset := time.Now().Add(2 * time.Hour)
	pool.MarkLimited("a", reset)
	if rc := AgentMain([]string{"--timeout", "0", "--log", log, p}); rc != ExitLimited {
		t.Fatalf("rc = %d", rc)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("agent ran with every account limited")
	}
	if ev := usageEvents(t, os.Getenv("SMITHY_HOME")); !strings.Contains(ev, "every pool account is limited") {
		t.Fatalf("events = %s", ev)
	}
}

func TestAgentPoolSecondLimitExits75(t *testing.T) {
	p, log, _ := fakePoolClaude(t)
	home := os.Getenv("SMITHY_HOME")
	pool.Add("a", "claude", filepath.Join(home, "a-limited"))
	pool.Add("b", "claude", filepath.Join(home, "b-limited"))
	if rc := AgentMain([]string{"--readonly", "--timeout", "0", "--log", log, p}); rc != ExitLimited {
		t.Fatalf("rc = %d", rc)
	}
	st := pool.State()
	if st["a"].LimitedUntil.IsZero() || st["b"].LimitedUntil.IsZero() {
		t.Fatalf("state = %+v", st)
	}
}

func TestAgentPoolOffRunsAsBefore(t *testing.T) {
	p, log, ran := fakePoolClaude(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "/keep/mine")
	if rc := AgentMain([]string{"--timeout", "0", "--log", log, p}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if r, _ := os.ReadFile(ran); strings.TrimSpace(string(r)) != "/keep/mine" {
		t.Fatalf("ran with %q", r)
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	testkit.Git(t, d, "init", "-q", "-b", "feat/x")
	testkit.Git(t, d, "commit", "-q", "--allow-empty", "-m", "init")
	return d
}

// limitedBatch sets up a warden card dir and two accounts, the first limited.
func limitedBatch(t *testing.T) (p, log, ran, wh string) {
	t.Helper()
	p, _, ran = fakePoolClaude(t)
	home := os.Getenv("SMITHY_HOME")
	pool.Add("a", "claude", filepath.Join(home, "a-limited"))
	pool.Add("b", "claude", filepath.Join(home, "b"))
	wh = t.TempDir()
	t.Setenv("WARDEN_HOME", wh)
	os.MkdirAll(filepath.Join(wh, "b1"), 0o755)
	return p, filepath.Join(wh, "b1", "01-x.log"), ran, wh
}

func TestAgentPoolRetriesAnUntouchedWriteRun(t *testing.T) {
	p, log, ran, _ := limitedBatch(t)
	repo := gitRepo(t)
	if rc := AgentMain([]string{"--timeout", "0", "--cwd", repo, "--log", log, p}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if r, _ := os.ReadFile(ran); strings.Count(string(r), "\n") != 2 {
		t.Fatalf("runs = %q", r)
	}
}

func TestAgentPoolNeverRetriesAWriteRunWithPartialWork(t *testing.T) {
	p, log, ran, wh := limitedBatch(t)
	repo := gitRepo(t)
	t.Setenv("FAKE_TOUCH", "1")
	if rc := AgentMain([]string{"--timeout", "0", "--cwd", repo, "--log", log, p}); rc != ExitLimited {
		t.Fatalf("rc = %d", rc)
	}
	if r, _ := os.ReadFile(ran); strings.Count(string(r), "\n") != 1 {
		t.Fatalf("retried after partial work: %q", r)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "first attempt") {
		t.Fatalf("log = %q", b)
	}
	card, _ := os.ReadFile(filepath.Join(wh, "b1", "01-x.json"))
	if !strings.Contains(string(card), `"state": "blocked"`) || !strings.Contains(string(card), "account a limited until") || !strings.Contains(string(card), "relaunch on another account") {
		t.Fatalf("card = %s", card)
	}
}

func TestAgentPoolNeverRetriesOutsideGit(t *testing.T) {
	p, log, ran, _ := limitedBatch(t)
	if rc := AgentMain([]string{"--timeout", "0", "--cwd", t.TempDir(), "--log", log, p}); rc != ExitLimited {
		t.Fatalf("rc = %d", rc)
	}
	if r, _ := os.ReadFile(ran); strings.Count(string(r), "\n") != 1 {
		t.Fatalf("runs = %q", r)
	}
}

func TestAutoDeniedCoversPoolAccounts(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	custom := filepath.Join(t.TempDir(), "elsewhere")
	if _, err := pool.Add("c", "claude", custom); err != nil {
		t.Fatal(err)
	}
	d := strings.Join(AutoDenied([]string{"/bin/smithy"}), "\n")
	p := filepath.ToSlash(custom)
	for _, want := range []string{"Read(/" + p + "/**)", "Edit(/" + p + "/**)", "Bash(*" + p + "*)", "Read(~/.smithy/accounts/**)", "Edit(~/.smithy/accounts/**)", "Edit(~/.smithy/pool.json)", "Bash(*.smithy/accounts*)", "Bash(/bin/smithy pool *)"} {
		if !strings.Contains(d, want) {
			t.Errorf("AutoDenied lacks %s", want)
		}
	}
}

func TestAgentPoolRecordsAccountOnTheCard(t *testing.T) {
	p, _, _ := fakePoolClaude(t)
	wh := t.TempDir()
	t.Setenv("WARDEN_HOME", wh)
	os.MkdirAll(filepath.Join(wh, "b1"), 0o755)
	pool.Add("work", "claude", "")
	if rc := AgentMain([]string{"--timeout", "0", "--log", filepath.Join(wh, "b1", "01-x.log"), p}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if b, _ := os.ReadFile(filepath.Join(wh, "b1", "01-x.json")); !strings.Contains(string(b), `"account": "work"`) {
		t.Fatalf("card = %s", b)
	}
}

func TestAcquirePoolFallsBackAcrossHarnessesAndLogsTheSwitch(t *testing.T) {
	d := t.TempDir()
	t.Setenv("SMITHY_HOME", d)
	t.Setenv("SMITHY_EVENTS", "")
	if err := config.Set(config.UserFile(), "pool.order", "claude,codex"); err != nil {
		t.Fatal(err)
	}
	_, _ = pool.Add("c1", "claude", filepath.Join(d, "c1"))
	_, _ = pool.Add("x1", "codex", filepath.Join(d, "x1"))
	_ = pool.MarkLimited("c1", time.Now().Add(time.Hour))
	var o Options
	if a, rc := acquire("pool", &o); rc != 0 || a.Name != "x1" {
		t.Fatalf("acquire = %s rc %d, want the codex fallback", a.Name, rc)
	}
	es, _ := events.Read(time.Time{}, 0)
	found := false
	for _, e := range es {
		found = found || (e.Kind == "pool_fallback" && e.Account == "x1" && strings.Contains(e.Detail, "claude") && strings.Contains(e.Detail, "codex"))
	}
	if !found {
		t.Fatalf("no pool_fallback event in %+v", es)
	}
	_ = pool.Clear("c1")
	if a, _ := acquire("pool", &o); a.Name != "c1" {
		t.Fatalf("acquire = %s, want back on claude after the reset", a.Name)
	}
}
