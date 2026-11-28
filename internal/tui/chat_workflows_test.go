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
	sl   daemon.ScheduleList
	runs []string
	sets []string
}

func (f *fakeFlows) Workflows(context.Context) (daemon.WorkflowList, error) { return f.l, nil }

func (f *fakeFlows) Workflow(_ context.Context, name string) (daemon.WorkflowDetail, error) {
	st := time.Date(2026, 11, 27, 17, 0, 0, 0, time.Local)
	return daemon.WorkflowDetail{Info: f.l.Workflows[0], History: []workflow.Record{{Workflow: name, Trigger: "schedule", Status: "ok", Started: st, Ended: st.Add(time.Minute),
		Steps: []workflow.StepResult{{ID: "judge", Kind: "agent", Answer: "yes", Why: "patch bump"}}}}}, nil
}

func (f *fakeFlows) RunWorkflow(_ context.Context, name, via string, _ map[string]string) error {
	f.runs = append(f.runs, name+"@"+via)
	return nil
}

func (f *fakeFlows) Schedules(context.Context) (daemon.ScheduleList, error) { return f.sl, nil }

func (f *fakeFlows) Schedule(_ context.Context, name string) (daemon.ScheduleDetail, error) {
	return daemon.ScheduleDetail{Info: f.sl.Schedules[0]}, nil
}

func (f *fakeFlows) SetSchedule(_ context.Context, name string, on bool) (workflow.SchedInfo, error) {
	if on {
		f.sets = append(f.sets, "on "+name)
		f.sl.Schedules[0].Enabled = true
	}
	return f.sl.Schedules[0], nil
}

func TestWorkflowNoteShowsInChat(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.event(daemon.KindWorkflowNote, "", time.Now(), []byte(`{"workflow":"dependabot-daily","title":"Dependabot: merge? yes","text":"patch bump, CI green"}`))
	if out := screen(m); !strings.Contains(out, "workflow dependabot-daily · Dependabot: merge? yes: patch bump, CI green") {
		t.Fatalf("note not shown:\n%s", out)
	}
}

func TestWorkflowsModal(t *testing.T) {
	w, _ := workflow.Parse([]byte(workflow.Example))
	sch, _ := workflow.ParseSchedule([]byte(workflow.ExampleSchedules["dependabot-daily"]))
	f := &fakeFlows{l: daemon.WorkflowList{Workflows: []workflow.Info{{Workflow: w, File: "/nowhere/dependabot-daily.json", Schedules: []string{"dependabot-daily"}}}},
		sl: daemon.ScheduleList{Schedules: []workflow.SchedInfo{{Schedule: sch}}}}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.flows = f
	m = cpress(typeIn(m, "/workflows"), ckey("enter"))
	if m.overlay != overlayWorkflows {
		t.Fatal("/workflows must open the modal")
	}
	out := screen(m)
	for _, s := range []string{"Workflows", "dependabot-daily", "4 steps", "schedules dependabot-daily", "run"} {
		if !strings.Contains(out, s) {
			t.Errorf("modal lacks %q:\n%s", s, out)
		}
	}
	m = cpress(m, pressCode('r'))
	if len(f.runs) != 1 || f.runs[0] != "dependabot-daily@tui" {
		t.Fatalf("r runs: %v", f.runs)
	}
	m = cpress(m, ckey("enter"))
	if out = screen(m); m.overlay != overlayInfo || !strings.Contains(out, "judge") || !strings.Contains(out, "patch bump") || !strings.Contains(out, "STEPS") {
		t.Fatalf("enter shows steps and runs:\n%s", out)
	}
	m = cpress(m, ckey("esc"))
	m = cpress(m, ckey("tab"))
	if out = screen(m); !strings.Contains(out, "0 17 * * * → dependabot-daily") || !strings.Contains(out, "off") {
		t.Fatalf("schedules tab:\n%s", out)
	}
	m = cpress(m, ckey("right"))
	if len(f.sets) != 1 || f.sets[0] != "on dependabot-daily" {
		t.Fatalf("right enables: %v", f.sets)
	}
}

func TestRunCommand(t *testing.T) {
	f := &fakeFlows{}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.flows = f
	m = cpress(typeIn(m, "/run greet who=ana"), ckey("enter"))
	if len(f.runs) != 1 || f.runs[0] != "greet@tui" {
		t.Fatalf("%v", f.runs)
	}
}
