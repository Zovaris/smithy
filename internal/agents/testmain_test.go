package agents

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestMain clears the variables the host agent session sets (harness markers and agent/consult
// overrides) so results do not depend on whether the tests run inside Claude, Codex or a plain shell.
func TestMain(m *testing.M) {
	if os.Getenv("SMITHY_TEST_FAKE_ACP") == "1" {
		fakeACP()
		os.Exit(0)
	}
	if os.Getenv("SMITHY_TEST_FAKE_ACP") == "broken" {
		// a protocol error, then a child that never exits on its own
		var m map[string]any
		json.NewDecoder(os.Stdin).Decode(&m)
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m["id"], "error": map[string]any{"code": -32000, "message": "broken"}})
		os.Stdout.Write(append(b, '\n'))
		time.Sleep(time.Minute)
		os.Exit(0)
	}
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
