package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
)

type apiFake struct {
	got  apiloop.Request
	wait bool
}

func (f *apiFake) Stream(ctx context.Context, r apiloop.Request, onText func(string)) (apiloop.Reply, error) {
	f.got = r
	if f.wait {
		<-ctx.Done()
		return apiloop.Reply{}, ctx.Err()
	}
	onText("answer to: " + r.Messages[0].Parts[0].Text)
	return apiloop.Reply{Stop: apiloop.StopEnd, Usage: usage.Usage{Input: 1_000_000, Output: 0}}, nil
}

func apiRun(t *testing.T, fake *apiFake, args ...string) (string, string, int, []string) {
	t.Helper()
	t.Setenv("SMITHY_HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "from-env-ignored")
	var keys []string
	old := APIProvider
	APIProvider = func(name, key, base string) (apiloop.Provider, error) {
		keys = append(keys, name+"="+key)
		return fake, nil
	}
	t.Cleanup(func() { APIProvider = old })
	if err := auth.Set("anthropic", "stored-placeholder"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "prompt.md")
	os.WriteFile(p, []byte("fix it\n"), 0o644)
	log := filepath.Join(dir, "run.log")
	rc := AgentMain(append(append([]string{"--timeout", "1"}, args...), "--log", log, p))
	b, _ := os.ReadFile(log)
	return string(b), log, rc, keys
}

func TestAgentAPIBackendRunsTheLoopWithTheStoredKey(t *testing.T) {
	fake := &apiFake{}
	out, log, rc, keys := apiRun(t, fake, "--backend", "api", "--model", "anthropic/claude-sonnet-4-5")
	if rc != 0 || !strings.Contains(out, "answer to: fix it") {
		t.Fatalf("rc=%d log=%q", rc, out)
	}
	if len(keys) != 1 || keys[0] != "anthropic=stored-placeholder" {
		t.Fatalf("keys %v: want the smithy auth key, never the env var", keys)
	}
	if fake.got.Model != "claude-sonnet-4-5" {
		t.Fatalf("model %q", fake.got.Model)
	}
	if strings.Contains(out, "stored-placeholder") {
		t.Fatal("key in the log")
	}
	u, ok := usage.Read(log)
	if !ok || u.Input != 1_000_000 || u.CostUSD == nil || *u.CostUSD != 3 {
		t.Fatalf("sidecar %+v %v", u, ok)
	}
	es, _ := events.Read(time.Time{}, 100)
	var seen bool
	for _, e := range es {
		if e.Kind == "usage" && e.Usage != nil && e.Usage.Input == 1_000_000 && e.Detail == "run.log" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("no usage event in %+v", es)
	}
}

func TestAgentAPITargetFromAgentCLI(t *testing.T) {
	t.Setenv("AGENT_CLI", "api:anthropic/claude-haiku-4-5")
	fake := &apiFake{}
	if _, _, rc, _ := apiRun(t, fake); rc != 0 || fake.got.Model != "claude-haiku-4-5" {
		t.Fatalf("rc=%d model=%q", rc, fake.got.Model)
	}
}

func TestAgentAPIBackendFailsWithoutKeyOrHarnessOrModel(t *testing.T) {
	_, _, rc, _ := apiRun(t, &apiFake{}, "--backend", "api", "--model", "openai/gpt-5")
	if rc != 2 {
		t.Fatalf("no openai key: rc=%d", rc)
	}
	if _, _, rc, _ := apiRun(t, &apiFake{}, "--backend", "api"); rc != 2 {
		t.Fatalf("no model: rc=%d", rc)
	}
	if _, _, rc, _ := apiRun(t, &apiFake{}, "--backend", "nope"); rc != 2 {
		t.Fatalf("bad backend: rc=%d", rc)
	}
	t.Setenv("SMITHY_HOME", t.TempDir())
	old := APIProvider
	APIProvider = nil
	t.Cleanup(func() { APIProvider = old })
	if rc := runAPI(Options{Backend: "api", Model: "anthropic/x"}, ""); rc != 2 {
		t.Fatalf("skills build: rc=%d", rc)
	}
}

func TestAgentAPITimeoutExits124(t *testing.T) {
	out, _, rc, _ := apiRun(t, &apiFake{wait: true}, "--timeout", "0.0001", "--backend", "api", "--model", "anthropic/m")
	if rc != 124 || !strings.Contains(out, "timed out") {
		t.Fatalf("rc=%d log=%q", rc, out)
	}
}

func TestAPISinkReportsToolsAndRecordsRejectedPermissions(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	var got []string
	old := addDecision
	addDecision = func(q, choice, why, ref string) error { got = append(got, q+" -> "+choice+" ("+why+")"); return nil }
	t.Cleanup(func() { addDecision = old })
	s := apiSink{out: &strings.Builder{}, log: "run.log"}
	s.Permission(apiloop.ToolCall{Name: "shell"}, false, "read-only run")
	s.Permission(apiloop.ToolCall{Name: "read"}, true, "")
	s.Tool(apiloop.ToolCall{Name: "read"}, apiloop.ToolResult{Output: "x"})
	if len(got) != 1 || got[0] != "let the agent run shell? -> rejected (read-only run)" {
		t.Fatalf("decisions %v", got)
	}
	es, _ := events.Read(time.Time{}, 100)
	var details []string
	for _, e := range es {
		details = append(details, e.Kind+" "+e.Detail)
	}
	if strings.Join(details, ",") != "permission shell: rejected,permission read: allowed,tool read: completed" {
		t.Fatalf("events %v", details)
	}
}

func TestAgentAPIBackendOffersTheCoreToolsUnderAutoRules(t *testing.T) {
	fake := &apiFake{}
	if _, _, rc, _ := apiRun(t, fake, "--backend", "api", "--model", "anthropic/claude-sonnet-4"); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	var names []string
	for _, s := range fake.got.Tools {
		names = append(names, s.Name)
	}
	for _, s := range apiTools(Options{}, "gpt-5").Specs() {
		names = append(names, s.Name)
	}
	got := strings.Join(names, ",")
	for _, want := range []string{"read", "write", "edit", "shell", "glob", "grep", "fetch", "apply_patch"} {
		if !strings.Contains(","+got+",", ","+want+",") {
			t.Fatalf("tools %s: missing %s", got, want)
		}
	}
	set := apiTools(Options{Cwd: t.TempDir()}, "claude-sonnet-4")
	if ok, _ := set.Permit(apiloop.ToolCall{Name: "shell", Args: []byte(`{"command":"curl https://x"}`)}); ok {
		t.Fatal("curl must be denied as under --profile auto")
	}
	if ok, _ := set.Permit(apiloop.ToolCall{Name: "read", Args: []byte(`{"path":"~/.smithy/auth.json"}`)}); ok {
		t.Fatal("auth.json must be denied")
	}
	if ok, _ := set.Permit(apiloop.ToolCall{Name: "shell", Args: []byte(`{"command":"git status"}`)}); !ok {
		t.Fatal("git status must be allowed")
	}
}
