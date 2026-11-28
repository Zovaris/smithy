//go:build harness

package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
)

type fakeSessGoal struct {
	fakeSessModel
	goals []string
}

func (f *fakeSessGoal) SetSessionGoal(_ context.Context, id, goal string) (api.Session, error) {
	f.goals = append(f.goals, id+"|"+goal)
	return api.Session{ID: id, Agent: "claude", Goal: goal, Status: daemon.StatusDone}, nil
}

func goalScene() (*fakeSessGoal, chatView) {
	_, _, m := sessionScene()
	fg := &fakeSessGoal{}
	m.sess = fg
	return fg, m
}

func TestGoalCommandSetsShowsAndClears(t *testing.T) {
	fg, m := goalScene()
	m2, cmd := m.command("/goal make the tests pass")
	m = run(m2.(chatView), cmd)
	if len(fg.goals) != 1 || fg.goals[0] != "s-1|make the tests pass" {
		t.Fatalf("set %v", fg.goals)
	}
	if !strings.Contains(screen(m), "goal") || !strings.Contains(screen(m), "make the tests pass") {
		t.Fatalf("the header shows the pinned goal:\n%s", screen(m))
	}
	m2, cmd = m.command("/goal")
	m = run(m2.(chatView), cmd)
	if len(fg.goals) != 1 || !strings.Contains(screen(m), "make the tests pass") {
		t.Fatalf("/goal alone shows it, sets nothing: %v\n%s", fg.goals, screen(m))
	}
	m2, cmd = m.command("/goal clear")
	run(m2.(chatView), cmd)
	if fg.goals[len(fg.goals)-1] != "s-1|" {
		t.Fatalf("/goal clear drops it: %v", fg.goals)
	}
}

func TestGoalEventsLandInTranscript(t *testing.T) {
	_, m := goalScene()
	ev := func(g, st string) {
		b, _ := json.Marshal(map[string]any{"goal": g, "state": st})
		m.sessionEvent(daemon.KindSessionGoal, "s-1", b)
	}
	ev("ship it", "")
	ev("ship it", daemon.GoalMet)
	out := screen(m)
	if !strings.Contains(out, "goal set: ship it") || !strings.Contains(out, "goal met") {
		t.Fatalf("goal set and met show in the transcript:\n%s", out)
	}
	if s := m.openSession(); s == nil || s.goal != "ship it" || s.goalState != daemon.GoalMet {
		t.Fatalf("session goal not tracked: %+v", s)
	}
}

func TestGoalNeedsASession(t *testing.T) {
	_, m := goalScene()
	m.current = ""
	m2, cmd := m.command("/goal x")
	m = run(m2.(chatView), cmd)
	if !strings.Contains(screen(m), "open a session") {
		t.Fatalf("no session: says why\n%s", screen(m))
	}
}
