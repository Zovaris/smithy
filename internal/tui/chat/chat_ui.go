//go:build harness

package chat

import (
	"crypto/rand"
	hexenc "encoding/hex"
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/layout"
	"github.com/Zovaris/smithy/internal/tui/kit"
	"github.com/charmbracelet/x/ansi"
)

// chatMode is how much of a turn the transcript shows; v cycles it.
type chatMode int

const (
	modeMinimal    chatMode = iota // a run of tools is one summary line
	modeExpandable                 // one header per tool, opened by a click
	modeVerbose                    // every tool open, plus thinking and turn stats
)

func (c chatMode) String() string { return [...]string{"minimal", "expandable", "verbose"}[c] }

type chatOverlay int

const (
	overlayNone chatOverlay = iota
	overlayAgents
	overlayPalette
	overlaySetup   // first run: every agent ready or missing
	overlayPicker  // ctrl+l, /agent, /model
	overlaySkills  // /skills
	overlayHelp    // ? and /help
	overlayInfo    // /connect, /remote, /web, /log
	overlayProject // which project a new session starts in
	overlayWelcome
	overlayAsk       // the question card for the first decision waiting on you
	overlayBoard     // /board: the task files by status
	overlayWorkflows // /workflows and /schedules
	overlayConfig    // /config: every setting, grouped and searchable
	overlayUsage     // /usage: tokens and spend
)

// chatTool is the data of a "tool" event: one tool call of a session's turn.
type chatTool struct {
	Name   string   `json:"name"`
	Arg    string   `json:"arg,omitempty"`
	Stat   string   `json:"stat,omitempty"`
	Took   string   `json:"took,omitempty"`
	Status string   `json:"status,omitempty"` // ok, failed or running
	Lines  []string `json:"lines,omitempty"`
}

// chatSession is a session seen in the event stream and the state its last event left it in.
type chatSession struct {
	id, kind, state          string
	title, agent, model, now string
	effort                   string
	goal, goalState          string
	dir                      string // the folder it started in; "" for batch events
	parent                   string
	prs                      []string
	links                    *api.SessionLinks
	since, turn              time.Time
}

// chatDecidedMsg is the result of posting an answer to /v1/decisions.
type chatDecidedMsg struct {
	id  string
	err error
}

type palette struct {
	query string
	sel   int
}

type palItem struct {
	group, glyph     string
	color            color.Color
	label, hint, key string
	run              func(m *chatView) tea.Cmd
	cycle            func(m *chatView, dir int) tea.Cmd // a setting row: left/right step its value
}

// event routes one daemon event: session events to their conversation, decisions to the
// question card, a turn's tools and thinking to the transcript, everything else to the status line.
func (m *chatView) event(kind, session string, at time.Time, data json.RawMessage) {
	if strings.HasPrefix(kind, "session_") && session != "" {
		m.evAt = at
		if m.sessionEvent(kind, session, data) {
			return
		}
	}
	if session != "" {
		m.track(session, kind)
	}
	switch kind {
	case "tool":
		var t chatTool
		if json.Unmarshal(data, &t) == nil && t.Name != "" {
			m.push(chatMsg{who: "daemon", tool: &t})
		}
		return
	case "thinking", "turn":
		var d struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(data, &d) == nil && d.Text != "" {
			m.push(chatMsg{who: "daemon", kind: kind, text: kit.Clean(d.Text)})
		}
		return
	case daemon.KindAsked:
		var d api.Decision
		if json.Unmarshal(data, &d) == nil && d.ID != "" {
			m.setPending(append(m.pending, d))
			m.offerAsk()
		}
	case daemon.KindAnswered:
		var o api.Outcome
		if json.Unmarshal(data, &o) == nil {
			m.keepQA(o.ID, o.Answer)
			m.unpend(o.ID)
		}
	case daemon.KindWorkflowNote:
		var n struct{ Workflow, Title, Text string }
		if json.Unmarshal(data, &n) == nil && n.Workflow != "" {
			m.system(kit.Clean("workflow " + n.Workflow + " · " + n.Title + ": " + n.Text))
		}
		return
	}
	if t := eventText(kind, session, data); t != "" {
		m.logNoise(noise{at: at, text: t, quiet: kind == daemon.KindAsked})
	}
}

func (m *chatView) track(id, kind string) {
	state := "running"
	switch kind {
	case "failed", "error":
		state = "failed"
	case "done", "finished", "merged", "turn", "stopped":
		state = "done"
	case daemon.KindAsked, daemon.StatusNeedsYou:
		state = "waiting"
	}
	for i := range m.sessions {
		if s := &m.sessions[i]; s.id == id {
			if state == "running" && s.state != "running" {
				s.turn = time.Now()
				delete(m.streamed, id)
			}
			if state == "done" && s.state == "running" && !s.turn.IsZero() && m.answered(id) {
				agent, model := s.agent, s.model
				if agent == "" && m.setupSt != nil {
					agent, model = m.setupSt.Default.Agent, m.setupSt.Default.Model
				}
				foot := turnFooter(agent, model, time.Since(s.turn).Round(100*time.Millisecond), m.tokRate(id), " "+m.th.G.Dot+" ")
				prev := m.convs[id]
				if id == m.current {
					prev = m.msgs
				}
				for j := len(prev) - 1; j >= 0; j-- {
					if prev[j].who == "you" {
						prev[j].lines = nil
						break
					}
				}
				m.into(id, chatMsg{who: "daemon", kind: "foot", text: foot})
			}
			s.kind, s.state = kind, state
			return
		}
	}
	m.sessions = append(m.sessions, chatSession{id: id, kind: kind, state: state, since: time.Now(), turn: time.Now()})
}

// setPending replaces the decisions waiting on the user, keeping one per id in arrival order.
func (m *chatView) setPending(ds []api.Decision) {
	var out []api.Decision
	seen := map[string]bool{}
	for _, d := range ds {
		if d.ID != "" && d.Outcome == nil && !seen[d.ID] {
			seen[d.ID] = true
			out = append(out, d)
		}
	}
	m.pending = out
}

func (m *chatView) unpend(id string) {
	var out []api.Decision
	for _, d := range m.pending {
		if d.ID != id {
			out = append(out, d)
		}
	}
	m.pending = out
	if m.overlay == overlayAsk && id == m.card.id {
		m.overlay = overlayNone
		m.offerAsk()
	}
}

func (m *chatView) decided(d chatDecidedMsg) {
	if d.err != nil {
		m.system(m.th.G.Fail + " answer failed: " + d.err.Error())
		return
	}
	m.keepQA(d.id, m.sent[d.id])
	m.unpend(d.id)
}

func (m *chatView) system(text string) {
	m.offset = 0
	if n := len(m.msgs); n > 0 && m.msgs[n-1].kind == "system" && m.msgs[n-1].text == text {
		return
	}
	m.push(chatMsg{who: "daemon", kind: "system", text: text})
}

// answer posts an answer to the first waiting decision.
func (m *chatView) answer(a string) tea.Cmd {
	if len(m.pending) == 0 {
		m.system("nothing waits on you")
		return nil
	}
	if m.decide == nil {
		return nil
	}
	if m.sent == nil {
		m.sent = map[string]string{}
	}
	m.sent[m.pending[0].ID] = a
	return m.decide(m.pending[0].ID, a)
}

func (m *chatView) setMode(c chatMode) {
	m.mode = c
	m.ver++
	*m.at = anchor{}
	m.scroll(0)
}

func (m *chatView) setFocus(i int) {
	if m.focus >= 0 && m.focus < len(m.msgs) {
		m.msgs[m.focus].lines = nil
	}
	m.focus = i
	if i >= 0 {
		m.msgs[i].lines = nil
		m.input.Blur()
	} else {
		m.input.Focus()
	}
	*m.at = anchor{}
}

// runOf is the first and last message of the run of tool messages around i; in minimal the
// thoughts between them join the run, so it collapses to one activity line.
func (m chatView) runOf(i int) (a, b int) {
	a, b = i, i
	for a > 0 && m.inRun(m.msgs[a-1]) {
		a--
	}
	for b < len(m.msgs)-1 && m.inRun(m.msgs[b+1]) {
		b++
	}
	return a, b
}

func (m chatView) inRun(c *chatMsg) bool {
	return c.tool != nil || m.mode == modeMinimal && c.kind == "thinking"
}

// toggle opens or closes tool message i: the whole run in minimal, one block in expandable.
func (m chatView) toggle(i int) chatView {
	if i >= 0 && i < len(m.msgs) && m.msgs[i].sh != nil {
		m.msgs[i].open = !m.msgs[i].open
		m.msgs[i].lines = nil
		*m.at = anchor{}
		m.scroll(0)
		return m
	}
	if i < 0 || i >= len(m.msgs) || !m.inRun(m.msgs[i]) || m.mode == modeVerbose {
		return m
	}
	if m.mode == modeMinimal {
		a, b := m.runOf(i)
		m.msgs[a].open = !m.msgs[a].open
		for j := a; j <= b; j++ {
			m.msgs[j].lines = nil
		}
	} else {
		m.msgs[i].open = !m.msgs[i].open
		m.msgs[i].lines = nil
	}
	*m.at = anchor{}
	m.scroll(0)
	return m
}

// shortcut handles the chat's chord keys. A printable key always goes to the input, so typing
// never loses a letter; a lone ? or option letter acts on enter instead.
func (m *chatView) shortcut(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "ctrl+o":
		m.setMode((m.mode + 1) % 3)
		return true, nil
	case "tab":
		m.overlay, m.agentSel = overlayAgents, max(m.listedAt(m.current), 0)
		return true, nil
	case "1", "2", "3", "4", "5":
		if r := m.recent(); m.fresh() && m.input.Value() == "" && m.overlay == overlayNone && int(msg.String()[0]-'1') < len(r) {
			m.open(r[msg.String()[0]-'1'].id)
			return true, nil
		}
	case "ctrl+n", "ctrl+p":
		m.switchTab(map[string]int{"ctrl+n": 1, "ctrl+p": -1}[msg.String()])
		return true, nil
	case "ctrl+s":
		m.selecting = !m.selecting
		if m.selecting {
			m.status("select mode: drag to select, your terminal copies; ctrl+s gives the mouse back")
		}
		return true, nil
	case "ctrl+k":
		return true, m.openPalette("")
	case "ctrl+l":
		m.openPicker(overlayPicker)
		return true, m.loadSetup(false)
	}
	return false, nil
}

// command runs a / command typed in the input.
func (m chatView) command(line string) (tea.Model, tea.Cmd) {
	name, arg, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "mode":
		next := (m.mode + 1) % 3
		for c := modeMinimal; c <= modeVerbose; c++ {
			if arg == c.String() {
				next = c
			}
		}
		m.setMode(next)
	case "sidebar":
		m.sideOff = !m.sideOff
		m.ver++
		*m.at = anchor{}
	case "agents", "sessions":
		m.overlay, m.agentSel = overlayAgents, max(m.listedAt(m.current), 0)
	case "new":
		m.newSession()
		m.status("new session: your next message starts it with " + m.agentLabel())
	case "stop":
		return m.stopSession()
	case "web":
		return m.openWeb()
	case "connect":
		return m, m.showConnect()
	case "remote":
		m.showRemote()
	case "log":
		m.showLog()
	case "palette":
		return m, m.openPalette("")
	case "agent":
		return m.agentCommand(arg)
	case "model":
		return m.modelCommand(arg)
	case "effort":
		return m.effortCommand(arg)
	case "goal":
		return m.goalCommand(arg)
	case "theme":
		return m.themeCommand(arg)
	case "skills":
		m.overlay, m.setupSel, m.setupNote = overlaySkills, 0, ""
		return m, m.loadSetup(false)
	case "welcome":
		m.openWelcome()
		return m, tea.Batch(m.loadSetup(false), m.loadWelcomeSettings())
	case "setup":
		m.openPicker(overlaySetup)
		return m, m.loadSetup(false)
	case "ask":
		return m, m.askDemo(arg)
	case "answer":
		if arg == "" {
			m.openAsk()
			return m, nil
		}
		return m, m.answer(arg)
	case "detach":
		return m.detach()
	case "board", "tasks":
		return m.openBoard(arg)
	case "links":
		return m.linksCommand(arg)
	case "workflows", "workflow":
		return m.openWorkflows(false)
	case "schedules", "schedule":
		return m.openWorkflows(true)
	case "run":
		return m.runWorkflowCmd(arg)
	case "serve", "set", "unset", "key", "keys", "doctor", "tend", "prompt", "channels":
		return m.settingsCommand(name, arg)
	case "usage":
		return m.openUsage()
	case "config", "settings":
		return m.openConfig(arg)
	case "quit":
		return m.quit()
	case "help":
		m.overlay, m.pal = overlayHelp, palette{}
	default:
		if p := m.skillPrompt(name, arg); p != "" {
			return m.send(p)
		}
		m.system("unknown command /" + name)
	}
	return m, nil
}

// skillPrompt is what /name or $name sends for an installed skill: read its SKILL.md and follow it
// on the rest of the line; "" when name is no installed skill.
func (m chatView) skillPrompt(name, arg string) string {
	if m.setupSt == nil {
		return ""
	}
	p := m.setupSt.Paths[name]
	if p == "" {
		return ""
	}
	out := "Use the " + name + " skill: read " + p + " and follow it."
	if arg != "" {
		out += "\n\n" + arg
	}
	return out
}

// skillDir is where a skill lives, for its hint: the folder holding it, with ~ for home.
func skillDir(path string) string {
	d := filepath.Dir(filepath.Dir(path))
	if h := layout.UserHome(); h != "" {
		if r, ok := strings.CutPrefix(d, h+string(filepath.Separator)); ok {
			return "~/" + r
		}
	}
	return d
}

// click focuses what is under the pointer: a transcript row
// focuses its message (and opens or closes a tool block), the input row focuses the input.
func (m chatView) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	const head = headRows
	inputY := m.height - 2 - m.input.Height()
	switch {
	case m.overlay != overlayNone:
		return m, nil
	case msg.Y == head+m.rows() && m.refined() != "" && !m.working():
		_, hits := m.refineRow(max(m.width, 20))
		for _, h := range hits {
			if msg.X >= h.x0 && msg.X < h.x1 {
				return m.refineAct(h.option)
			}
		}
	case msg.Y >= inputY && msg.Y < m.height-1:
		m.setFocus(-1)
	case msg.Y == 1:
		_, hits, x := m.tabStrip(max(m.width, 20))
		if msg.X >= x {
			m.newSession()
			m.status("new session: your next message starts it with " + m.agentLabel())
			return m, nil
		}
		for _, h := range hits {
			if msg.X >= h.x0 && msg.X < h.x1 && h.x0 >= 0 {
				m.open(h.id)
			}
		}
	case m.sideOn() && msg.X >= m.sideX() && msg.Y >= head && msg.Y < head+m.rows():
		_, urls := m.sideRows()
		if r := msg.Y - head - 1; r >= 0 && r < len(urls) && urls[r] != "" {
			if msg.X >= m.sideX()+m.sideW()-4 {
				return m.removeLink(urls[r])
			}
			return m, m.openLink(urls[r])
		}
	case msg.Y >= head && msg.Y < head+m.rows():
		vis, idx := m.visible()
		j := msg.Y - head - m.lead(len(vis))
		if j < 0 || j >= len(idx) {
			return m, nil
		}
		i := idx[j]
		m.setFocus(i)
		m = m.toggle(i)
	}
	return m, nil
}

func (m chatView) overlayKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m.quit()
	}
	if k == "/" && (m.overlay == overlayWorkflows || m.overlay == overlayBoard || m.overlay == overlayAgents) {
		m.overlay, m.infoBack = overlayNone, overlayNone
		m.input.SetValue("/")
		m.input.CursorEnd()
		m.setFocus(-1)
		return m, nil
	}
	switch m.overlay {
	case overlaySetup, overlayPicker, overlaySkills, overlayWelcome:
		return m.setupKey(msg)
	case overlayAsk:
		return m.askKey(msg)
	case overlayConfig:
		return m.configKey(msg)
	case overlayUsage:
		return m.usageKey(msg)
	case overlayAgents:
		return m.agentsKey(k)
	case overlayProject:
		return m.projectKey(k)
	case overlayBoard:
		return m.boardKey(k)
	case overlayWorkflows:
		return m.workflowsKey(k)
	case overlayHelp, overlayInfo:
		if m.overlay == overlayInfo && m.info.urls != nil {
			if n, cmd, ok := m.linksKey(k); ok {
				return n, cmd
			}
		}
		if p := m.info.set; p != nil && m.overlay == overlayInfo {
			switch k {
			case "left", "h":
				p.sel = (p.sel + len(p.opts) - 1) % len(p.opts)
				return m, nil
			case "right", "l", "space":
				p.sel = (p.sel + 1) % len(p.opts)
				return m, nil
			case "enter":
				m.overlay = overlayNone
				return m, m.saveSetting(*p)
			}
		}
		switch k {
		case "esc", "q", "?":
			if k == "q" && m.overlay == overlayHelp {
				break
			}
			m.overlay, m.infoBack = overlayNone, overlayNone
			return m, nil
		case "up":
			m.pal.sel = max(m.pal.sel-1, 0)
		case "down":
			m.pal.sel++
		case "backspace":
			if m.overlay == overlayInfo && m.infoBack != overlayNone {
				m.overlay, m.infoBack = m.infoBack, overlayNone
				return m, nil
			}
			if r := []rune(m.pal.query); len(r) > 0 {
				m.pal = palette{query: string(r[:len(r)-1])}
			}
		default:
			if m.overlay == overlayHelp && msg.Text != "" {
				m.pal = palette{query: m.pal.query + msg.Text}
			}
		}
		return m, nil
	}
	items := m.palItems()
	switch k {
	case "esc", "ctrl+k":
		m.overlay, m.picker = overlayNone, nil
	case "up":
		m.pal.sel = max(m.pal.sel-1, 0)
	case "down":
		m.pal.sel = min(m.pal.sel+1, max(len(items)-1, 0))
	case "left", "right":
		if m.pal.sel < len(items) && items[m.pal.sel].cycle != nil {
			return m, items[m.pal.sel].cycle(&m, map[string]int{"left": -1, "right": 1}[k])
		}
	case "tab":
		for i := m.pal.sel + 1; i < len(items); i++ {
			if items[i].group != items[m.pal.sel].group {
				m.pal.sel = i
				return m, nil
			}
		}
		m.pal.sel = 0
	case "backspace":
		if r := []rune(m.pal.query); len(r) > 0 {
			m.pal = palette{query: string(r[:len(r)-1])}
		}
	case "enter":
		m.overlay = overlayNone
		if m.pal.sel < len(items) {
			c := items[m.pal.sel].run(&m)
			if m.overlay == overlayNone {
				m.picker = nil
			}
			return m, c
		}
	default:
		if msg.Text != "" {
			m.pal = palette{query: m.pal.query + msg.Text}
		}
	}
	return m, nil
}

// agentsKey is the agents panel: move, open a session, stop it, start a new one.
func (m chatView) agentsKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "tab", "esc":
		m.overlay = overlayNone
	case "ctrl+k":
		return m, m.openPalette("")
	case "up", "k":
		m.agentSel = max(m.agentSel-1, 0)
	case "down", "j":
		m.agentSel = min(m.agentSel+1, max(len(m.listed())-1, 0))
	case "a":
		m.allProj = !m.allProj
		m.agentSel = min(m.agentSel, max(len(m.listed())-1, 0))
	case "enter":
		if l := m.listed(); m.agentSel < len(l) {
			m.open(l[m.agentSel].id)
		}
		m.overlay = overlayNone
	case "n":
		m.newSession()
		m.overlay = overlayNone
	case "s":
		if l := m.listed(); m.agentSel < len(l) && m.sess != nil {
			api, id := m.sess, l[m.agentSel].id
			return m, func() tea.Msg {
				ctx, cancel := daemonCtx()
				defer cancel()
				if err := api.StopSession(ctx, id); err != nil {
					return sessionErrMsg{what: "stop", err: err}
				}
				return nil
			}
		}
	}
	return m, nil
}

// palItems are the palette's rows after the query filter, in group order.
func (m chatView) palItems() []palItem {
	t := m.th
	if m.picker != nil {
		return m.connectItems()
	}
	var all []palItem
	for i, d := range m.pending {
		all = append(all, palItem{group: "WAITING ON YOU", glyph: t.G.Wait, color: t.Warn, label: d.Question, hint: strings.Join(d.Options, " "+t.G.Dot+" "),
			run: func(m *chatView) tea.Cmd {
				m.pending[0], m.pending[i] = m.pending[i], m.pending[0]
				return nil
			}})
	}
	cycle := func(m *chatView) tea.Cmd { m.setMode((m.mode + 1) % 3); return nil }
	step := func(m *chatView, dir int) tea.Cmd { m.setMode((m.mode + chatMode(3+dir)) % 3); return nil }
	agents := func(m *chatView) tea.Cmd { m.overlay = overlayAgents; return nil }
	all = append(all,
		palItem{group: "ACTIONS", glyph: "+", color: t.Muted, label: "New session", hint: "your next message starts it", key: "/new", run: func(m *chatView) tea.Cmd { m.newSession(); return nil }},
		palItem{group: "ACTIONS", glyph: t.G.Sel, color: t.Muted, label: "Agent and model", hint: m.agentLabel(), key: "ctrl+l", run: func(m *chatView) tea.Cmd { m.openPicker(overlayPicker); return m.loadSetup(false) }},
		palItem{group: "ACTIONS", glyph: "≡", color: t.Muted, label: "Skills and MCP", hint: "what goes into every session", key: "/skills", run: func(m *chatView) tea.Cmd {
			m.overlay, m.setupSel, m.setupNote = overlaySkills, 0, ""
			return m.loadSetup(false)
		}},
		palItem{group: "ACTIONS", glyph: "≡", color: t.Muted, label: "Verbosity: " + m.mode.String(), hint: "minimal · expandable · verbose", key: "ctrl+o", run: cycle},
		palItem{group: "ACTIONS", glyph: t.G.On, color: t.Muted, label: "Agents panel", hint: "sessions and waiting items", key: "tab", run: agents},
		palItem{group: "ACTIONS", glyph: "≡", color: t.Muted, label: "Board", hint: "the task files by status", key: "/board", run: func(m *chatView) tea.Cmd { n, c := m.openBoard(""); *m = n.(chatView); return c }},
		palItem{group: "ACTIONS", glyph: "→", color: t.Muted, label: "Open the web UI", hint: "same sessions in the browser", key: "/web", run: func(m *chatView) tea.Cmd { _, c := m.openWeb(); return c }},
		palItem{group: "ACTIONS", glyph: "→", color: t.Muted, label: "Connect", hint: strings.TrimPrefix(m.server+" "+t.G.Dot+" "+m.url, " "+t.G.Dot+" "), key: "/connect", run: func(m *chatView) tea.Cmd { return m.showConnect() }},
		palItem{group: "ACTIONS", glyph: t.G.Dot, color: t.Muted, label: "Daemon log", hint: fmt.Sprintf("%d events", len(m.noise)), key: "/log", run: func(m *chatView) tea.Cmd { m.showLog(); return nil }},
		palItem{group: "ACTIONS", glyph: "↓", color: t.Muted, label: "Jump to latest", key: "end", run: func(m *chatView) tea.Cmd { m.offset = 0; return nil }},
		palItem{group: "ACTIONS", glyph: t.G.Fail, color: t.Muted, label: "Quit", key: "ctrl+c", run: func(m *chatView) tea.Cmd { m.quitting = true; return tea.Quit }},
	)
	for _, s := range m.sessions {
		g, c := m.sessionGlyph(s.state)
		id, label := s.id, kit.Short(s.title, 48)
		if label == "" {
			label = s.id
		}
		all = append(all, palItem{group: "SESSIONS", glyph: g, color: c, label: label, hint: strings.Trim(s.agent+" "+t.G.Dot+" "+s.state, " "+t.G.Dot), run: func(m *chatView) tea.Cmd { m.open(id); return nil }})
	}
	all = append(all,
		palItem{group: "SETTINGS", glyph: t.G.Dot, color: t.Muted, label: "chat.mode", hint: "‹ " + m.mode.String() + " ›  how much of each turn the transcript shows", run: cycle, cycle: step},
		palItem{group: "SETTINGS", glyph: t.G.Dot, color: t.Muted, label: "daemon", hint: m.url, run: func(*chatView) tea.Cmd { return nil }},
	)
	for _, r := range m.palSet {
		all = append(all, m.settingItem(r))
	}
	q := strings.ToLower(m.pal.query)
	var out []palItem
	for _, it := range all {
		if q == "" || strings.Contains(strings.ToLower(it.group+" "+it.label+" "+it.hint), q) {
			out = append(out, it)
		}
	}
	return out
}

func (m chatView) sessionGlyph(state string) (string, color.Color) {
	t := m.th
	switch state {
	case "failed":
		return t.G.Fail, t.Err
	case "done":
		return t.G.Pass, t.Ok
	case "waiting":
		return t.G.Wait, t.Warn
	}
	return t.G.On, t.Accent
}

func (m chatView) toolGlyph(status string) (string, color.Color) {
	switch status {
	case "failed":
		return m.sessionGlyph("failed")
	case "running":
		return m.sessionGlyph("running")
	}
	return m.sessionGlyph("done")
}

// render draws message i at width w in the current mode; a row with no rows hides it.
func (m chatView) render(i, w int) []string {
	c := m.msgs[i]
	switch c.kind {
	case "qa":
		return m.qaRows(c, w)
	case "foot":
		for j := i - 1; j >= 0; j-- {
			if m.msgs[j].who == "you" {
				return []string{}
			}
		}
		return []string{m.lane(w, "", m.railMark(false), m.th.Border, m.th.Fg(m.th.Dim).Render(c.text), nil), m.gap(w)}
	case "shell":
		return m.shellRows(i, w)
	}
	if c.tool == nil && c.kind == "thinking" && m.mode == modeMinimal {
		a, _ := m.runOf(i)
		switch {
		case m.msgs[a].open:
			return m.textRows(c, w)
		case i == a:
			return m.summary(a, i, w)
		}
		return []string{}
	}
	if c.tool == nil {
		if (c.kind == "thinking" || c.kind == "turn") && m.mode != modeVerbose {
			return []string{}
		}
		return m.textRows(c, w)
	}
	a, b := m.runOf(i)
	switch m.mode {
	case modeMinimal:
		if m.msgs[a].open {
			return m.toolRows(i, w, true, i == b)
		}
		if i != a {
			return []string{}
		}
		return m.summary(a, b, w)
	case modeExpandable:
		return m.toolRows(i, w, c.open, i == b)
	}
	return m.toolRows(i, w, true, i == b)
}

// summary is a run of finished tools and thoughts collapsed to one dim line, "+ 2 commands,
// 1 thought", with the worst status and the time they took; enter or a click expands it.
func (m chatView) summary(a, b, w int) []string {
	t := m.th
	status, tools, thoughts, took := "ok", []chatTool{}, 0, 0.0
	dim := t.Fg(t.Dim)
	for _, c := range m.msgs[a : b+1] {
		tl := c.tool
		if tl == nil {
			if c.kind == "thinking" {
				thoughts++
			}
			continue
		}
		if tl.Status == "running" {
			continue
		}
		if tl.Status == "failed" {
			status = "failed"
		}
		tools = append(tools, *tl)
		var f float64
		if _, err := fmt.Sscanf(tl.Took, "%fs", &f); err == nil {
			took += f
		}
	}
	if len(tools)+thoughts == 0 {
		return []string{}
	}
	g, col := m.toolGlyph(status)
	right := ""
	if took > 0 {
		right = dim.Render(fmt.Sprintf("%.1fs", took))
	}
	var bg color.Color
	if a == m.focus {
		bg = t.Focus
	}
	return []string{m.spreadLane(w, t.Fg(col).Render(g), m.railMark(false), t.Border, m.activityChips(tools, thoughts), right, bg), m.gap(w)}
}

// toolLine colors one line of a tool's output: added lines ok, removed lines failed, hunk
// headers and notes dim.
func (m chatView) toolLine(l string) string {
	t := m.th
	switch {
	case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "@@"), strings.HasPrefix(l, "diff "), strings.HasPrefix(l, "index "), strings.HasPrefix(l, "…"):
		return t.Fg(t.Dim).Render(l)
	case strings.HasPrefix(l, "+"):
		return t.Fg(t.Ok).Render(l)
	case strings.HasPrefix(l, "-"):
		return t.Fg(t.Err).Render(l)
	}
	return t.Fg(t.Text).Render(l)
}

// toolRows is one tool block: status glyph, name, argument, stat and time on one row, its
// output below when open. A running tool spins and shimmers instead.
func (m chatView) toolRows(i, w int, open, last bool) []string {
	t := m.th
	tl := m.msgs[i].tool
	dim := t.Fg(t.Dim)
	mark, col := t.Fg(t.Ok).Render(t.G.Pass), t.Info
	arg := t.Fg(t.Text).Render(tl.Arg)
	switch tl.Status {
	case "failed":
		mark, col = t.Fg(t.Err).Render(t.G.Fail), t.Err
	case "running":
		mark, col = t.Fg(t.Warn).Render(t.Spinner(m.frame)), t.Warn
		n := ansi.StringWidth(tl.Arg)
		arg = t.Shimmer(tl.Arg, math.Mod(float64(m.frame)*0.6, float64(n+12))-6)
	}
	name := tl.Name
	if ansi.StringWidth(name) < 5 {
		name += strings.Repeat(" ", 5-ansi.StringWidth(name))
	}
	left := t.Fg(col).Bold(true).Render(name) + " " + arg
	var right string
	if m.mode == modeVerbose {
		var meta []string
		for _, s := range []string{tl.Took, tl.Stat} {
			if s != "" {
				meta = append(meta, s)
			}
		}
		right = dim.Render(strings.Join(meta, " "+t.G.Dot+" "))
	} else {
		right = t.Fg(t.Muted).Render(tl.Stat) + dim.Render("  "+tl.Took)
	}
	var bg color.Color
	if open {
		bg = t.Raised
	}
	if i == m.focus {
		bg = t.Focus
	}
	rail := m.railMark(false)
	rows := []string{m.spreadLane(w, mark, rail, t.Border, left, right, bg)}
	if open {
		for _, l := range tl.Lines {
			for _, wl := range strings.Split(ansi.Wrap(kit.Clean(l), m.textWidth(w)-2, ""), "\n") {
				rows = append(rows, m.lane(w, "", rail, t.Border, m.toolLine(wl), bg))
			}
		}
	}
	if last || (open && m.mode == modeVerbose) {
		rows = append(rows, m.gap(w))
	}
	return rows
}

// askDemo asks a sample question (or the one typed) on the question card through the daemon,
// the same path an agent's ask tool takes outside warden, so it can be seen and answered.
func (m *chatView) askDemo(q string) tea.Cmd {
	if m.ask == nil {
		m.system("/ask needs smithy daemon running: it carries the question and the answer")
		return nil
	}
	opts := []string{"yes", "no", "later"}
	if q == "" {
		q, opts = "Demo: which answer should this sample question get?", []string{"approve", "reject", "later"}
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	m.status("asked: answer it on the question card (a number, arrows and enter, or your own words)")
	return m.ask("ask:demo"+hexenc.EncodeToString(b), q, opts)
}

func spread(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		if rw := ansi.StringWidth(right); rw+10 < w {
			return ansi.Truncate(left, w-rw-1, "…") + " " + right
		}
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// turnResult is the heading's right side for the prompt c: how its turn went, once it ended
// (✓ and the agent's footer), or the spinner while it runs.
func (m chatView) turnResult(c *chatMsg) string {
	t := m.th
	at := -1
	for i, x := range m.msgs {
		if x == c {
			at = i
		}
	}
	if at < 0 {
		return ""
	}
	for _, x := range m.msgs[at+1:] {
		switch {
		case x.who == "you":
			return ""
		case x.kind == "foot":
			return t.Fg(t.Ok).Render(t.G.Pass+" ") + t.Fg(t.Dim).Render(x.text)
		}
	}
	return ""
}

// activityChips is a folded run of tools as one small tally per kind, "▪ 2 reads  ▪ 1 edit".
func (m chatView) activityChips(tools []chatTool, thoughts int) string {
	t := m.th
	var out []string
	for _, p := range strings.Split(strings.TrimPrefix(activityLabel(tools, thoughts), "+ "), ", ") {
		out = append(out, t.Fg(t.Info).Render(t.G.On)+" "+t.Fg(t.Muted).Render(p))
	}
	return strings.Join(out, "  ")
}
