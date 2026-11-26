package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func readyByName(rs []Ready) map[string]Ready {
	m := map[string]Ready{}
	for _, r := range rs {
		m[r.Name] = r
	}
	return m
}

func TestDetectReadyFindsCLIsOnPathAndInKnownFolders(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".claude", "local")
	os.MkdirAll(local, 0o755)
	os.WriteFile(filepath.Join(local, "claude"), []byte("#!/bin/sh\n"), 0o755)
	look := func(bin string) (string, error) {
		if bin == "codex" {
			return "/usr/bin/codex", nil
		}
		return "", os.ErrNotExist
	}
	env := map[string]string{"HOME": home}
	rs := DetectReady(ReadyDeps{LookPath: look, Getenv: func(k string) string { return env[k] }, Stored: func(string) string { return "" }})
	m := readyByName(rs)
	if c := m["claude"]; !c.Ready || c.Kind != "cli" || c.Detail != filepath.Join(local, "claude") {
		t.Fatalf("claude off PATH in ~/.claude/local must be ready with its path: %+v", c)
	}
	if c := m["codex"]; !c.Ready || c.Detail != "/usr/bin/codex" {
		t.Fatalf("codex on PATH: %+v", c)
	}
	g := m["agy"]
	if g.Ready || g.Fix != "curl -fsSL https://antigravity.google/cli/install.sh | bash" {
		t.Fatalf("missing agy needs its install hint: %+v", g)
	}
	if _, ok := m["gemini"]; ok {
		t.Fatal("Gemini CLI is gone; agy replaces it")
	}
	if c := m["copilot"]; c.Ready || c.Fix != "npm install -g @github/copilot" {
		t.Fatalf("missing copilot: %+v", c)
	}
	if c := m["grok"]; c.Ready || c.Fix != "curl -fsSL https://x.ai/cli/install.sh | bash" {
		t.Fatalf("missing grok: %+v", c)
	}
	if rs[0].Name != "claude" {
		t.Fatalf("Claude Code comes first, got %s", rs[0].Name)
	}
	for _, r := range rs {
		if !r.Ready && r.Fix == "" {
			t.Fatalf("%s is missing with no fix", r.Name)
		}
	}
}

func TestDetectReadyAPIKeys(t *testing.T) {
	env := map[string]string{"HOME": t.TempDir(), "OPENAI_API_KEY": "x"}
	stored := map[string]string{"anthropic": "k"}
	rs := DetectReady(ReadyDeps{
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		Getenv:   func(k string) string { return env[k] },
		Stored:   func(s string) string { return stored[s] },
	})
	m := readyByName(rs)
	if a := m["api:anthropic"]; !a.Ready || a.Kind != "api" || a.Detail != "smithy auth" {
		t.Fatalf("stored anthropic key: %+v", a)
	}
	o := m["api:openai"]
	if o.Ready || o.Fix != "smithy auth set openai" || o.Detail != "OPENAI_API_KEY is set; smithy's API loop reads only smithy auth" {
		t.Fatalf("an env-only key is not enough for the API loop and says why: %+v", o)
	}
	if d := m["api:deepseek"]; d.Ready || d.Fix != "smithy auth set deepseek" {
		t.Fatalf("deepseek: %+v", d)
	}
	if ol := m["api:ollama"]; ol.Ready || ol.Fix == "" {
		t.Fatalf("ollama without the binary: %+v", ol)
	}
}

// PathOnly (the demo) finds a CLI on PATH and nowhere else, so the host's agents never show.
func TestDetectReadyPathOnlySkipsInstallFolders(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".claude", "local")
	os.MkdirAll(local, 0o755)
	os.WriteFile(filepath.Join(local, "claude"), []byte("#!/bin/sh\n"), 0o755)
	look := func(bin string) (string, error) {
		if bin == "codex" {
			return "/demo/bin/codex", nil
		}
		return "", os.ErrNotExist
	}
	env := map[string]string{"HOME": home}
	m := readyByName(DetectReady(ReadyDeps{LookPath: look, Getenv: func(k string) string { return env[k] }, Stored: func(string) string { return "" }, PathOnly: true}))
	if c := m["claude"]; c.Ready {
		t.Fatalf("claude off PATH must not be found in PathOnly: %+v", c)
	}
	if c := m["codex"]; !c.Ready || c.Detail != "/demo/bin/codex" {
		t.Fatalf("codex on PATH: %+v", c)
	}
}

// The web shows each agent by the name its catalog entry gives it, not a table of its own.
func TestDetectReadyNamesEveryAgent(t *testing.T) {
	rs := DetectReady(ReadyDeps{
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		Getenv:   func(k string) string { return map[string]string{"HOME": t.TempDir()}[k] },
		Stored:   func(string) string { return "" },
	})
	m := readyByName(rs)
	for name, want := range map[string]string{"claude": "Claude Code", "agy": "Antigravity", "pi": "Pi", "dsh": "dsh", "api:anthropic": "Anthropic API", "api:openrouter": "OpenRouter API"} {
		if got := m[name].Display; got != want {
			t.Errorf("%s display = %q, want %q", name, got, want)
		}
	}
	for _, r := range rs {
		if r.Display == "" {
			t.Errorf("%s has no display name", r.Name)
		}
	}
}

func TestCLIPathAddsFoldersOffPath(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".claude", "local")
	os.MkdirAll(local, 0o755)
	os.WriteFile(filepath.Join(local, "claude"), []byte("#!/bin/sh\n"), 0o755)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n"), 0o755)
	got := CLIPath(bin, func(k string) string { return map[string]string{"HOME": home}[k] })
	parts := filepath.SplitList(got)
	if parts[0] != bin || parts[1] != local {
		t.Fatalf("CLIPath keeps PATH first, then adds claude's folder: %q", got)
	}
	if again := CLIPath(got, func(k string) string { return map[string]string{"HOME": home}[k] }); again != got {
		t.Fatalf("CLIPath adds a folder once: %q then %q", got, again)
	}
}
