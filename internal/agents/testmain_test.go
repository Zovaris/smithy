package agents

import (
	"os"
	"testing"
)

// TestMain clears the variables the host agent session sets (harness markers and agent/consult
// overrides) so results do not depend on whether the tests run inside Claude, Codex or a plain shell.
func TestMain(m *testing.M) {
	if os.Getenv("SMITHY_TEST_AGENT_MAIN") == "1" && len(os.Args) > 1 && os.Args[1] == "agent" {
		os.Exit(AgentMain(os.Args[2:]))
	}
	for _, k := range []string{
		"WARDEN_HARNESS", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CODEX_THREAD_ID", "CODEX_CI", "CODEX_SANDBOX",
		"CODEX_SANDBOX_NETWORK_DISABLED", "OPENCODE", "OPENCODE_SESSION_ID", "GEMINI_CLI", "PI_CODING_AGENT",
		"PI_SESSION_ID", "DSH_SESSION_ID", "AI_AGENT", "CURSOR_AGENT", "CURSOR_TRACE_ID",
		"AGENT_CLI", "AGENT_CMD", "AGENT_MODEL", "AGENT_EFFORT", "AGENT_ARGS", "CONSULT_CMD", "CONSULT_MODEL",
	} {
		os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
