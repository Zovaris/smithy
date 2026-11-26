// Package harness detects the agent harness this process runs in and the adapter warden uses there.
package harness

import (
	"os"
	"strings"
)

// Detect returns the harness name (claude-desktop, claude-cli, codex, opencode, agy, copilot, grok,
// pi, dsh, cursor or unknown) and the adapter (desktop, cli or any). WARDEN_HARNESS overrides detection.
// Only variables each harness sets for its own child processes are used, never user settings.
func Detect() (string, string) {
	h := os.Getenv("WARDEN_HARNESS")
	env := func(keys ...string) bool {
		for _, k := range keys {
			if os.Getenv(k) != "" {
				return true
			}
		}
		return false
	}
	if h == "" {
		switch {
		case os.Getenv("CLAUDE_CODE_ENTRYPOINT") == "claude-desktop":
			h = "claude-desktop"
		case env("CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT"):
			h = "claude-cli"
		case env("CODEX_THREAD_ID", "CODEX_CI", "CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED"):
			h = "codex"
		case env("OPENCODE", "OPENCODE_SESSION_ID"):
			h = "opencode"
		case env("PI_CODING_AGENT", "PI_SESSION_ID"):
			h = "pi"
		case env("DSH_SESSION_ID"):
			h = "dsh"
		default:
			a := os.Getenv("AI_AGENT")
			switch {
			case strings.HasPrefix(a, "claude"):
				h = "claude-cli"
			case strings.HasPrefix(a, "codex"):
				h = "codex"
			case strings.HasPrefix(a, "opencode"):
				h = "opencode"
			case strings.HasPrefix(a, "agy"), strings.HasPrefix(a, "antigravity"):
				h = "agy"
			case strings.HasPrefix(a, "copilot"):
				h = "copilot"
			case strings.HasPrefix(a, "grok"):
				h = "grok"
			case strings.HasPrefix(a, "pi"):
				h = "pi"
			case strings.HasPrefix(a, "dsh"), strings.HasPrefix(a, "deepseek"):
				h = "dsh"
			case strings.HasPrefix(a, "cursor"), env("CURSOR_AGENT", "CURSOR_TRACE_ID"):
				h = "cursor"
			default:
				h = "unknown"
			}
		}
	}
	switch h {
	case "claude-desktop":
		return h, "desktop"
	case "claude-cli":
		return h, "cli"
	}
	return h, "any"
}
