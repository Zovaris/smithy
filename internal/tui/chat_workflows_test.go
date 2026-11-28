//go:build harness

package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/workflow"
)

type fakeFlows struct {
	l    daemon.WorkflowList
	runs []string
	sets []string
}

func (f *fakeFlows) Workflows(context.Context) (daemon.WorkflowList, error) { return f.l, nil }

func (f *fakeFlows) Workflow(_ context.Context, name string) (daemon.WorkflowDetail, error) {
	st := time.Date(2026, 11, 27, 17, 0, 0, 0, time.Local)
	return daemon.WorkflowDetail{Info: f.l.Workflows[0], History: []workflow.Record{{Workflow: name, Trigger: "schedule", Status: "ok", Started: st, Ended: st.Add(time.Minute),
		Steps: []workflow.StepResult{{ID: "judge", Kind: "agent", Answer: "yes", Why: "patch bump"}}}}}, nil
}

func (f *fakeFlows) RunWorkflow(_ context.Context, name, via string) error {
	f.runs = append(f.runs, name+"@"+via)
	return nil
}

func (f *fakeFlows) SetWorkflow(_ context.Context, name string, on bool) (workflow.Info, error) {
	if on {
		f.sets = append(f.sets, "on "+name)
		f.l.Workflows[0].Enabled = true
	} else {
		f.sets = append(f.sets, "off "+name)
	}
	return f.l.Workflows[0], nil
}

func TestWorkflowsModal(t *testing.T) {
	w, _ := workflow.Parse([]byte(workflow.Example))
	f := &fakeFlows{l: daemon.WorkflowList{Workflows: []workflow.Info{{Workflow: w, File: "/nowhere/dependabot-daily.json"}}}}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.flows = f
	m = cpress(typeIn(m, "/workflows"), ckey("enter"))
	if m.overlay != overlayWorkflows {
		t.Fatal("/workflows must open the workflows modal")
	}
	out := screen(m)
	for _, s := range []string{"Workflows", "dependabot-daily", "0 17 * * *", "off", "run now"} {
		if !strings.Contains(out, s) {
			t.Errorf("modal lacks %q:\n%s", s, out)
		}
	}
	m = cpress(m, ckey("right"))
	if len(f.sets) != 1 || f.sets[0] != "on dependabot-daily" || !strings.Contains(screen(m), " on") {
		t.Fatalf("right toggles on: %v", f.sets)
	}
	m = cpress(m, pressCode('r'))
	if len(f.runs) != 1 || f.runs[0] != "dependabot-daily@tui" {
		t.Fatalf("r runs now: %v", f.runs)
	}
	m = cpress(m, ckey("enter"))
	out = screen(m)
	if m.overlay != overlayInfo || !strings.Contains(out, "judge") || !strings.Contains(out, "patch bump") || !strings.Contains(out, "STEPS") {
		t.Fatalf("enter shows history:\n%s", out)
	}
	m = cpress(m, ckey("esc"))
	if m.overlay != overlayWorkflows {
		t.Fatalf("esc goes back to the list, got %v", m.overlay)
	}
}
