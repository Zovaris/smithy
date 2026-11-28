//go:build harness

package tui

import (
	"context"
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
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/layout"
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
	overlayWorkflows // /workflows: the scheduled workflows
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
	links                    *daemon.SessionLinks
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
}

// event routes one daemon event: session events to their conversation, decisions to the
// question card, a turn's tools and thinking to the transcript, everything else to the status line.
func (m *chatView) event(kind, session string, at time.Time, data json.RawMessage) {
	if strings.HasPrefix(kind, "session_") && session != "" {
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
			m.push(chatMsg{who: "daemon", kind: kind, text: clean(d.Text)})
		}
		return
	case daemon.KindAsked:
		var d daemon.Decision
		if json.Unmarshal(data, &d) == nil && d.ID != "" {
			m.setPending(append(m.pending, d))
			m.offerAsk()
		}
	case daemon.KindAnswered:
		var o daemon.Outcome
		if json.Unmarshal(data, &o) == nil {
			m.unpend(o.ID)
		}
	case daemon.KindWorkflowNote:
		var n struct{ Workflow, Title, Text string }
		if json.Unmarshal(data, &n) == nil && n.Workflow != "" {
			m.system(clean("workflow " + n.Workflow + " · " + n.Title + ": " + n.Text))
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
			s.kind, s.state = kind, state
			return
		}
	}
	m.sessions = append(m.sessions, chatSession{id: id, kind: kind, state: state, since: time.Now(), turn: time.Now()})
}

// setPending replaces the decisions waiting on the user, keeping one per id in arrival order.
func (m *chatView) setPending(ds []daemon.Decision) {
	var out []daemon.Decision
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
	var out []daemon.Decision
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
		m.system(m.th.g.fail + " answer failed: " + d.err.Error())
		return
	}
	m.unpend(d.id)
}

func (m *chatView) system(text string) {
	m.offset = 0
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

// runOf is the first and last message of the run of tool messages around i.
func (m chatView) runOf(i int) (a, b int) {
	a, b = i, i
	for a > 0 && m.msgs[a-1].tool != nil {
		a--
	}
	for b < len(m.msgs)-1 && m.msgs[b+1].tool != nil {
		b++
	}
	return a, b
}

// toggle opens or closes tool message i: the whole run in minimal, one block in expandable.
func (m chatView) toggle(i int) chatView {
	if i < 0 || i >= len(m.msgs) || m.msgs[i].tool == nil || m.mode == modeVerbose {
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
	case "ctrl+k":
		m.overlay, m.pal = overlayPalette, palette{}
		return true, nil
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
		m.overlay, m.pal = overlayPalette, palette{}
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
		return m.openWorkflows()
	case "serve", "set", "unset", "key", "keys", "doctor", "tend", "prompt", "channels":
		return m.settingsCommand(name, arg)
	case "quit":
		return m, tea.Quit
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
	inputY := m.height - 1 - m.input.Height()
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
	case msg.Y >= head && msg.Y < head+m.rows():
		vis, idx := m.visible()
		j := msg.Y - head - (m.rows() - len(vis))
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
		return m, tea.Quit
	}
	switch m.overlay {
	case overlaySetup, overlayPicker, overlaySkills, overlayWelcome:
		return m.setupKey(msg)
	case overlayAsk:
		return m.askKey(msg)
	case overlayAgents:
		return m.agentsKey(k)
	case overlayProject:
		return m.projectKey(k)
	case overlayBoard:
		return m.boardKey(k)
	case overlayWorkflows:
		return m.workflowsKey(k)
	case overlayHelp, overlayInfo:
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
			m.overlay, m.infoBack = m.infoBack, overlayNone
			return m, nil
		case "up":
			m.pal.sel = max(m.pal.sel-1, 0)
		case "down":
			m.pal.sel++
		case "backspace":
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
		m.overlay, m.pal = overlayPalette, palette{}
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
				if err := api.StopSession(context.Background(), id); err != nil {
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
		all = append(all, palItem{group: "WAITING ON YOU", glyph: t.g.wait, color: t.warn, label: d.Question, hint: strings.Join(d.Options, " "+t.g.dot+" "),
			run: func(m *chatView) tea.Cmd {
				m.pending[0], m.pending[i] = m.pending[i], m.pending[0]
				return nil
			}})
	}
	cycle := func(m *chatView) tea.Cmd { m.setMode((m.mode + 1) % 3); return nil }
	agents := func(m *chatView) tea.Cmd { m.overlay = overlayAgents; return nil }
	all = append(all,
		palItem{group: "ACTIONS", glyph: "+", color: t.muted, label: "New session", hint: "your next message starts it", key: "/new", run: func(m *chatView) tea.Cmd { m.newSession(); return nil }},
		palItem{group: "ACTIONS", glyph: t.g.sel, color: t.muted, label: "Agent and model", hint: m.agentLabel(), key: "ctrl+l", run: func(m *chatView) tea.Cmd { m.openPicker(overlayPicker); return m.loadSetup(false) }},
		palItem{group: "ACTIONS", glyph: "≡", color: t.muted, label: "Skills and MCP", hint: "what goes into every session", key: "/skills", run: func(m *chatView) tea.Cmd {
			m.overlay, m.setupSel, m.setupNote = overlaySkills, 0, ""
			return m.loadSetup(false)
		}},
		palItem{group: "ACTIONS", glyph: "≡", color: t.muted, label: "Verbosity: " + m.mode.String(), hint: "minimal · expandable · verbose", key: "ctrl+o", run: cycle},
		palItem{group: "ACTIONS", glyph: t.g.on, color: t.muted, label: "Agents panel", hint: "sessions and waiting items", key: "tab", run: agents},
		palItem{group: "ACTIONS", glyph: "≡", color: t.muted, label: "Board", hint: "the task files by status", key: "/board", run: func(m *chatView) tea.Cmd { n, c := m.openBoard(""); *m = n.(chatView); return c }},
		palItem{group: "ACTIONS", glyph: "→", color: t.muted, label: "Open the web UI", hint: "same sessions in the browser", key: "/web", run: func(m *chatView) tea.Cmd { _, c := m.openWeb(); return c }},
		palItem{group: "ACTIONS", glyph: "→", color: t.muted, label: "Connect", hint: strings.TrimPrefix(m.server+" "+t.g.dot+" "+m.url, " "+t.g.dot+" "), key: "/connect", run: func(m *chatView) tea.Cmd { return m.showConnect() }},
		palItem{group: "ACTIONS", glyph: t.g.dot, color: t.muted, label: "Daemon log", hint: fmt.Sprintf("%d events", len(m.noise)), key: "/log", run: func(m *chatView) tea.Cmd { m.showLog(); return nil }},
		palItem{group: "ACTIONS", glyph: "↓", color: t.muted, label: "Jump to latest", key: "end", run: func(m *chatView) tea.Cmd { m.offset = 0; return nil }},
		palItem{group: "ACTIONS", glyph: t.g.fail, color: t.muted, label: "Quit", key: "ctrl+c", run: func(*chatView) tea.Cmd { return tea.Quit }},
	)
	for _, s := range m.sessions {
		g, c := m.sessionGlyph(s.state)
		id, label := s.id, short(s.title, 48)
		if label == "" {
			label = s.id
		}
		all = append(all, palItem{group: "SESSIONS", glyph: g, color: c, label: label, hint: strings.Trim(s.agent+" "+t.g.dot+" "+s.state, " "+t.g.dot), run: func(m *chatView) tea.Cmd { m.open(id); return nil }})
	}
	all = append(all,
		palItem{group: "SETTINGS", glyph: t.g.dot, color: t.muted, label: "chat.mode", hint: m.mode.String(), run: cycle},
		palItem{group: "SETTINGS", glyph: t.g.dot, color: t.muted, label: "daemon", hint: m.url, run: func(*chatView) tea.Cmd { return nil }},
	)
	q := strings.ToLower(m.pal.query)
	var out []palItem
	for _, it := range all {
		if q == "" || strings.Contains(strings.ToLower(it.label+" "+it.hint), q) {
			out = append(out, it)
		}
	}
	return out
}

func (m chatView) sessionGlyph(state string) (string, color.Color) {
	t := m.th
	switch state {
	case "failed":
		return t.g.fail, t.err
	case "done":
		return t.g.pass, t.ok
	case "waiting":
		return t.g.wait, t.warn
	}
	return t.g.on, t.accent
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

// summary is a run of finished tools on one line, as in Chat-Minimal: the worst status, each
// tool's name (an edit with its file and stat, a command with its first words and result), and
// the time they took.
func (m chatView) summary(a, b, w int) []string {
	t := m.th
	status, parts, took := "ok", []string{}, 0.0
	dim := t.fg(t.dim)
	for _, c := range m.msgs[a : b+1] {
		tl := c.tool
		if tl.Status == "running" {
			continue
		}
		if tl.Status == "failed" {
			status = "failed"
		}
		part := dim.Render(tl.Name)
		switch tl.Name {
		case "Edit", "Write", "Diff", "MultiEdit":
			if f := lastPart(tl.Arg); f != "" {
				part += dim.Render(" " + f)
			}
			if strings.HasPrefix(tl.Stat, "+") {
				part += " " + t.fg(t.text).Render(tl.Stat)
			}
		case "Bash":
			part += dim.Render(" " + firstWords(tl.Arg, 2))
			if tl.Status == "failed" {
				part += " " + t.fg(t.err).Render("failed")
			} else {
				part += " " + t.fg(t.ok).Render("ok")
			}
		}
		parts = append(parts, part)
		var f float64
		if _, err := fmt.Sscanf(tl.Took, "%fs", &f); err == nil {
			took += f
		}
	}
	if len(parts) == 0 {
		return []string{}
	}
	g, col := m.toolGlyph(status)
	right := ""
	if took > 0 {
		right = dim.Render(fmt.Sprintf("%.1fs", took))
	}
	p := strings.Repeat(" ", m.pad())
	line := spread(p+"  "+t.fg(col).Render(g)+"  "+strings.Join(parts, dim.Render(" "+t.g.dot+" ")), right+p+"  ", w)
	return []string{m.blockRow(a, w, ansi.Truncate(line, w, "…")), ""}
}

func lastPart(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	return strings.Join(f, " ")
}

// toolLine colors one line of a tool's output: added lines ok, removed lines failed, hunk
// headers and notes dim.
func (m chatView) toolLine(l string) string {
	t := m.th
	switch {
	case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"), strings.HasPrefix(l, "@@"), strings.HasPrefix(l, "diff "), strings.HasPrefix(l, "index "), strings.HasPrefix(l, "…"):
		return t.fg(t.dim).Render(l)
	case strings.HasPrefix(l, "+"):
		return t.fg(t.ok).Render(l)
	case strings.HasPrefix(l, "-"):
		return t.fg(t.err).Render(l)
	}
	return t.fg(t.text).Render(l)
}

// toolRows is one tool block: status glyph, name, argument, stat and time on one row, its
// output below when open. A running tool spins and shimmers instead.
func (m chatView) toolRows(i, w int, open, last bool) []string {
	t := m.th
	tl := m.msgs[i].tool
	p := strings.Repeat(" ", m.pad())
	dim := t.fg(t.dim)
	g, col := m.toolGlyph(tl.Status)
	arg := t.fg(t.text).Render(tl.Arg)
	if tl.Status == "running" {
		spin := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		if t.g.unicode {
			g = spin[m.frame%len(spin)]
		}
		n := ansi.StringWidth(tl.Arg)
		arg = t.shimmer(tl.Arg, math.Mod(float64(m.frame)*0.6, float64(n+12))-6)
	}
	name := tl.Name
	if ansi.StringWidth(name) < 5 {
		name += strings.Repeat(" ", 5-ansi.StringWidth(name))
	}
	left := p + "  " + t.fg(col).Render(g) + "  " + t.fg(t.strong).Bold(true).Render(name) + " " + arg
	var right string
	if m.mode == modeVerbose {
		var meta []string
		for _, s := range []string{tl.Took, tl.Stat} {
			if s != "" {
				meta = append(meta, s)
			}
		}
		right = dim.Render(strings.Join(meta, " "+t.g.dot+" "))
	} else {
		took := tl.Took
		if n := ansi.StringWidth(took); n < 5 {
			took = strings.Repeat(" ", 5-n) + took
		}
		right = dim.Render(tl.Stat + "  " + took)
	}
	head := spread(ansi.Truncate(left, w-ansi.StringWidth(right)-m.pad()-4, "…"), right+p+"  ", w)
	tier := t.focus
	if m.mode == modeVerbose {
		tier = t.raised
	}
	if !open {
		tier = nil
	}
	rows := []string{m.tierRow(i, w, tier, head)}
	if open {
		ind := p + strings.Repeat(" ", 11)
		for _, l := range tl.Lines {
			for _, wl := range strings.Split(ansi.Wrap(clean(l), max(w-2*m.pad()-14, 10), ""), "\n") {
				rows = append(rows, m.tierRow(i, w, tier, ind+m.toolLine(wl)))
			}
		}
		rows = append(rows, m.tierRow(i, w, tier, ""))
	}
	if last || (open && m.mode == modeVerbose) {
		rows = append(rows, "")
	}
	return rows
}

// tierRow lays a block row on background c between the gutters; the focused block also gets
// the border mark at its left edge.
func (m chatView) tierRow(i, w int, c color.Color, s string) string {
	t := m.th
	if i == m.focus && c == nil {
		c = t.focus
	}
	if c == nil {
		return s
	}
	p := m.pad()
	bw := w - 2*p
	inner := ansi.Cut(s, p, w-p)
	if i == m.focus {
		inner = t.fg(t.accent).Render("▎") + ansi.Cut(inner, 1, bw)
	}
	return strings.Repeat(" ", p) + fill(lipgloss.NewStyle().Background(c), bw, inner) + strings.Repeat(" ", p)
}

// blockRow lays one row of message i: the focused block sits on the focus tier.
func (m chatView) blockRow(i, w int, s string) string { return m.tierRow(i, w, nil, s) }

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
