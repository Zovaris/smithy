//go:build harness

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/agents"
)

func TestSystemPromptNamesSmithy(t *testing.T) {
	p := SystemPrompt("claude", false)
	for _, want := range []string{"smithy", "mcp__smithy__", "smithy board", "smithy config", "agent.code_mode"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
}

func TestSystemPromptCodeMode(t *testing.T) {
	if p := SystemPrompt("api:anthropic/x", true); !strings.Contains(p, "run_code") || !strings.Contains(p, "on") {
		t.Errorf("api agent with code mode on should be told run_code is offered: %s", p)
	}
	if p := SystemPrompt("api:anthropic/x", false); !strings.Contains(p, "off") {
		t.Errorf("code mode off not said: %s", p)
	}
	if p := SystemPrompt("claude", true); strings.Contains(p, "run_code is offered") {
		t.Errorf("a CLI agent never gets run_code: %s", p)
	}
}

func TestQuoteArgRoundTrips(t *testing.T) {
	s := SystemPrompt("claude", false) + " it's \"quoted\" $HOME `x` \\ end"
	got := agents.SplitArgs("--verbose --append-system-prompt " + quoteArg(s))
	if len(got) != 3 || got[2] != s {
		t.Fatalf("round trip failed: %d words", len(got))
	}
}

func TestWithSystemForOtherCLIs(t *testing.T) {
	p := withSystem("codex", false, "hello")
	if !strings.HasSuffix(p, "hello") || !strings.Contains(p, "smithy") {
		t.Fatalf("non-claude prompt should carry the system text before the message: %q", p)
	}
	if withSystem("claude", false, "hello") != "hello" {
		t.Fatal("claude gets the system prompt as a flag, not in the message")
	}
}

func TestSystemPromptUserFileReplacesBase(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	if st := PromptStatus(); st.Active {
		t.Fatalf("no user file should mean no override: %+v", st)
	}
	base := SystemPrompt("claude", false)
	if !strings.Contains(base, "mcp__smithy__") {
		t.Fatal("embedded default prompt missing")
	}
	path := PromptStatus().Path
	if err := os.WriteFile(path, []byte("Custom house rules."), 0o600); err != nil {
		t.Fatal(err)
	}
	p := SystemPrompt("claude", false)
	if !strings.HasPrefix(p, "Custom house rules.") || strings.Contains(p, "mcp__smithy__ tools") {
		t.Fatalf("user file should replace the base: %q", p)
	}
	if !strings.Contains(p, "agent.code_mode") {
		t.Fatal("code mode line should still follow the override")
	}
	if st := PromptStatus(); !st.Active || filepath.Base(st.Path) != "prompt.md" {
		t.Fatalf("override not reported active: %+v", st)
	}
}

func TestSystemPromptEmptyUserFileKeepsDefault(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	os.WriteFile(PromptStatus().Path, []byte("  \n"), 0o600)
	if !strings.Contains(SystemPrompt("claude", false), "mcp__smithy__") || PromptStatus().Active {
		t.Fatal("a blank user file should not replace the default")
	}
}
