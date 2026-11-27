//go:build harness

package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
)

type sessionGoalAPI interface {
	SetSessionGoal(ctx context.Context, id, goal string) (daemon.Session, error)
}

type sessionGoalSetMsg struct {
	s   daemon.Session
	err error
}

// goalCommand is /goal: no argument shows the open session's goal, "clear" drops it, any other
// text pins it. Only a person sets it; the daemon continues the session until it is met.
func (m chatView) goalCommand(arg string) (tea.Model, tea.Cmd) {
	s := m.openSession()
	if s == nil {
		m.system("open a session first: /goal pins a goal on the open session")
		return m, nil
	}
	if arg == "" {
		if s.goal == "" {
			m.system("no goal on this session; /goal <text> pins one")
		} else {
			m.system("goal" + goalStateLabel(s.goalState) + ": " + s.goal)
		}
		return m, nil
	}
	if arg == "clear" {
		arg = ""
	}
	api, ok := m.sess.(sessionGoalAPI)
	if !ok {
		m.system("this daemon does not take goals")
		return m, nil
	}
	id := s.id
	return m, func() tea.Msg {
		s, err := api.SetSessionGoal(context.Background(), id, arg)
		return sessionGoalSetMsg{s: s, err: err}
	}
}

func (m *chatView) sessionGoalSet(msg sessionGoalSetMsg) {
	if msg.err != nil {
		m.system("error: " + msg.err.Error())
		return
	}
	m.goalSeen(msg.s.ID, msg.s.Goal, msg.s.GoalState)
}

// goalSeen records a session's goal and notes each change in its transcript.
func (m *chatView) goalSeen(id, goal, state string) {
	s := m.sessionByID(id)
	if s == nil || (s.goal == goal && s.goalState == state) {
		return
	}
	switch {
	case goal == "":
		m.into(id, chatMsg{who: "daemon", kind: "system", text: "goal cleared"})
	case goal != s.goal || state == "":
		m.into(id, chatMsg{who: "daemon", kind: "system", text: "goal set: " + goal})
	default:
		m.into(id, chatMsg{who: "daemon", kind: "system", text: "goal" + goalStateLabel(state)})
	}
	s.goal, s.goalState = goal, state
}

func goalStateLabel(state string) string {
	switch state {
	case daemon.GoalMet:
		return " met"
	case daemon.GoalAsked:
		return " paused: the agent asks you"
	case daemon.GoalCapped:
		return " paused: turn or cost cap reached (session.goal_max_*)"
	}
	return ""
}

// goalTag is the header's note of the open session's goal; "" when it has none.
func (m chatView) goalTag() string {
	s := m.openSession()
	if s == nil || s.goal == "" {
		return ""
	}
	g := strings.Join(strings.Fields(s.goal), " ")
	if r := []rune(g); len(r) > 40 {
		g = string(r[:39]) + "…"
	}
	tag := "goal: " + g
	switch s.goalState {
	case daemon.GoalMet:
		tag += " " + m.th.g.pass
	case daemon.GoalAsked, daemon.GoalCapped:
		tag += " · paused"
	}
	return tag
}
