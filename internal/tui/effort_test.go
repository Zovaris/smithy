//go:build harness

package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/daemon"
)

// fakeSessModel is a sessions API that also sets a session's model and effort.
type fakeSessModel struct {
	fakeSessions
	set []string
}

func (f *fakeSessModel) SetSessionModel(_ context.Context, id, model, effort string) (daemon.Session, error) {
	f.set = append(f.set, id+"|"+model+"|"+effort)
	return daemon.Session{ID: id, Agent: "claude", Model: model, Effort: effort, Status: daemon.StatusDone}, nil
}

// sessionScene is the models scene with session s-1 (claude) open.
func sessionScene() (*fakeModels, *fakeSessModel, chatView) {
	f, m := modelsScene()
	f.s.Agents = append(f.s.Agents, agents.Ready{Name: "opencode", Kind: "cli", Ready: true})
	m.setupSt.Agents = f.s.Agents
	fs := &fakeSessModel{}
	m.sess = fs
	m.current = "s-1"
	m.meta("s-1", daemon.Session{ID: "s-1", Agent: "claude", Model: "sonnet", Status: daemon.StatusDone})
	return f, fs, m
}

func TestModelInASessionSetsThatSession(t *testing.T) {
	f, fs, m := sessionScene()
	m2, cmd := m.modelCommand("")
	m = run(m2.(chatView), cmd)
	if !strings.Contains(screen(m), "this session") {
		t.Fatalf("the modal says it changes the open session:\n%s", screen(m))
	}
	m = sendKey(typeIn(m, "beam"), ckey("enter"))
	if len(fs.set) != 1 || fs.set[0] != "s-1|opencode/beam-free|" {
		t.Fatalf("set %v", fs.set)
	}
	if len(f.set) != 0 {
		t.Errorf("a session's model leaves the default alone: %v", f.set)
	}
	if m.overlay != overlayNone {
		t.Errorf("the modal closes after the save")
	}
	m2, cmd = m.modelCommand("haiku")
	run(m2.(chatView), cmd)
	if fs.set[len(fs.set)-1] != "s-1|haiku|" {
		t.Errorf("/model <name> sets the session: %v", fs.set)
	}
}

func TestModelTabListsTheOtherAgents(t *testing.T) {
	f, _, m := sessionScene()
	m.current = ""
	m2, cmd := m.modelCommand("")
	m = run(m2.(chatView), cmd)
	m = sendKey(m, ckey("tab"))
	if m.modelFor == "claude" {
		t.Fatalf("tab moves to the next ready agent")
	}
	for m.modelFor != "opencode" {
		m = sendKey(m, ckey("tab"))
		if m.modelFor == "claude" {
			t.Fatalf("tab never reached opencode")
		}
	}
	if !strings.Contains(screen(m), "Model for opencode") || !strings.Contains(screen(m), "opencode/big-pickle") {
		t.Fatalf("opencode's models are listed:\n%s", screen(m))
	}
	sendKey(typeIn(m, "beam"), ckey("enter"))
	if f.set[len(f.set)-1] != "opencode|opencode/beam-free" {
		t.Fatalf("picking an opencode model makes opencode the default with it: %v", f.set)
	}
}

func TestEffortModal(t *testing.T) {
	_, fs, m := sessionScene()
	m2, cmd := m.command("/effort")
	m = run(m2.(chatView), cmd)
	out := screen(m)
	for _, want := range []string{"Effort for claude", "low", "medium", "agent.effort"} {
		if !strings.Contains(out, want) {
			t.Fatalf("effort modal lacks %q:\n%s", want, out)
		}
	}
	m = sendKey(sendKey(m, ckey("down")), ckey("enter"))
	if len(fs.set) != 1 || fs.set[0] != "s-1|sonnet|low" {
		t.Fatalf("set %v", fs.set)
	}
	m2, cmd = m.command("/effort medium")
	run(m2.(chatView), cmd)
	if fs.set[len(fs.set)-1] != "s-1|sonnet|medium" {
		t.Fatalf("/effort medium: %v", fs.set)
	}
}

func TestEffortRefusedWhereItDoesNotApply(t *testing.T) {
	_, fs, m := sessionScene()
	m.meta("s-1", daemon.Session{ID: "s-1", Agent: "opencode", Status: daemon.StatusDone})
	m2, _ := m.command("/effort low")
	m = m2.(chatView)
	if len(fs.set) != 0 || !strings.Contains(screen(m), "opencode keeps its own effort") {
		t.Fatalf("opencode takes no effort: %v\n%s", fs.set, screen(m))
	}
	m.current = ""
	m2, _ = m.command("/effort")
	if !strings.Contains(screen(m2.(chatView)), "per session") {
		t.Fatalf("no session: says effort is per session:\n%s", screen(m2.(chatView)))
	}
}
