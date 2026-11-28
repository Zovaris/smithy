//go:build harness

package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
		if strings.EqualFold(o[:1], t) {
			return o, true
		}
	}
	return "", false
}

// refineHit is where a button sits on the refine row, for a click.
type refineHit struct {
	x0, x1 int
	option string
}

// refineRow is the row under the transcript while a refined prompt waits: the label, then each
// button with its key letter, the first on the accent tier as the ask card's pick.
func (m chatView) refineRow(w int) (string, []refineHit) {
	t := m.th
	s := strings.Repeat(" ", m.pad()) + "  " + t.Fg(t.Strong).Render("refined prompt ready") + "  "
	var hits []refineHit
	for n, o := range refineOptions {
		x := ansi.StringWidth(s)
		st := lipgloss.NewStyle().Foreground(t.Text)
		if t.Focus != nil {
			st = st.Background(t.Focus)
		}
		if n == 0 {
			st = lipgloss.NewStyle().Foreground(t.Surface).Bold(true)
			if t.Accent != nil {
				st = st.Background(t.Accent)
			}
		}
		s += st.Render(" " + o[:1] + "  " + o + " ")
		hits = append(hits, refineHit{x0: x, x1: ansi.StringWidth(s), option: o})
		s += " "
	}
	return spread(s, t.Fg(t.Dim).Render("letter, then enter")+strings.Repeat(" ", m.pad()+2), w), hits
}
