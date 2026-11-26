//go:build harness

package daemon

import (
	"strings"

	"github.com/Zovaris/smithy/internal/apiloop"
)

const systemBase = `You run inside smithy, a local harness: the user talks to you from smithy's TUI or web chat, and a smithy daemon runs each of your turns in this folder. You are not "just" Claude Code or another CLI; answer questions about smithy as part of it.

What smithy gives you:
- The mcp__smithy__ tools (ask, board_claim, board_list, board_set, board_show, search, secret_call) come from smithy's own MCP server: the board is smithy's task board, ask puts a question to the user, secret_call calls an API with a key the user stored without showing it to you.
- The smithy command line: smithy board (tasks), smithy tasks, smithy status, smithy config list / set (settings and models), smithy commit and smithy pr (repo-style commits and PRs), smithy cards and smithy wait (warden batches), smithy update. Run smithy <command> --help for details.
- Every tool call you make shows in the user's chat as a row with its name, arguments and result.`

// SystemPrompt is what every chat agent is told about smithy: the tools, the commands and
// whether code mode (run_code) is on for it. Code mode only exists for api: agents.
func SystemPrompt(agent string, codeMode bool) string {
	var b strings.Builder
	b.WriteString(systemBase)
	b.WriteString("\n- Code mode (setting agent.code_mode, for api: agents only): ")
	switch {
	case !strings.HasPrefix(agent, "api:"):
		b.WriteString("not available to this agent; it applies when the user picks an api: agent.")
	case codeMode:
		b.WriteString("on. run_code is offered: one sandboxed JavaScript program that calls the other tools through the tools object, instead of many separate calls.")
	default:
		b.WriteString("off. The user turns it on with smithy config set agent.code_mode true.")
	}
	return b.String()
}

// withSystem puts the system prompt before the message for CLIs that take no system-prompt
// flag; claude gets it through --append-system-prompt instead.
func withSystem(agent string, codeMode bool, prompt string) string {
	if agent == "claude" {
		return prompt
	}
	return "Context from smithy (not from the user):\n\n" + SystemPrompt(agent, codeMode) + "\n\n---\n\n" + prompt
}

// quoteArg quotes s as one word for agents.SplitArgs.
func quoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func hasRunCode(t apiloop.Tools) bool {
	if t == nil {
		return false
	}
	for _, s := range t.Specs() {
		if s.Name == "run_code" {
			return true
		}
	}
	return false
}
