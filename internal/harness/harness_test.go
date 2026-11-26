package harness

import "testing"

var envVars = []string{
	"WARDEN_HARNESS", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CODEX_THREAD_ID", "CODEX_CI", "CODEX_SANDBOX",
	"CODEX_SANDBOX_NETWORK_DISABLED", "CODEX_HOME", "OPENCODE", "OPENCODE_SESSION_ID", "GEMINI_CLI",
	"PI_CODING_AGENT", "PI_SESSION_ID", "DSH_SESSION_ID", "AI_AGENT", "CURSOR_AGENT", "CURSOR_TRACE_ID",
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantName    string
		wantAdapter string
	}{
		{"nothing", nil, "unknown", "any"},
		{"override", map[string]string{"WARDEN_HARNESS": "custom", "CLAUDECODE": "1"}, "custom", "any"},
		{"override claude-cli", map[string]string{"WARDEN_HARNESS": "claude-cli"}, "claude-cli", "cli"},
		{"override claude-desktop", map[string]string{"WARDEN_HARNESS": "claude-desktop"}, "claude-desktop", "desktop"},
		{"claude desktop", map[string]string{"CLAUDECODE": "1", "CLAUDE_CODE_ENTRYPOINT": "claude-desktop"}, "claude-desktop", "desktop"},
		{"claude cli entrypoint", map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli"}, "claude-cli", "cli"},
		{"claudecode", map[string]string{"CLAUDECODE": "1"}, "claude-cli", "cli"},
		{"codex thread", map[string]string{"CODEX_THREAD_ID": "x"}, "codex", "any"},
		{"codex ci", map[string]string{"CODEX_CI": "1"}, "codex", "any"},
		{"codex sandbox", map[string]string{"CODEX_SANDBOX": "seatbelt"}, "codex", "any"},
		{"codex network", map[string]string{"CODEX_SANDBOX_NETWORK_DISABLED": "1"}, "codex", "any"},
		{"codex home alone", map[string]string{"CODEX_HOME": "/x"}, "unknown", "any"},
		{"opencode", map[string]string{"OPENCODE": "1"}, "opencode", "any"},
		{"opencode session", map[string]string{"OPENCODE_SESSION_ID": "s"}, "opencode", "any"},
		{"pi agent", map[string]string{"PI_CODING_AGENT": "1"}, "pi", "any"},
		{"pi session", map[string]string{"PI_SESSION_ID": "1"}, "pi", "any"},
		{"dsh", map[string]string{"DSH_SESSION_ID": "1"}, "dsh", "any"},
		{"cursor agent", map[string]string{"CURSOR_AGENT": "1"}, "cursor", "any"},
		{"cursor trace", map[string]string{"CURSOR_TRACE_ID": "1"}, "cursor", "any"},
		{"ai agent claude", map[string]string{"AI_AGENT": "claude-code/1.0"}, "claude-cli", "cli"},
		{"ai agent codex", map[string]string{"AI_AGENT": "codex"}, "codex", "any"},
		{"ai agent opencode", map[string]string{"AI_AGENT": "opencode/2"}, "opencode", "any"},
		{"ai agent agy", map[string]string{"AI_AGENT": "agy"}, "agy", "any"},
		{"ai agent antigravity", map[string]string{"AI_AGENT": "antigravity-cli"}, "agy", "any"},
		{"ai agent copilot", map[string]string{"AI_AGENT": "copilot-cli"}, "copilot", "any"},
		{"ai agent grok", map[string]string{"AI_AGENT": "grok"}, "grok", "any"},
		{"gemini cli is gone", map[string]string{"GEMINI_CLI": "1"}, "unknown", "any"},
		{"ai agent pi", map[string]string{"AI_AGENT": "pi/1"}, "pi", "any"},
		{"ai agent dsh", map[string]string{"AI_AGENT": "dsh"}, "dsh", "any"},
		{"ai agent deepseek", map[string]string{"AI_AGENT": "deepseek-tui"}, "dsh", "any"},
		{"ai agent cursor", map[string]string{"AI_AGENT": "cursor"}, "cursor", "any"},
		{"ai agent other", map[string]string{"AI_AGENT": "mystery"}, "unknown", "any"},
		{"claude beats codex", map[string]string{"CLAUDECODE": "1", "CODEX_CI": "1"}, "claude-cli", "cli"},
		{"env var beats AI_AGENT", map[string]string{"OPENCODE": "1", "AI_AGENT": "claude"}, "opencode", "any"},
		{"cursor env with unknown AI_AGENT", map[string]string{"AI_AGENT": "zzz", "CURSOR_AGENT": "1"}, "cursor", "any"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range envVars {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			n, a := Detect()
			if n != tc.wantName || a != tc.wantAdapter {
				t.Fatalf("Detect() = %q, %q; want %q, %q", n, a, tc.wantName, tc.wantAdapter)
			}
		})
	}
}
