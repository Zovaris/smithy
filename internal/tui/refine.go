//go:build harness

package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// refineFence opens the block the refine skill ends with; the harness offers it to copy or to
// start a new session with.
const refineFence = "```refined"

// refinedPrompt is the body of the last refined block in an agent reply.
func refinedPrompt(text string) (string, bool) {
	i := strings.LastIndex(text, refineFence+"\n")
	if i < 0 {
		return "", false
	}
	body := text[i+len(refineFence)+1:]
	if j := strings.Index(body, "\n```"); j >= 0 {
		body = body[:j]
	}
	body = strings.TrimSpace(body)
	return body, body != ""
}

// refined is the refined prompt the open session's last reply ends with, "" when none.
func (m chatView) refined() string {
	if len(m.pending) > 0 {
		return ""
	}
	p, _ := refinedPrompt(m.lastText(m.current))
	return p
}

var refineOptions = []string{"copy", "new session"}

// refineAct runs a strip button: copy the prompt, or start a new session with it.
func (m chatView) refineAct(o string) (chatView, tea.Cmd) {
	p := m.refined()
	if o == "copy" {
		m.status("copied the refined prompt")
		return m, tea.SetClipboard(p)
	}
	m.newSession()
	return m.send(p)
}

// refineOption is the strip button whose first letter is t, when a refined prompt waits.
func (m chatView) refineOption(t string) (string, bool) {
	if m.refined() == "" || len([]rune(t)) != 1 {
		return "", false
	}
	for _, o := range refineOptions {
		if strings.EqualFold(firstRune(o), t) {
			return o, true
		}
	}
	return "", false
}
