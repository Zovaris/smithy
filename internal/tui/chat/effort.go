//go:build harness

package chat

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
)

type sessionModelAPI interface {
	SetSessionModel(ctx context.Context, id, model, effort string) (api.Session, error)
}

type sessionModelSetMsg struct {
	s   api.Session
	err error
}

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
		ctx, cancel := daemonCtx()
		defer cancel()
		s, err := api.SetSessionModel(ctx, id, model, effort)
		return sessionModelSetMsg{s: s, err: err}
	}
}

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
	m.shimmerOn(msg.s.Effort)
}

// shimmerFrames is how many motion frames the top-effort shimmer plays on the composer.
const shimmerFrames = 24

// shimmerOn starts the composer shimmer when level is the top effort; reduced motion plays none.
func (m *chatView) shimmerOn(level string) {
	if level == agents.MaxEffort && !m.still {
		m.shimmer = shimmerFrames
	}
}

// effortDefaultMsg is agent.effort saved: level, "" when put back to its default.
type effortDefaultMsg struct {
	level string
	err   error
}

// effortLevel normalizes a typed level: off and default mean "" (the agent.effort default).
func effortLevel(arg string) string {
	if a := strings.ToLower(strings.TrimSpace(arg)); a != "off" && a != "default" {
		return a
	}
	return ""
}

// setDefaultEffort saves agent.effort, the effort the next session starts with.
func (m chatView) setDefaultEffort(level string) tea.Cmd {
	api := m.cfg
	if api == nil {
		return nil
	}
	return func() tea.Msg {
		if level == "" {
			ctx, cancel := daemonCtx()
			defer cancel()
			return effortDefaultMsg{err: api.UnsetSetting(ctx, "agent.effort")}
		}
		ctx, cancel := daemonCtx()
		defer cancel()
		return effortDefaultMsg{level: level, err: api.SetSetting(ctx, "agent.effort", level)}
	}
}

func (m *chatView) effortDefaultSet(msg effortDefaultMsg) {
	if msg.err != nil {
		m.system("error: " + msg.err.Error())
		return
	}
	if msg.level == "" {
		m.effort = agents.MaxEffort
		m.system("agent.effort back to its default (" + agents.MaxEffort + "): the next session starts with it")
		return
	}
	m.effort = msg.level
	m.system("saved agent.effort = " + msg.level + ": the next session starts with it")
	m.shimmerOn(msg.level)
}

func effortRows() []string { return append([]string{""}, agents.Efforts...) }

func (m chatView) effortCommand(arg string) (tea.Model, tea.Cmd) {
	off := strings.TrimSpace(arg) != "" && effortLevel(arg) == ""
	arg = effortLevel(arg)
	s := m.openSession()
	if s == nil {
		if off {
			return m, m.setDefaultEffort("")
		}
		if arg != "" {
			if e, capped := agents.ClampEffort(arg); capped || e != arg {
				m.system("effort must be off or one of " + strings.Join(agents.Efforts, ", ") + " (smithy caps effort at " + agents.MaxEffort + ")")
				return m, nil
			}
			return m, m.setDefaultEffort(arg)
		}
		m.overlay, m.setupStep, m.setupNote, m.effortSel = overlayPicker, stepEffort, "", 0
		for i, e := range effortRows() {
			if e == m.effort {
				m.effortSel = i
			}
		}
		return m, nil
	}
	if !agents.EffortFlags[s.agent] {
		m.system(s.agent + " keeps its own effort; /effort works for claude and codex")
		return m, nil
	}
	if off {
		return m, m.setSessionModel(s.id, s.model, "")
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
	case "left", "h":
		m.effortSel = (m.effortSel + len(rows) - 1) % len(rows)
	case "right", "l":
		m.effortSel = (m.effortSel + 1) % len(rows)
	case "enter":
		m.overlay = overlayNone
		if s := m.openSession(); s != nil {
			return m, m.setSessionModel(s.id, s.model, rows[m.effortSel])
		}
		return m, m.setDefaultEffort(rows[m.effortSel])
	}
	return m, nil
}

func (m chatView) effortBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	agent, cur, scope, title := "", m.effort, " · the next session (agent.effort)", "Effort"
	if s := m.openSession(); s != nil {
		agent, cur, scope = s.agent, s.effort, " · this session"
		title = "Effort for " + agent
	}
	items := []boxItem{{group: title + scope}}
	for _, e := range effortRows() {
		it := boxItem{glyph: t.G.Dot, color: t.Muted, label: e}
		if e == "" {
			it.label, it.hint = "default", "the agent.effort setting"
			if agent == "" {
				it.hint = "back to " + agents.MaxEffort
			}
		}
		if e == cur {
			it.key, it.color = t.G.On+" now", t.Accent
		}
		items = append(items, it)
	}
	items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: "smithy caps effort at " + agents.MaxEffort + "; higher levels are not offered", static: true})
	foot := "  " + keyHints(t, "↑↓←→", "move", "enter", "save", "esc", "cancel")
	return m.overBox(screen, m.floatBox(bw, bh, "", title, items, m.effortSel, foot), w)
}
