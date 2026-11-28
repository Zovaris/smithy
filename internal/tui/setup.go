//go:build harness

package tui

import (
	"context"
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
	stepEffort
	stepTheme
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
		if msg.first && (!msg.s.Chosen || m.welcome == welcomeSplash) && m.overlay == overlayNone {
			if m.input.Value() == "" {
				m.openWelcome()
				return m, m.loadWelcomeSettings(), true
			}
			m.welcome = welcomeOff
			m.system("/welcome sets smithy up: agents, model, API keys, notifications")
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
		if msg.note != "" && m.welcome != welcomeOff {
			m.welcomeNote = msg.note
		} else if msg.note != "" {
			m.system(msg.note)
		}
		if m.welcome != welcomeOff {
			n, cmd := m.welcomeSaved()
			return n, tea.Batch(cmd, n.loadSetup(false)), true
		}
		return m, m.loadSetup(false), true
	case sessionGoalSetMsg:
		m.sessionGoalSet(msg)
		return m, nil, true
	case sessionModelSetMsg:
		m.sessionModelSet(msg)
		return m, nil, true
	case welcomeSettingsMsg:
		if msg.err != nil {
			m.setupNote = "could not read the settings: " + msg.err.Error()
		} else {
			m.welcomeSet = &msg.s
		}
		return m, nil, true
	case themeMsg:
		return m.themeLoaded(msg), nil, true
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
	m.modelSess = ""
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

func (m chatView) modelCommand(arg string) (tea.Model, tea.Cmd) {
	if _, ok := m.sess.(sessionModelAPI); ok {
		if s := m.openSession(); s != nil && !strings.HasPrefix(s.agent, "api:") {
			if arg != "" {
				return m, m.setSessionModel(s.id, arg, s.effort)
			}
			cmd := m.askModel(s.agent)
			m.modelSess = s.id
			return m, cmd
		}
	}
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
	if m.welcome != welcomeOff {
		if n, cmd, ok := m.welcomeKey(msg); ok {
			return n, cmd
		}
	}
	if m.overlay == overlaySkills {
		return m.skillsKey(msg)
	}
	if m.setupStep == stepModel {
		return m.modelKey(msg)
	}
	if m.setupStep == stepEffort {
		return m.effortKey(msg)
	}
	if m.setupStep == stepTheme {
		return m.themeKey(msg)
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
			m.welcomeAgent = rows[m.setupSel].Name
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
	case "tab":
		if m.modelSess == "" {
			if next := m.nextReadyAgent(m.modelFor); next != "" {
				return m, m.askModel(next)
			}
		}
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
		if m.modelSess != "" {
			eff := ""
			if s := m.sessionByID(m.modelSess); s != nil {
				eff = s.effort
			}
			m.overlay = overlayNone
			return m, m.setSessionModel(m.modelSess, mod, eff)
		}
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
