//go:build harness

package chat

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/tui/kit"
)

// maxQueue caps the messages waiting for one session's turn to end.
const maxQueue = 20

// busy reports whether session id is in a turn: working or waiting on a question.
func (m chatView) busy(id string) bool {
	if id == "" {
		return false
	}
	for _, s := range m.sessions {
		if s.id == id {
			return s.state == "running" || s.state == "waiting"
		}
	}
	return false
}

// enqueue keeps text for when session id's turn ends; a message taken back with up goes back
// to the head of the queue.
func (m *chatView) enqueue(id, text string) {
	if m.queues == nil {
		m.queues = map[string][]string{}
	}
	q := m.queues[id]
	if len(q) >= maxQueue {
		m.status("the queue is full; wait for the turn to end")
		return
	}
	if m.qEdit {
		q = append([]string{text}, q...)
	} else {
		q = append(q, text)
	}
	m.queues[id], m.qEdit = q, false
	m.status("queued: sent when the turn ends · ↑ edits the next one")
}

// takeQueued puts the next queued message of the open session in the input to edit; an empty
// input then drops it.
func (m *chatView) takeQueued() {
	q := m.queues[m.current]
	m.input.SetValue(q[0])
	m.input.CursorEnd()
	m.queues[m.current], m.qEdit = q[1:], true
}

// drain sends the next queued message of every session whose turn has ended.
func (m *chatView) drain() tea.Cmd {
	if !m.busy(m.current) {
		m.stopAsked = false
	}
	if m.sess == nil {
		return nil
	}
	var cmds []tea.Cmd
	for id, q := range m.queues {
		if len(q) == 0 || m.busy(id) || (id == m.current && m.qEdit) {
			continue
		}
		text, api := q[0], m.sess
		m.queues[id] = q[1:]
		m.track(id, "working")
		cmds = append(cmds, func() tea.Msg {
			if err := api.SendSession(context.Background(), id, text); err != nil {
				return sessionErrMsg{what: "send", err: err}
			}
			return nil
		})
	}
	return tea.Batch(cmds...)
}

// queueRows is the open session's queue under the input: up to three, then a count.
func (m chatView) queueRows(w int) []string {
	q := m.queues[m.current]
	if len(q) == 0 {
		return nil
	}
	t := m.th
	var out []string
	for i, s := range q {
		if i == 3 {
			out = append(out, t.Fg(t.Dim).Render(kit.Short("  + "+strconv.Itoa(len(q)-3)+" more queued", w)))
			break
		}
		lead := "  queued  "
		if i == 0 {
			lead = "  queued  ↑ "
		}
		out = append(out, t.Fg(t.Dim).Render(lead)+t.Fg(t.Text).Render(kit.Short(kit.Clean(s), max(w-14, 8))))
	}
	return out
}

// switchTab opens the tab d steps away in the tab strip, the unsent new session counting as one.
func (m *chatView) switchTab(d int) {
	var ids []string
	for _, s := range m.listed() {
		ids = append(ids, s.id)
	}
	if m.current == "" {
		ids = append(ids, "")
	}
	if len(ids) < 2 {
		return
	}
	i := 0
	for j, id := range ids {
		if id == m.current {
			i = j
		}
	}
	m.open(ids[(i+d+len(ids))%len(ids)])
}
