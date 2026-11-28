package skills

import (
	"os"
	"testing"
)

// TestMain clears the variables the host agent session sets, as the agents package does.
func TestMain(m *testing.M) {
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
