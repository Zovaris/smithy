//go:build harness

package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/daemon"
)

// setupAPI is the daemon's setup endpoints; *daemon.Client is one.
type setupAPI interface {
	Setup(ctx context.Context) (daemon.Setup, error)
	SetDefault(ctx context.Context, agent, model string) error
	SetKit(ctx context.Context, k daemon.KitChoice) error
	InstallSkills(ctx context.Context, harness string) (string, error)
}

type setupStep int

const (
	stepAgent setupStep = iota
	stepModel
)

type setupLoadedMsg struct {
	s     daemon.Setup
	first bool
	err   error
}

type setupSavedMsg struct {
	note string
	err  error
}

type skillsInstalledMsg struct {
	out string
	err error
}

// loadSetup fetches GET /v1/setup; first opens the first-run overlay when no agent was ever chosen.
func (m chatView) loadSetup(first bool) tea.Cmd {
	api := m.setup
	if api == nil {
		return nil
	}
	return func() tea.Msg {
		s, err := api.Setup(context.Background())
		return setupLoadedMsg{s: s, first: first, err: err}
	}
}

func (m chatView) saveDefault(agent, model string) tea.Cmd {
	api := m.setup
	if api == nil {
		return nil
	}
	note := "new sessions start with " + agent
	if model != "" {
		note += " " + m.th.g.dot + " model " + model
	}
	return func() tea.Msg {
		return setupSavedMsg{note: note, err: api.SetDefault(context.Background(), agent, model)}
	}
}

func (m chatView) saveKit(k daemon.KitChoice) tea.Cmd {
	api := m.setup
	if api == nil {
		return nil
	}
	return func() tea.Msg { return setupSavedMsg{err: api.SetKit(context.Background(), k)} }
}

func (m chatView) installSkills() tea.Cmd {
	api := m.setup
	if api == nil {
		return nil
	}
	return func() tea.Msg {
		out, err := api.InstallSkills(context.Background(), "")
		return skillsInstalledMsg{out: out, err: err}
	}
}

// setupUpdate handles the setup messages; ok is false for any other message.
func (m chatView) setupUpdate(msg tea.Msg) (chatView, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case setupLoadedMsg:
		if msg.err != nil {
			m.setupNote = "could not read the agents: " + msg.err.Error()
			if m.overlay == overlayNone {
				m.system(m.setupNote)
			}
			return m, nil, true
		}
		m.setupSt = &msg.s
		if msg.first && !msg.s.Chosen && m.overlay == overlayNone {
			m.openPicker(overlaySetup)
		}
		return m, nil, true
	case setupSavedMsg:
		if msg.err != nil {
			m.setupNote = msg.err.Error()
			if m.overlay == overlayNone {
				m.system("error: " + msg.err.Error())
			}
			return m, nil, true
		}
		if m.overlay == overlaySetup || m.overlay == overlayPicker {
			m.overlay = overlayNone
		}
		if msg.note != "" {
			m.system(msg.note)
		}
		return m, m.loadSetup(false), true
	case skillsInstalledMsg:
		out := strings.TrimSpace(msg.out)
		if msg.err != nil {
			out = strings.TrimSpace("install failed: " + msg.err.Error() + "\n" + out)
		}
		m.setupNote = lastLines(out, 3)
		return m, m.loadSetup(false), true
	}
	return m, nil, false
}

func lastLines(s string, n int) string {
	l := strings.Split(s, "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

// openPicker opens the agent list (first run or ctrl+l) with the current default selected.
func (m *chatView) openPicker(o chatOverlay) {
	m.overlay, m.setupStep, m.setupNote, m.modelIn, m.setupSel = o, stepAgent, "", "", 0
	if m.setupSt == nil {
		return
	}
	cur := m.setupSt.Default.Agent
	for i, a := range m.setupSt.Agents {
		if a.Name == cur || strings.HasPrefix(cur, a.Name+"/") {
			m.setupSel = i
		}
	}
}

// askModel opens the model step for agent (a CLI, or api:<provider> whose model is required).
func (m *chatView) askModel(agent string) {
	m.overlay, m.setupStep, m.modelFor, m.modelIn, m.setupNote = overlayPicker, stepModel, agent, "", ""
	if m.setupSt != nil && m.setupSt.Default.Agent == agent {
		m.modelIn = m.setupSt.Default.Model
	}
}

func (m chatView) agentLabel() string {
	if m.setupSt == nil {
		return ""
	}
	d := m.setupSt.Default
	if d.Model != "" {
		return d.Agent + " " + m.th.g.dot + " " + d.Model
	}
	return d.Agent
}

func (m chatView) readyRow(name string) (agents.Ready, bool) {
	if m.setupSt == nil {
		return agents.Ready{}, false
	}
	for _, a := range m.setupSt.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return agents.Ready{}, false
}

// pick acts on agent row a: a ready CLI becomes the default, an API provider asks for its model,
// a missing one shows its fix.
func (m *chatView) pick(a agents.Ready) tea.Cmd {
	switch {
	case !a.Ready:
		m.setupNote = a.Name + " is missing: " + a.Fix
		return nil
	case a.Kind == "api":
		m.askModel(a.Name)
		return nil
	}
	return m.saveDefault(a.Name, "")
}

// agentCommand is /agent [name]: no name opens the picker.
func (m chatView) agentCommand(arg string) (tea.Model, tea.Cmd) {
	if arg == "" {
		m.openPicker(overlayPicker)
		return m, m.loadSetup(false)
	}
	if a, ok := m.readyRow(arg); ok {
		cmd := m.pick(a)
		if m.setupNote != "" {
			m.system(m.setupNote)
			m.setupNote = ""
		}
		return m, cmd
	}
	if p, ok := strings.CutPrefix(arg, "api:"); ok && strings.Contains(p, "/") {
		return m, m.saveDefault(arg, "")
	}
	m.system("unknown agent " + arg + "; /agent lists the ones on this machine")
	return m, nil
}

// modelCommand is /model [name] for the current agent: no name asks for it.
func (m chatView) modelCommand(arg string) (tea.Model, tea.Cmd) {
	if m.setupSt == nil || m.setupSt.Default.Agent == "" {
		m.system("pick an agent first: /agent")
		return m, nil
	}
	cur := m.setupSt.Default.Agent
	if p, ok := strings.CutPrefix(cur, "api:"); ok {
		prov, _, _ := strings.Cut(p, "/")
		if arg == "" {
			m.askModel("api:" + prov)
			return m, nil
		}
		return m, m.saveDefault("api:"+prov+"/"+arg, "")
	}
	if arg == "" {
		m.askModel(cur)
		return m, nil
	}
	return m, m.saveDefault(cur, arg)
}

// setupKey handles keys while the first-run, picker or skills overlay is open.
func (m chatView) setupKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if m.overlay == overlaySkills {
		return m.skillsKey(msg)
	}
	if m.setupStep == stepModel {
		switch k {
		case "esc":
			m.overlay = overlayNone
		case "backspace":
			if r := []rune(m.modelIn); len(r) > 0 {
				m.modelIn = string(r[:len(r)-1])
			}
		case "enter":
			mod := strings.TrimSpace(m.modelIn)
			if strings.HasPrefix(m.modelFor, "api:") {
				if mod == "" {
					m.setupNote = "an API agent needs a model"
					return m, nil
				}
				return m, m.saveDefault(m.modelFor+"/"+mod, "")
			}
			return m, m.saveDefault(m.modelFor, mod)
		default:
			if msg.Text != "" && msg.Text != " " {
				m.modelIn += msg.Text
			}
		}
		return m, nil
	}
	var rows []agents.Ready
	if m.setupSt != nil {
		rows = m.setupSt.Agents
	}
	switch k {
	case "esc", "ctrl+l":
		m.overlay = overlayNone
	case "up", "k":
		m.setupSel = max(m.setupSel-1, 0)
		m.setupNote = ""
	case "down", "j":
		m.setupSel = min(m.setupSel+1, max(len(rows)-1, 0))
		m.setupNote = ""
	case "m":
		if m.setupSel < len(rows) && rows[m.setupSel].Ready {
			m.askModel(rows[m.setupSel].Name)
		}
	case "enter":
		if m.setupSel < len(rows) {
			return m, m.pick(rows[m.setupSel])
		}
	}
	return m, nil
}

func (m chatView) skillsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.setupSt == nil {
		if msg.String() == "esc" {
			m.overlay = overlayNone
		}
		return m, nil
	}
	n := 1 + len(m.setupSt.Skills)
	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
	case "up", "k":
		m.setupSel = max(m.setupSel-1, 0)
	case "down", "j":
		m.setupSel = min(m.setupSel+1, n-1)
	case "space", " ", "enter":
		k := m.setupSt.Kit
		k.Skills = slices.Clone(k.Skills)
		if m.setupSel == 0 {
			k.MCP = !k.MCP
		} else {
			s := m.setupSt.Skills[m.setupSel-1]
			if i := slices.Index(k.Skills, s); i >= 0 {
				k.Skills = slices.Delete(k.Skills, i, i+1)
			} else {
				k.Skills = append(k.Skills, s)
			}
		}
		m.setupSt.Kit = k
		return m, m.saveKit(k)
	case "i":
		m.setupNote = "installing smithy skills into every detected harness…"
		return m, m.installSkills()
	}
	return m, nil
}

func (m chatView) selRow(w int, sel bool, s string) string {
	t := m.th
	if !sel {
		return "  " + s
	}
	st := lipgloss.NewStyle()
	if t.border != nil {
		st = st.Background(t.border)
	}
	return st.Width(w).MaxWidth(w).Render(t.fg(t.accent).Render(t.g.sel) + " " + s)
}

// setupPanel is the first-run screen and the ctrl+l / /agent / /model picker.
func (m chatView) setupPanel(w int) []string {
	t := m.th
	dim, strong := t.fg(t.dim), t.fg(t.strong)
	var rows []string
	if m.overlay == overlaySetup {
		rows = append(rows, "", "  "+strong.Render("Welcome to smithy"), "  "+dim.Render("These are the agents on this machine. Pick the one new sessions start with; ctrl+l changes it later."), "")
	} else {
		rows = append(rows, "", spread("  "+strong.Render("Pick an agent"), dim.Render("esc close  "), w), "")
	}
	if m.setupSt == nil {
		rows = append(rows, "  "+dim.Render("reading the agents…"))
		if m.setupNote != "" {
			rows = append(rows, "  "+t.fg(t.err).Render(m.setupNote))
		}
		return m.panel(w, rows)
	}
	if m.setupStep == stepModel {
		rows = append(rows, "  "+strong.Render("Model for "+m.modelFor),
			"  "+t.fg(t.accent).Render(t.g.prompt)+" "+t.fg(t.text).Render(m.modelIn)+t.fg(t.accent).Render("▏"))
		hint := "empty: the agent's own default"
		if strings.HasPrefix(m.modelFor, "api:") {
			hint = "required, e.g. claude-sonnet-5-5"
		}
		rows = append(rows, "  "+dim.Render(hint), "")
		if m.setupNote != "" {
			rows = append(rows, "  "+t.fg(t.warn).Render(m.setupNote))
		}
		return m.panel(w, append(rows, "", dim.Render("  enter save   esc cancel")))
	}
	kind := ""
	for i, a := range m.setupSt.Agents {
		if a.Kind != kind {
			kind = a.Kind
			label := "AGENT CLIS"
			if kind == "api" {
				label = "API KEYS"
			}
			if i > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, "  "+t.fg(t.muted).Render(label))
		}
		g, c, state := t.g.pass, t.ok, "ready"
		if !a.Ready {
			g, c, state = t.g.fail, t.err, "missing"
		}
		name := fmt.Sprintf("%-16s", a.Name)
		cur := ""
		if d := m.setupSt.Default.Agent; m.setupSt.Chosen && (d == a.Name || strings.HasPrefix(d, a.Name+"/")) {
			cur = t.fg(t.accent).Render("  " + t.g.on + " default")
		}
		info := dim.Render(fmt.Sprintf("%-8s", state)) + " " + dim.Render(short(a.Detail, max(w/3, 20)))
		if !a.Ready && a.Fix != "" {
			info = dim.Render(fmt.Sprintf("%-8s", state)) + " " + t.fg(t.text).Render(a.Fix)
		}
		rows = append(rows, m.selRow(w, i == m.setupSel, t.fg(c).Render(g)+" "+strong.Render(name)+" "+info+cur))
	}
	rows = append(rows, "")
	if m.setupNote != "" {
		rows = append(rows, "  "+t.fg(t.warn).Render(m.setupNote), "")
	}
	foot := dim.Render("  ↑↓ move   enter pick   m model   esc " + map[bool]string{true: "later", false: "close"}[m.overlay == overlaySetup])
	n := m.rows()
	if len(rows) >= n {
		rows = rows[:n-1]
	}
	for len(rows) < n-1 {
		rows = append(rows, "")
	}
	return m.panel(w, append(rows, foot))
}

// skillsPanel is /skills: what goes into every session (smithy's MCP server, smithy skills) and
// what each harness has installed.
func (m chatView) skillsPanel(w int) []string {
	t := m.th
	dim, strong := t.fg(t.dim), t.fg(t.strong)
	rows := []string{"", spread("  "+strong.Render("Skills and MCP"), dim.Render("esc close  "), w), ""}
	if m.setupSt == nil {
		return m.panel(w, append(rows, "  "+dim.Render("reading…")))
	}
	s := m.setupSt
	box := func(on bool) string {
		if on {
			return t.fg(t.ok).Render("[x]")
		}
		return dim.Render("[ ]")
	}
	rows = append(rows, "  "+t.fg(t.muted).Render("INJECTED INTO EVERY SESSION"))
	rows = append(rows, m.selRow(w, m.setupSel == 0, box(s.Kit.MCP)+" "+strong.Render(fmt.Sprintf("%-20s", "smithy MCP server"))+dim.Render("board, cards and decisions as tools")))
	for i, sk := range s.Skills {
		rows = append(rows, m.selRow(w, m.setupSel == i+1, box(slices.Contains(s.Kit.Skills, sk))+" "+strong.Render(fmt.Sprintf("%-20s", sk))+dim.Render("skill")))
	}
	if len(s.Skills) == 0 {
		rows = append(rows, "  "+dim.Render("no smithy skills installed: press i"))
	}
	rows = append(rows, "", "  "+t.fg(t.muted).Render("INSTALLED PER HARNESS"))
	for _, h := range s.Harnesses {
		name := strong.Render(fmt.Sprintf("%-10s", h.Harness))
		if !h.Present {
			rows = append(rows, "  "+dim.Render(t.g.dot)+" "+name+" "+dim.Render("not installed"))
			continue
		}
		g, c := t.g.pass, t.ok
		if len(h.Missing) > 0 {
			g, c = t.g.wait, t.warn
		}
		mcp := "no MCP servers"
		if len(h.MCP) > 0 {
			mcp = "MCP " + strings.Join(h.MCP, ", ")
		}
		if h.MCPError != "" {
			mcp = h.MCPError
		}
		other := ""
		if len(h.Other) > 0 {
			other = fmt.Sprintf(" %s %d other skills", t.g.dot, len(h.Other))
		}
		line := t.fg(c).Render(g) + " " + name + " " + t.fg(t.text).Render(fmt.Sprintf("smithy %d/%d", len(h.Smithy), len(h.Smithy)+len(h.Missing))) + dim.Render(other+" "+t.g.dot+" "+mcp)
		rows = append(rows, "  "+line)
	}
	rows = append(rows, "")
	if m.setupNote != "" {
		for _, l := range strings.Split(m.setupNote, "\n") {
			rows = append(rows, "  "+t.fg(t.info).Render(l))
		}
	}
	foot := dim.Render("  ↑↓ move   space toggle   i install/update smithy skills   esc close")
	n := m.rows()
	if len(rows) >= n {
		rows = rows[:n-1]
	}
	for len(rows) < n-1 {
		rows = append(rows, "")
	}
	return m.panel(w, append(rows, foot))
}
