//go:build harness

package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/models"
)

// setupAPI is the daemon's setup endpoints; *daemon.Client is one.
type setupAPI interface {
	Setup(ctx context.Context) (daemon.Setup, error)
	SetDefault(ctx context.Context, agent, model string) error
	SetKit(ctx context.Context, k daemon.KitChoice) error
	InstallSkills(ctx context.Context, harness string) (string, error)
}

// modelsAPI is the daemon's model list; *daemon.Client is one. A setupAPI without it gets
// the typed field only.
type modelsAPI interface {
	Models(ctx context.Context, agent string, refresh bool) (models.List, error)
}

type modelsLoadedMsg struct {
	agent string
	l     models.List
	err   error
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
	case modelsLoadedMsg:
		if msg.agent != m.modelFor {
			return m, nil, true
		}
		l := msg.l
		if msg.err != nil {
			l.Error = msg.err.Error()
		}
		m.modelList = &l
		m.modelSel = m.currentModelRow()
		return m, nil, true
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

// askModel opens the model list for agent (a CLI, or api:<provider> whose model is required)
// and fetches what the agent reports.
func (m *chatView) askModel(agent string) tea.Cmd {
	m.overlay, m.setupStep, m.modelFor, m.modelIn, m.setupNote, m.modelSel, m.modelList = overlayPicker, stepModel, agent, "", "", 0, nil
	return m.loadModels(false)
}

func (m chatView) loadModels(refresh bool) tea.Cmd {
	api, ok := m.setup.(modelsAPI)
	if !ok {
		return nil
	}
	agent := m.modelFor
	return func() tea.Msg {
		l, err := api.Models(context.Background(), agent, refresh)
		return modelsLoadedMsg{agent: agent, l: l, err: err}
	}
}

// modelRow is one line of the model list: the agent's default (id ""), a reported model, or
// Other (the typed filter as a name).
type modelRow struct {
	m     models.Model
	other bool
}

// modelRows are the rows the filter keeps: the agent default first (a CLI, no filter), the
// reported models whose id or name holds the filter, and Other when the filter names none exactly.
func (m chatView) modelRows() []modelRow {
	f := strings.ToLower(strings.TrimSpace(m.modelIn))
	var rows []modelRow
	if f == "" && !strings.HasPrefix(m.modelFor, "api:") {
		rows = append(rows, modelRow{})
	}
	exact := false
	if m.modelList != nil {
		for _, x := range m.modelList.Models {
			if strings.Contains(strings.ToLower(x.ID), f) || strings.Contains(strings.ToLower(x.Name), f) {
				rows = append(rows, modelRow{m: x})
				exact = exact || strings.EqualFold(x.ID, f)
			}
		}
	}
	if f != "" && !exact {
		rows = append(rows, modelRow{m: models.Model{ID: strings.TrimSpace(m.modelIn)}, other: true})
	}
	return rows
}

// currentModelRow is the row of the agent's saved model, else 0.
func (m chatView) currentModelRow() int {
	if m.setupSt == nil {
		return 0
	}
	d := m.setupSt.Default
	cur := d.Model
	if strings.HasPrefix(m.modelFor, "api:") {
		_, cur, _ = strings.Cut(d.Agent, "/")
	} else if d.Agent != m.modelFor {
		return 0
	}
	for i, r := range m.modelRows() {
		if cur != "" && r.m.ID == cur {
			return i
		}
	}
	return 0
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

// pick acts on agent row a: an API provider, or any agent from the ctrl+l picker, shows its
// models; a ready CLI on first run becomes the default; a missing one shows its fix.
func (m *chatView) pick(a agents.Ready) tea.Cmd {
	switch {
	case !a.Ready:
		m.setupNote = a.Name + " is missing: " + a.Fix
		return nil
	case a.Kind == "api" || m.overlay == overlayPicker:
		return m.askModel(a.Name)
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
			return m, m.askModel("api:" + prov)
		}
		return m, m.saveDefault("api:"+prov+"/"+arg, "")
	}
	if arg == "" {
		return m, m.askModel(cur)
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
		return m.modelKey(msg)
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
			return m, m.askModel(rows[m.setupSel].Name)
		}
	case "enter":
		if m.setupSel < len(rows) {
			return m, m.pick(rows[m.setupSel])
		}
	}
	return m, nil
}

// modelKey handles the model list: type to filter, ↑↓ to move, enter to save, ctrl+r to ask
// the agent again.
func (m chatView) modelKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := m.modelRows()
	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
	case "up":
		m.modelSel = max(m.modelSel-1, 0)
	case "down":
		m.modelSel = min(m.modelSel+1, max(len(rows)-1, 0))
	case "ctrl+r":
		m.modelList = nil
		return m, m.loadModels(true)
	case "backspace":
		if r := []rune(m.modelIn); len(r) > 0 {
			m.modelIn, m.modelSel = string(r[:len(r)-1]), 0
		}
	case "enter":
		if m.modelSel >= len(rows) {
			if strings.HasPrefix(m.modelFor, "api:") {
				m.setupNote = "an API agent needs a model: type its name"
			}
			return m, nil
		}
		mod := rows[m.modelSel].m.ID
		if strings.HasPrefix(m.modelFor, "api:") {
			return m, m.saveDefault(m.modelFor+"/"+mod, "")
		}
		return m, m.saveDefault(m.modelFor, mod)
	default:
		if msg.Text != "" && msg.Text != " " {
			m.modelIn, m.modelSel = m.modelIn+msg.Text, 0
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

// selRow marks the selected row with the accent pointer; the rest keep the same indent.
func (m chatView) selRow(sel bool, s string) string {
	if !sel {
		return "  " + s
	}
	return m.th.fg(m.th.accent).Render(m.th.g.sel) + " " + s
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
		return m.modelPanel(w)
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
		rows = append(rows, m.selRow(i == m.setupSel, t.fg(c).Render(g)+" "+strong.Render(name)+" "+info+cur))
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

// modelPanel is the model list for m.modelFor: the filter, the rows that match it and where
// the list came from.
func (m chatView) modelPanel(w int) []string {
	t := m.th
	dim, strong := t.fg(t.dim), t.fg(t.strong)
	rows := []string{"", spread("  "+strong.Render("Model for "+m.modelFor), dim.Render("esc close  "), w), "",
		"  " + t.fg(t.accent).Render(t.g.prompt) + " " + t.fg(t.text).Render(m.modelIn) + t.fg(t.accent).Render("▏") + dim.Render(map[bool]string{true: "  type to filter", false: ""}[m.modelIn == ""]), ""}
	list := m.modelRows()
	n := m.rows()
	room := max(n-len(rows)-4, 3)
	from := 0
	if m.modelSel >= room {
		from = m.modelSel - room + 1
	}
	for i := from; i < len(list) && i < from+room; i++ {
		r := list[i]
		var line string
		switch {
		case r.other:
			line = strong.Render("Other: ") + t.fg(t.text).Render(r.m.ID) + dim.Render("  use this name as typed")
		case r.m.ID == "":
			line = strong.Render("agent default") + dim.Render("  whatever "+m.modelFor+" starts with")
		default:
			line = t.fg(t.text).Render(fmt.Sprintf("%-36s", r.m.ID))
			if r.m.Name != "" && r.m.Name != r.m.ID {
				line += " " + dim.Render(short(r.m.Name, max(w/4, 12)))
			}
			if r.m.InPerM != nil && r.m.OutPerM != nil {
				line += " " + t.fg(t.muted).Render(fmt.Sprintf("$%.2f/$%.2f per M", *r.m.InPerM, *r.m.OutPerM))
			}
			if r.m.Default {
				line += t.fg(t.accent).Render("  " + t.g.on + " default")
			}
		}
		rows = append(rows, m.selRow(i == m.modelSel, line))
	}
	if m.modelList == nil {
		rows = append(rows, "  "+dim.Render("asking "+m.modelFor+" for its models…"))
	}
	rows = append(rows, "")
	if l := m.modelList; l != nil {
		src := fmt.Sprintf("%d models from %s", len(l.Models), l.Source)
		if l.Error != "" {
			rows = append(rows, "  "+t.fg(t.warn).Render(short(l.Error, w-4)))
		}
		rows = append(rows, "  "+dim.Render(src))
	}
	if m.setupNote != "" {
		rows = append(rows, "  "+t.fg(t.warn).Render(m.setupNote))
	}
	foot := dim.Render("  type filter   ↑↓ move   enter save   ctrl+r ask again   esc cancel")
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
	rows = append(rows, m.selRow(m.setupSel == 0, box(s.Kit.MCP)+" "+strong.Render(fmt.Sprintf("%-20s", "smithy MCP server"))+dim.Render("board, cards and decisions as tools")))
	for i, sk := range s.Skills {
		rows = append(rows, m.selRow(m.setupSel == i+1, box(slices.Contains(s.Kit.Skills, sk))+" "+strong.Render(fmt.Sprintf("%-20s", sk))+dim.Render("skill")))
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
