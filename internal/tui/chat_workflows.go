//go:build harness

package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/workflow"
)

// workflowAPI is the part of the daemon client /workflows uses.
type workflowAPI interface {
	Workflows(ctx context.Context) (daemon.WorkflowList, error)
	Workflow(ctx context.Context, name string) (daemon.WorkflowDetail, error)
	RunWorkflow(ctx context.Context, name, via string) error
	SetWorkflow(ctx context.Context, name string, on bool) (workflow.Info, error)
}

// workflowsView is the open /workflows: the list and the selected workflow.
type workflowsView struct {
	l    *daemon.WorkflowList
	name string
	err  string
}

type workflowsMsg struct {
	l   daemon.WorkflowList
	err error
}

type workflowDoneMsg struct {
	what, name string
	err        error
}

type workflowDetailMsg struct {
	d   daemon.WorkflowDetail
	err error
}

// openWorkflows is /workflows: the scheduled workflows in the shared modal.
func (m chatView) openWorkflows() (tea.Model, tea.Cmd) {
	if m.flows == nil {
		m.system("/workflows needs the daemon")
		return m, nil
	}
	m.overlay, m.flowSt = overlayWorkflows, workflowsView{}
	return m, m.loadWorkflows()
}

func (m chatView) loadWorkflows() tea.Cmd {
	api := m.flows
	return func() tea.Msg {
		l, err := api.Workflows(context.Background())
		return workflowsMsg{l: l, err: err}
	}
}

func (m chatView) workflowsLoaded(msg workflowsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.flowSt.err = msg.err.Error()
		return m, nil
	}
	m.flowSt.l, m.flowSt.err = &msg.l, ""
	if m.flowSel() < 0 && len(msg.l.Workflows) > 0 {
		m.flowSt.name = msg.l.Workflows[0].Name
	}
	return m, nil
}

func (m chatView) flowSel() int {
	if m.flowSt.l == nil {
		return -1
	}
	for i, w := range m.flowSt.l.Workflows {
		if w.Name == m.flowSt.name {
			return i
		}
	}
	return -1
}

func (m chatView) flowRunning(name string) bool {
	if m.flowSt.l == nil {
		return false
	}
	for _, n := range m.flowSt.l.Running {
		if n == name {
			return true
		}
	}
	return false
}

func (m chatView) workflowsBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	st := m.flowSt
	var items []boxItem
	switch {
	case st.err != "":
		items = append(items, boxItem{glyph: t.g.off, color: t.err, label: st.err, static: true})
	case st.l == nil:
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "reading the workflows…", static: true})
	case len(st.l.Workflows) == 0:
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "no workflows yet", hint: "smithy workflow example", static: true})
	default:
		for _, in := range st.l.Workflows {
			g, col, key := t.g.off, t.muted, "off"
			switch {
			case m.flowRunning(in.Name):
				g, col, key = t.g.half, t.info, "running"
			case in.Err != "":
				g, col, key = t.g.fail, t.err, "error"
			case in.Changed:
				g, col, key = t.g.wait, t.warn, "changed"
			case in.Enabled:
				g, col, key = t.g.on, t.ok, "on"
			}
			hint := []string{in.Schedule}
			if !in.Next.IsZero() {
				hint = append(hint, "next "+in.Next.Local().Format("Mon 15:04"))
			}
			if in.Last != nil {
				hint = append(hint, "last "+in.Last.Status)
			}
			items = append(items, boxItem{glyph: g, color: col, label: in.Name, hint: strings.Join(hint, " "+t.g.dot+" "), key: key})
		}
	}
	sel := m.flowSel()
	if sel < 0 {
		sel = 0
	}
	foot := "  " + keyHints(t, "↑↓", "move", "←→", "on/off", "r", "run now", "enter", "history", "e", "edit", "esc", "close")
	return m.overBox(screen, m.floatBox(bw, bh, "", "Workflows", items, sel, foot), w)
}

func (m chatView) workflowsKey(k string) (tea.Model, tea.Cmd) {
	var list []workflow.Info
	if m.flowSt.l != nil {
		list = m.flowSt.l.Workflows
	}
	cur := m.flowSel()
	api := m.flows
	switch k {
	case "esc", "q":
		m.overlay = overlayNone
		return m, nil
	case "up", "k":
		if cur > 0 {
			m.flowSt.name = list[cur-1].Name
		}
		return m, nil
	case "down", "j":
		if cur+1 < len(list) {
			m.flowSt.name = list[cur+1].Name
		}
		return m, nil
	}
	if cur < 0 {
		return m, nil
	}
	in := list[cur]
	switch k {
	case "left", "right", "h", "l", "space":
		on := !in.Enabled
		return m, func() tea.Msg {
			_, err := api.SetWorkflow(context.Background(), in.Name, on)
			what := "disabled"
			if on {
				what = "enabled"
			}
			return workflowDoneMsg{what: what, name: in.Name, err: err}
		}
	case "r":
		m.status("running " + in.Name)
		return m, func() tea.Msg {
			return workflowDoneMsg{what: "started", name: in.Name, err: api.RunWorkflow(context.Background(), in.Name, "tui")}
		}
	case "enter":
		return m, func() tea.Msg {
			d, err := api.Workflow(context.Background(), in.Name)
			return workflowDetailMsg{d: d, err: err}
		}
	case "e":
		if _, err := os.Stat(in.File); err != nil {
			m.status("the file is on the daemon's machine: " + in.File)
			return m, nil
		}
		open := m.opener
		if open == nil {
			open = openURL
		}
		m.status("opening " + in.File + " (a change made outside smithy needs enabling again)")
		f := in.File
		return m, func() tea.Msg {
			if err := open(f); err != nil {
				return sessionErrMsg{what: "open the workflow file", err: err}
			}
			return nil
		}
	}
	return m, nil
}

func (m chatView) workflowDone(msg workflowDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status(msg.name + ": " + msg.err.Error())
		return m, nil
	}
	m.status(msg.name + " " + msg.what)
	return m, m.loadWorkflows()
}

// workflowDetail shows a workflow's steps and newest runs with each step's result; esc goes back.
func (m chatView) workflowDetail(msg workflowDetailMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status(msg.err.Error())
		return m, nil
	}
	d := msg.d
	lines := []string{"-> " + d.Info.File, "#STEPS"}
	for i, s := range d.Info.Steps {
		lines = append(lines, fmt.Sprintf("%d. %s %s\t%s", i+1, s.Kind, s.ID, workflowStepText(s)))
	}
	lines = append(lines, "#RUNS")
	if len(d.History) == 0 {
		lines = append(lines, "no runs yet: r runs it now")
	}
	for _, r := range d.History {
		head := fmt.Sprintf("%s %s %s\t%s", r.Started.Local().Format("Jan 2 15:04"), r.Status, r.Trigger, r.Ended.Sub(r.Started).Round(time.Second))
		if r.Error != "" {
			head += " · " + r.Error
		}
		lines = append(lines, head)
		for _, s := range r.Steps {
			lines = append(lines, "  "+s.ID+"\t"+workflowResultText(s))
		}
	}
	m.info = infoPanel{title: d.Info.Name + " · " + d.Info.Schedule, lines: lines, scroll: true}
	m.overlay, m.infoBack = overlayInfo, overlayWorkflows
	return m, nil
}

func workflowStepText(s workflow.Step) string {
	switch s.Kind {
	case workflow.KindRun:
		return clipLine(s.Run, 80)
	case workflow.KindAgent:
		return clipLine(s.Prompt, 70)
	case workflow.KindIf:
		return "on " + s.Step
	}
	return clipLine(s.Title+" "+s.Message, 80)
}

func workflowResultText(s workflow.StepResult) string {
	switch {
	case s.Error != "":
		return "error: " + clipLine(s.Error, 90)
	case s.Kind == workflow.KindIf:
		return "→ " + s.Branch
	case s.Answer != "":
		return s.Answer + " " + clipLine(s.Why, 80)
	case s.Kind == workflow.KindRun:
		return fmt.Sprintf("exit %d · %s", s.Exit, clipLine(s.Output, 80))
	}
	return clipLine(s.Output, 90)
}

func clipLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
