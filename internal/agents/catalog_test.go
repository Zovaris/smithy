package agents

import (
	"reflect"
	"testing"
)

func TestTablesDeriveFromKnownCLIs(t *testing.T) {
	if want := []string{"claude", "codex", "opencode", "agy", "copilot", "grok", "cursor", "pi", "dsh"}; !reflect.DeepEqual(CLIs, want) {
		t.Errorf("CLIs = %v", CLIs)
	}
	if want := map[string]bool{"claude": true, "codex": true}; !reflect.DeepEqual(EffortFlags, want) {
		t.Errorf("EffortFlags = %v", EffortFlags)
	}
	if want := map[string][]string{"agy": {"agy", "acp"}, "copilot": {"copilot", "--acp"}, "grok": {"grok", "agent", "stdio"}, "opencode": {"opencode", "acp"}}; !reflect.DeepEqual(ACPCLIs, want) {
		t.Errorf("ACPCLIs = %v", ACPCLIs)
	}
	if Bin("cursor") != "agent" || Bin("codex") != "codex" || Bin("/x/custom") != "/x/custom" {
		t.Error("Bin")
	}
	if len(InstallHint) != len(CLIs) {
		t.Errorf("InstallHint has %d entries", len(InstallHint))
	}
	have := func(c string) bool { return c != "claude" }
	if got := ConsultCLI("cursor", func(c string) bool { return c == "agent" }); got != "cursor" {
		t.Errorf("ConsultCLI(cursor) = %q", got)
	}
	if got := ConsultCLI("claude-cli", have); got != "codex" {
		t.Errorf("ConsultCLI(claude-cli) without claude = %q", got)
	}
}
