//go:build harness

package daemon

import (
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
