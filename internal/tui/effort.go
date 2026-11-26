//go:build harness

package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/daemon"
)

// sessionModelAPI sets a session's model and effort; *daemon.Client is one.
type sessionModelAPI interface {
	SetSessionModel(ctx context.Context, id, model, effort string) (daemon.Session, error)
}

type sessionModelSetMsg struct {
	s   daemon.Session
	err error
}

// openSession is the open session's row, nil before the first message.
func (m *chatView) openSession() *chatSession {
	if m.current == "" {
		return nil
	}
	return m.sessionByID(m.current)
}

func (m *chatView) sessionByID(id string) *chatSession {
	for i := range m.sessions {
		if m.sessions[i].id == id {
			return &m.sessions[i]
		}
	}
	return nil
}

// nextReadyAgent is the ready agent after cur in the setup list, wrapping; "" when none other.
func (m chatView) nextReadyAgent(cur string) string {
	if m.setupSt == nil {
		return ""
	}
	var ready []string
	at := -1
	for _, a := range m.setupSt.Agents {
		if a.Ready {
			if a.Name == cur {
				at = len(ready)
			}
			ready = append(ready, a.Name)
		}
	}
	if len(ready) < 2 && at >= 0 {
		return ""
	}
	if len(ready) == 0 {
		return ""
	}
	return ready[(at+1)%len(ready)]
}

func (m chatView) setSessionModel(id, model, effort string) tea.Cmd {
	api, ok := m.sess.(sessionModelAPI)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		s, err := api.SetSessionModel(context.Background(), id, model, effort)
		return sessionModelSetMsg{s: s, err: err}
	}
}

// sessionModelSet applies the daemon's answer to a model or effort change.
func (m *chatView) sessionModelSet(msg sessionModelSetMsg) {
	if msg.err != nil {
		m.system("error: " + msg.err.Error())
		return
	}
	m.meta(msg.s.ID, msg.s)
	what := msg.s.Model
	if what == "" {
		what = "the agent default"
	}
	eff := msg.s.Effort
	if eff == "" {
		eff = "agent.effort"
	}
	m.status("next turns run " + msg.s.Agent + " · " + what + " · effort " + eff)
}

// effortRows are the effort list: the agent.effort setting, then each effort smithy passes on.
func effortRows() []string { return append([]string{""}, agents.Efforts...) }

// effortCommand is /effort [low|medium] for the open session's agent, when it takes one.
func (m chatView) effortCommand(arg string) (tea.Model, tea.Cmd) {
	s := m.openSession()
	if s == nil {
		m.system("effort is per session: send a message first, then /effort; the agent.effort setting is the default")
		return m, nil
	}
	if !agents.EffortFlags[s.agent] {
		m.system(s.agent + " keeps its own effort; /effort works for claude and codex")
		return m, nil
	}
	if arg != "" {
		if err := daemon.ValidEffort(s.agent, arg); err != nil {
			m.system(err.Error())
			return m, nil
		}
		return m, m.setSessionModel(s.id, s.model, arg)
	}
	m.overlay, m.setupStep, m.setupNote, m.effortSel = overlayPicker, stepEffort, "", 0
	for i, e := range effortRows() {
		if e == s.effort {
			m.effortSel = i
		}
	}
	return m, nil
}

func (m chatView) effortKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := effortRows()
	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
	case "up", "k":
		m.effortSel = max(m.effortSel-1, 0)
	case "down", "j":
		m.effortSel = min(m.effortSel+1, len(rows)-1)
	case "enter":
		m.overlay = overlayNone
		if s := m.openSession(); s != nil {
			return m, m.setSessionModel(s.id, s.model, rows[m.effortSel])
		}
	}
	return m, nil
}

// effortBox is the /effort list for the open session.
func (m chatView) effortBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	agent, cur := "", ""
	if s := m.openSession(); s != nil {
		agent, cur = s.agent, s.effort
	}
	items := []boxItem{{group: "Effort for " + agent + " · this session"}}
	for _, e := range effortRows() {
		it := boxItem{glyph: t.g.dot, color: t.muted, label: e}
		if e == "" {
			it.label, it.hint = "default", "the agent.effort setting"
		}
		if e == cur {
			it.key, it.color = t.g.on+" now", t.accent
		}
		items = append(items, it)
	}
	items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "smithy caps effort at " + agents.MaxEffort + "; higher levels are not offered", static: true})
	foot := "  " + keyHints(t, "↑↓", "move", "enter", "save", "esc", "cancel")
	return m.overBox(screen, m.floatBox(bw, bh, "", "Effort for "+agent, items, m.effortSel, foot), w)
}
