//go:build harness

package chat

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/tui/kit"
	"github.com/Zovaris/smithy/internal/workflow"
)

// workflowAPI is the part of the daemon client /workflows and /schedules use.
type workflowAPI interface {
	Workflows(ctx context.Context) (api.WorkflowList, error)
	Workflow(ctx context.Context, name string) (api.WorkflowDetail, error)
	RunWorkflow(ctx context.Context, name, via string, inputs map[string]string) error
	Schedules(ctx context.Context) (api.ScheduleList, error)
	Schedule(ctx context.Context, name string) (api.ScheduleDetail, error)
	SetSchedule(ctx context.Context, name string, on bool) (workflow.SchedInfo, error)
}

// workflowsView is the open /workflows or /schedules (tab switches): the lists and the selection.
type workflowsView struct {
	sched bool
	wl    *api.WorkflowList
	sl    *api.ScheduleList
	name  string
	err   string
}

type workflowsMsg struct {
	wl  api.WorkflowList
	sl  api.ScheduleList
	err error
}

type workflowDoneMsg struct {
	what, name string
	err        error
}

type workflowDetailMsg struct {
	d   api.WorkflowDetail
	sd  *api.ScheduleDetail
	err error
}

// openWorkflows is /workflows (or /schedules with sched): the lists in the shared modal.
func (m chatView) openWorkflows(sched bool) (tea.Model, tea.Cmd) {
	if m.flows == nil {
		m.system("/workflows needs the daemon")
		return m, nil
	}
	m.overlay, m.flowSt = overlayWorkflows, workflowsView{sched: sched}
	return m, m.loadWorkflows()
}

// runWorkflowCmd is /run <name> [k=v]...: start a workflow with those inputs.
func (m chatView) runWorkflowCmd(arg string) (tea.Model, tea.Cmd) {
	f := strings.Fields(arg)
	if m.flows == nil || len(f) == 0 {
		m.system("usage: /run <workflow> [input=value]…  (/workflows lists them)")
		return m, nil
	}
	in := map[string]string{}
	for _, kv := range f[1:] {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			in[k] = v
		}
	}
	api, name := m.flows, f[0]
	m.status("running " + name)
	return m, func() tea.Msg {
		return workflowDoneMsg{what: "started: /workflows shows it live", name: name, err: api.RunWorkflow(context.Background(), name, "tui", in)}
	}
}

func (m chatView) loadWorkflows() tea.Cmd {
	api := m.flows
	return func() tea.Msg {
		wl, err := api.Workflows(context.Background())
		if err != nil {
			return workflowsMsg{err: err}
		}
		sl, err := api.Schedules(context.Background())
		return workflowsMsg{wl: wl, sl: sl, err: err}
	}
}

func (m chatView) workflowsLoaded(msg workflowsMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.flowSt.err = msg.err.Error()
		return m, nil
	}
	m.flowSt.wl, m.flowSt.sl, m.flowSt.err = &msg.wl, &msg.sl, ""
	if m.flowSel() < 0 {
		m.flowSt.name = ""
		if n := m.flowNames(); len(n) > 0 {
			m.flowSt.name = n[0]
		}
	}
	return m, nil
}

func (m chatView) flowNames() []string {
	var out []string
	switch {
	case m.flowSt.sched && m.flowSt.sl != nil:
		for _, s := range m.flowSt.sl.Schedules {
			out = append(out, s.Name)
		}
	case !m.flowSt.sched && m.flowSt.wl != nil:
		for _, w := range m.flowSt.wl.Workflows {
			out = append(out, w.Name)
		}
	}
	return out
}

func (m chatView) flowSel() int {
	for i, n := range m.flowNames() {
		if n == m.flowSt.name {
			return i
		}
	}
	return -1
}

func (m chatView) flowRunning(name string) bool {
	if m.flowSt.wl == nil {
		return false
	}
	for _, n := range m.flowSt.wl.Running {
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
	title := "Workflows " + t.G.Dot + " [workflows] schedules"
	if st.sched {
		title = "Schedules " + t.G.Dot + " workflows [schedules]"
	}
	var items []boxItem
	switch {
	case st.err != "":
		items = append(items, boxItem{glyph: t.G.Off, color: t.Err, label: st.err, static: true})
	case st.wl == nil:
		items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: "reading the workflows…", static: true})
	case len(m.flowNames()) == 0 && st.sched:
		items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: "no schedules yet", hint: `smithy schedule add <name> --workflow <w> --cron "0 9 * * *"`, static: true})
	case len(m.flowNames()) == 0:
		items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: "no workflows yet", hint: "smithy workflow example", static: true})
	case st.sched:
		for _, in := range st.sl.Schedules {
			g, col, key := t.G.Off, t.Muted, "off"
			switch {
			case in.Err != "":
				g, col, key = t.G.Fail, t.Err, "error"
			case in.Changed:
				g, col, key = t.G.Wait, t.Warn, "changed"
			case in.Enabled:
				g, col, key = t.G.On, t.Ok, "on"
			}
			hint := []string{in.Trigger() + " → " + in.Workflow}
			if in.Next != nil {
				hint = append(hint, "next "+in.Next.Local().Format("Mon 15:04"))
			}
			if in.Last != nil {
				hint = append(hint, "last "+in.Last.Status)
			}
			items = append(items, boxItem{glyph: g, color: col, label: in.Name, hint: strings.Join(hint, " "+t.G.Dot+" "), key: key})
		}
	default:
		for _, in := range st.wl.Workflows {
			g, col, key := t.G.Off, t.Muted, ""
			switch {
			case m.flowRunning(in.Name):
				g, col, key = t.G.Half, t.Info, "running"
			case in.Err != "":
				g, col, key = t.G.Fail, t.Err, "error"
			case in.Last != nil && in.Last.Status == workflow.StatusOK:
				g, col = t.G.Pass, t.Ok
			case in.Last != nil:
				g, col = t.G.Fail, t.Warn
			}
			hint := []string{fmt.Sprintf("%d steps", len(in.Steps))}
			if len(in.Schedules) > 0 {
				hint = append(hint, "schedules "+strings.Join(in.Schedules, ", "))
			}
			if in.Last != nil {
				hint = append(hint, "last "+in.Last.Status)
			}
			items = append(items, boxItem{glyph: g, color: col, label: in.Name, hint: strings.Join(hint, " "+t.G.Dot+" "), key: key})
		}
	}
	sel := max(m.flowSel(), 0)
	foot := "  " + keyHints(t, "↑↓", "move", "r", "run", "enter", "steps · live · history", "e", "edit", "tab", "schedules", "esc", "close")
	if st.sched {
		foot = "  " + keyHints(t, "↑↓", "move", "←→", "on/off", "enter", "history", "tab", "workflows", "esc", "close")
	}
	return m.overBox(screen, m.floatBox(bw, bh, "", title, items, sel, foot), w)
}

func (m chatView) workflowsKey(k string) (tea.Model, tea.Cmd) {
	names := m.flowNames()
	cur := m.flowSel()
	api := m.flows
	switch k {
	case "esc", "q":
		m.overlay = overlayNone
		return m, nil
	case "tab", "shift+tab":
		m.flowSt.sched, m.flowSt.name = !m.flowSt.sched, ""
		if n := m.flowNames(); len(n) > 0 {
			m.flowSt.name = n[0]
		}
		return m, nil
	case "up", "k":
		if cur > 0 {
			m.flowSt.name = names[cur-1]
		}
		return m, nil
	case "down", "j":
		if cur+1 < len(names) {
			m.flowSt.name = names[cur+1]
		}
		return m, nil
	}
	if cur < 0 {
		return m, nil
	}
	name := names[cur]
	if m.flowSt.sched {
		in := m.flowSt.sl.Schedules[cur]
		switch k {
		case "left", "right", "h", "l", "space":
			on := !in.Enabled
			return m, func() tea.Msg {
				_, err := api.SetSchedule(context.Background(), name, on)
				what := "disabled"
				if on {
					what = "enabled"
				}
				return workflowDoneMsg{what: what, name: name, err: err}
			}
		case "enter":
			return m, func() tea.Msg {
				d, err := api.Schedule(context.Background(), name)
				return workflowDetailMsg{sd: &d, err: err}
			}
		}
		return m, nil
	}
	in := m.flowSt.wl.Workflows[cur]
	switch k {
	case "r":
		if len(in.Inputs) > 0 {
			m.overlay = overlayNone
			m.input.SetValue("/run " + name + " " + inputsLine(in.Inputs))
			m.input.CursorEnd()
			m.status("set the inputs, then enter")
			return m, nil
		}
		m.status("running " + name)
		return m, func() tea.Msg {
			return workflowDoneMsg{what: "started", name: name, err: api.RunWorkflow(context.Background(), name, "tui", nil)}
		}
	case "enter":
		return m, func() tea.Msg {
			d, err := api.Workflow(context.Background(), name)
			return workflowDetailMsg{d: d, err: err}
		}
	case "e":
		if _, err := os.Stat(in.File); err != nil {
			m.status("the file is on the daemon's machine: " + in.File)
			return m, nil
		}
		open := m.opener
		if open == nil {
			open = kit.OpenURL
		}
		m.status("opening " + in.File + " (a change turns off its schedules until enabled again)")
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

func inputsLine(in map[string]string) string {
	var parts []string
	for _, k := range sortedNames(in) {
		parts = append(parts, k+"="+in[k])
	}
	return strings.Join(parts, " ")
}

func sortedNames(m map[string]string) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	for i := 1; i < len(k); i++ {
		for j := i; j > 0 && k[j] < k[j-1]; j-- {
			k[j], k[j-1] = k[j-1], k[j]
		}
	}
	return k
}

func (m chatView) workflowDone(msg workflowDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status(msg.name + ": " + msg.err.Error())
		return m, nil
	}
	m.status(msg.name + " " + msg.what)
	if m.overlay == overlayWorkflows {
		return m, m.loadWorkflows()
	}
	return m, nil
}

// workflowDetail shows a workflow's live run, steps and runs, or a schedule's runs; esc goes back.
func (m chatView) workflowDetail(msg workflowDetailMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.status(msg.err.Error())
		return m, nil
	}
	var lines []string
	var title string
	var hist []workflow.Record
	if sd := msg.sd; sd != nil {
		title = sd.Info.Name + " · " + sd.Info.Trigger() + " → " + sd.Info.Workflow
		lines = append(lines, "-> "+sd.Info.File)
		if sd.Hook != "" {
			lines = append(lines, "webhook\tPOST /v1/hooks/schedules/"+sd.Info.Name+" (smithy schedule hook "+sd.Info.Name+")")
		}
		hist = sd.History
	} else {
		d := msg.d
		title = d.Info.Name
		lines = append(lines, "-> "+d.Info.File)
		for _, k := range sortedNames(d.Info.Inputs) {
			lines = append(lines, "input "+k+"\tdefault "+d.Info.Inputs[k])
		}
		if d.Live != nil {
			lines = append(lines, "#RUNNING NOW")
			lines = append(lines, resultLines(d.Live.Steps, "")...)
		}
		lines = append(lines, "#STEPS")
		lines = append(lines, stepLines(d.Info.Steps, "")...)
		hist = d.History
	}
	lines = append(lines, "#RUNS")
	if len(hist) == 0 {
		lines = append(lines, "no runs yet")
	}
	for _, r := range hist {
		trig := r.Trigger
		if r.Schedule != "" {
			trig += ":" + r.Schedule
		}
		head := fmt.Sprintf("%s %s %s\t%s", r.Started.Local().Format("Jan 2 15:04"), r.Status, trig, r.Ended.Sub(r.Started).Round(time.Second))
		if r.Error != "" {
			head += " · " + r.Error
		}
		lines = append(lines, head)
		lines = append(lines, resultLines(r.Steps, "  ")...)
	}
	m.info = infoPanel{title: title, lines: lines, scroll: true}
	m.overlay, m.infoBack = overlayInfo, overlayWorkflows
	return m, nil
}

func stepLines(steps []workflow.Step, indent string) []string {
	var out []string
	for i, s := range steps {
		out = append(out, fmt.Sprintf("%s%d. %s %s\t%s", indent, i+1, s.Kind, s.ID, s.Summary()))
		out = append(out, stepLines(s.Do, indent+"   ")...)
	}
	return out
}

func resultLines(rs []workflow.StepResult, indent string) []string {
	var out []string
	for _, s := range rs {
		out = append(out, fmt.Sprintf("%s%s %s\t%s", indent, s.ID, s.Status, s.Line()))
		out = append(out, resultLines(s.Steps, indent+"  ")...)
	}
	return out
}
