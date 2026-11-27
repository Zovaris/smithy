//go:build harness

package daemon

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/Zovaris/smithy/internal/layout"

	"github.com/Zovaris/smithy/internal/apiloop"
)

//go:embed prompt.md
var systemBase string

// PromptState says which file the system prompt's base text comes from: the user file at Path
// when Active, else the default built into smithy.
type PromptState struct {
	Path   string `json:"path"`
	Active bool   `json:"active"`
}

// PromptStatus is where the user's prompt file lives and whether it replaces the default.
func PromptStatus() PromptState {
	_, st := promptBase()
	return st
}

func promptBase() (string, PromptState) {
	st := PromptState{Path: filepath.Join(layout.Home(), "prompt.md")}
	if b, err := os.ReadFile(st.Path); err == nil && strings.TrimSpace(string(b)) != "" {
		st.Active = true
		return strings.TrimRight(string(b), "\n"), st
	}
	return strings.TrimRight(systemBase, "\n"), st
}

// SystemPrompt is what every chat agent is told about smithy: the tools, the commands and
// whether code mode (run_code) is on for it. Code mode only exists for api: agents.
func SystemPrompt(agent string, codeMode bool) string {
	var b strings.Builder
	base, _ := promptBase()
	b.WriteString(base)
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
