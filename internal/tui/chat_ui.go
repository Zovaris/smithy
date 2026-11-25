//go:build harness

package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/daemon"
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
	overlaySetup  // first run: every agent ready or missing
	overlayPicker // ctrl+l, /agent, /model
	overlaySkills // /skills
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
type chatSession struct{ id, kind, state string }

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

type stripHit struct {
	x0, x1 int
	answer string
}

// event adds one daemon event to the transcript, the decision strip and the sessions it tracks.
func (m *chatView) event(kind, session, at string, data json.RawMessage) {
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
		}
	case daemon.KindAnswered:
		var o daemon.Outcome
		if json.Unmarshal(data, &o) == nil {
			m.unpend(o.ID)
		}
	}
	if t := eventText(kind, session, data); t != "" {
		m.push(chatMsg{who: "daemon", at: at, text: t})
	}
}

func (m *chatView) track(id, kind string) {
	state := "running"
	switch kind {
	case "failed", "error":
		state = "failed"
	case "done", "finished", "merged", "turn", "stopped":
		state = "done"
	case daemon.KindAsked:
		state = "waiting"
	}
	for i := range m.sessions {
		if m.sessions[i].id == id {
			m.sessions[i].kind, m.sessions[i].state = kind, state
			return
		}
	}
	m.sessions = append(m.sessions, chatSession{id: id, kind: kind, state: state})
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

// shortcut handles the single keys that act only while the input is empty.
func (m *chatView) shortcut(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "tab":
		m.overlay = overlayAgents
		return true, nil
	case "ctrl+k":
		m.overlay, m.pal = overlayPalette, palette{}
		return true, nil
	case "ctrl+l":
		m.openPicker(overlayPicker)
		return true, m.loadSetup(false)
	}
	if m.input.Value() != "" || len([]rune(msg.Text)) != 1 {
		return false, nil
	}
	if len(m.pending) > 0 {
		for _, o := range m.pending[0].Options {
			if strings.EqualFold(firstRune(o), msg.Text) {
				return true, m.answer(o)
			}
		}
	}
	if msg.Text == "v" {
		m.setMode((m.mode + 1) % 3)
		return true, nil
	}
	return false, nil
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
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
	case "agents":
		m.overlay = overlayAgents
	case "palette":
		m.overlay, m.pal = overlayPalette, palette{}
	case "agent":
		return m.agentCommand(arg)
	case "model":
		return m.modelCommand(arg)
	case "skills":
		m.overlay, m.setupSel, m.setupNote = overlaySkills, 0, ""
		return m, m.loadSetup(false)
	case "setup":
		m.openPicker(overlaySetup)
		return m, m.loadSetup(false)
	case "answer":
		if arg == "" {
			m.system("usage: /answer <text>")
			return m, nil
		}
		return m, m.answer(arg)
	case "quit":
		return m, tea.Quit
	case "help":
		m.system("/agent [name] · /model [name] · /skills · /setup · /mode [minimal|expandable|verbose] · /agents · /palette · /answer <text> · /quit")
	default:
		m.system("unknown command /" + name)
	}
	return m, nil
}

// click focuses what is under the pointer: an option on the strip answers, a transcript row
// focuses its message (and opens or closes a tool block), the input row focuses the input.
func (m chatView) click(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	strip := m.stripRows()
	switch {
	case msg.Y < strip:
		_, hits := m.strip(max(m.width, 20))
		for _, h := range hits {
			if msg.X >= h.x0 && msg.X < h.x1 {
				return m, m.answer(h.answer)
			}
		}
	case msg.Y == m.height-2:
		m.setFocus(-1)
	case msg.Y < m.height-2 && m.overlay == overlayNone:
		vis, idx := m.visible()
		j := msg.Y - strip - (m.rows() - len(vis))
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
	if m.overlay == overlaySetup || m.overlay == overlayPicker || m.overlay == overlaySkills {
		return m.setupKey(msg)
	}
	if m.overlay == overlayAgents {
		switch k {
		case "tab", "esc":
			m.overlay = overlayNone
		case "ctrl+k":
			m.overlay, m.pal = overlayPalette, palette{}
		}
		return m, nil
	}
	items := m.palItems()
	switch k {
	case "esc", "ctrl+k":
		m.overlay = overlayNone
	case "up":
		m.pal.sel = max(m.pal.sel-1, 0)
	case "down":
		m.pal.sel = min(m.pal.sel+1, max(len(items)-1, 0))
	case "backspace":
		if r := []rune(m.pal.query); len(r) > 0 {
			m.pal = palette{query: string(r[:len(r)-1])}
		}
	case "enter":
		m.overlay = overlayNone
		if m.pal.sel < len(items) {
			return m, items[m.pal.sel].run(&m)
		}
	default:
		if msg.Text != "" {
			m.pal = palette{query: m.pal.query + msg.Text}
		}
	}
	return m, nil
}

// palItems are the palette's rows after the query filter, in group order.
func (m chatView) palItems() []palItem {
	t := m.th
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
		palItem{group: "ACTIONS", glyph: t.g.prompt, color: t.muted, label: "Agent and model", hint: m.agentLabel(), key: "ctrl+l", run: func(m *chatView) tea.Cmd { m.openPicker(overlayPicker); return m.loadSetup(false) }},
		palItem{group: "ACTIONS", glyph: "≡", color: t.muted, label: "Skills and MCP", hint: "what goes into every session", key: "/skills", run: func(m *chatView) tea.Cmd {
			m.overlay, m.setupSel, m.setupNote = overlaySkills, 0, ""
			return m.loadSetup(false)
		}},
		palItem{group: "ACTIONS", glyph: "≡", color: t.muted, label: "Verbosity: " + m.mode.String(), hint: "minimal · expandable · verbose", key: "v", run: cycle},
		palItem{group: "ACTIONS", glyph: t.g.on, color: t.muted, label: "Agents panel", hint: "sessions and waiting items", key: "tab", run: agents},
		palItem{group: "ACTIONS", glyph: "↓", color: t.muted, label: "Jump to latest", key: "end", run: func(m *chatView) tea.Cmd { m.offset = 0; return nil }},
		palItem{group: "ACTIONS", glyph: t.g.fail, color: t.muted, label: "Quit", key: "ctrl+c", run: func(*chatView) tea.Cmd { return tea.Quit }},
	)
	for _, s := range m.sessions {
		g, c := m.sessionGlyph(s.state)
		all = append(all, palItem{group: "SESSIONS", glyph: g, color: c, label: s.id, hint: s.kind, run: agents})
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

// summary is a run of tools on one line, as in Chat-Minimal: the worst status, then the names.
func (m chatView) summary(a, b, w int) []string {
	t := m.th
	status, names := "ok", make([]string, 0, b-a+1)
	for _, c := range m.msgs[a : b+1] {
		names = append(names, c.tool.Name)
		if c.tool.Status == "failed" || (c.tool.Status == "running" && status == "ok") {
			status = c.tool.Status
		}
	}
	g, col := m.toolGlyph(status)
	line := "  " + t.fg(col).Render(g) + " " + t.fg(t.muted).Render(strings.Join(names, " "+t.g.dot+" "))
	return []string{m.blockRow(a, w, line), ""}
}

// toolRows is one tool block: a header with its status, arg and stats; its output when open.
func (m chatView) toolRows(i, w int, open, last bool) []string {
	t := m.th
	tl := m.msgs[i].tool
	g, col := m.toolGlyph(tl.Status)
	arrow := ""
	if m.mode != modeVerbose {
		arrow = t.g.sel + " "
		if open {
			arrow = t.g.open + " "
		}
	}
	var meta []string
	for _, s := range []string{tl.Took, tl.Stat} {
		if s != "" {
			meta = append(meta, s)
		}
	}
	head := "  " + t.fg(t.dim).Render(arrow) + t.fg(col).Render(g) + " " + t.fg(t.strong).Render(tl.Name)
	if tl.Arg != "" {
		head += " " + t.fg(t.text).Render(tl.Arg)
	}
	if len(meta) > 0 {
		head += t.fg(t.dim).Render("  " + strings.Join(meta, " "+t.g.dot+" "))
	}
	rows := []string{m.blockRow(i, w, ansi.Truncate(head, w-1, "…"))}
	if open {
		for _, l := range tl.Lines {
			for _, wl := range strings.Split(ansi.Wrap(clean(l), max(w-10, 10), ""), "\n") {
				rows = append(rows, m.blockRow(i, w, "      "+t.fg(t.dim).Render(wl)))
			}
		}
	}
	if last {
		rows = append(rows, "")
	}
	return rows
}

// blockRow lays one row of message i: the focused block sits on the focus tier behind a border.
func (m chatView) blockRow(i, w int, s string) string {
	t := m.th
	if i != m.focus {
		return s
	}
	row := lipgloss.NewStyle()
	if t.focus != nil {
		row = row.Background(t.focus)
	}
	return row.Width(w).MaxWidth(w).Render(t.fg(t.border).Render(t.g.pipe) + ansi.Cut(s, 1, w))
}

func (m chatView) stripRows() int {
	if len(m.pending) == 0 {
		return 0
	}
	return 1
}

// strip is the sticky top row for the first decision waiting on the user, and where each of
// its options sits so a click can answer it.
func (m chatView) strip(w int) (string, []stripHit) {
	t := m.th
	d := m.pending[0]
	row := lipgloss.NewStyle()
	if t.overlay != nil {
		row = row.Background(t.overlay)
	}
	s := "  " + t.fg(t.warn).Render(t.g.wait) + " " + t.fg(t.strong).Render(short(d.Question, max(w/2, 20))) + "   "
	var hits []stripHit
	for _, o := range d.Options {
		x := ansi.StringWidth(s)
		s += t.fg(t.text).Render(firstRune(o)) + " " + t.fg(t.dim).Render(o)
		hits = append(hits, stripHit{x0: x, x1: ansi.StringWidth(s), answer: o})
		s += "  "
	}
	if len(d.Options) == 0 {
		s += t.fg(t.dim).Render("/answer <text>  ")
	}
	if n := len(m.pending) - 1; n > 0 {
		s += t.fg(t.dim).Render(fmt.Sprintf("+%d more", n))
	}
	return row.Width(w).MaxWidth(w).Render(ansi.Truncate(s, w, "")), hits
}

func (m chatView) panel(w int, rows []string) []string {
	t := m.th
	bg := lipgloss.NewStyle()
	if t.overlay != nil {
		bg = bg.Background(t.overlay)
	}
	n := m.rows()
	if len(rows) > n {
		rows = rows[:n]
	}
	out := make([]string, 0, n)
	for _, r := range rows {
		out = append(out, bg.Width(w).MaxWidth(w).Render(ansi.Truncate(r, w, "")))
	}
	for len(out) < n {
		out = append(out, bg.Width(w).Render(""))
	}
	return out
}

func spread(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// agentsPanel is the tab overlay, as in the Board mockup: the sessions seen and what waits on you.
func (m chatView) agentsPanel(w int) []string {
	t := m.th
	running := 0
	for _, s := range m.sessions {
		if s.state == "running" {
			running++
		}
	}
	dim := t.fg(t.dim)
	rows := []string{
		"",
		spread("  "+t.fg(t.strong).Render("Agents"), dim.Render("tab close  "), w),
		"  " + dim.Render(fmt.Sprintf("%d running %s %d sessions", running, t.g.dot, len(m.sessions))),
		"",
	}
	for _, s := range m.sessions {
		g, c := m.sessionGlyph(s.state)
		rows = append(rows, "  "+t.fg(c).Render(g)+" "+t.fg(t.strong).Render(fmt.Sprintf("%-12s", s.id))+" "+dim.Render(s.kind))
	}
	if len(m.sessions) == 0 {
		rows = append(rows, "  "+dim.Render("no sessions yet"))
	}
	if len(m.pending) > 0 {
		rows = append(rows, "", "  "+t.fg(t.muted).Render("Waiting on you"))
		for _, d := range m.pending {
			rows = append(rows, "  "+t.fg(t.warn).Render(t.g.wait)+" "+t.fg(t.strong).Render(fmt.Sprintf("%-12s", d.ID))+" "+t.fg(t.text).Render(d.Question))
		}
	}
	return m.panel(w, rows)
}

// palettePanel is the ctrl+k overlay, as in the Palette mockup: a query and grouped results.
func (m chatView) palettePanel(w int) []string {
	t := m.th
	dim := t.fg(t.dim)
	q := m.pal.query
	if q == "" {
		q = dim.Render("Search actions, sessions and settings")
	}
	rows := []string{"", spread("  "+t.fg(t.accent).Render(t.g.prompt)+" "+q, dim.Render("esc  "), w), ""}
	group := ""
	sel := lipgloss.NewStyle()
	if t.border != nil {
		sel = sel.Background(t.border)
	}
	for n, it := range m.palItems() {
		if it.group != group {
			group = it.group
			rows = append(rows, "  "+t.fg(t.muted).Render(group))
		}
		lead, label := "  ", t.fg(t.text)
		if n == m.pal.sel {
			lead, label = t.g.sel+" ", t.fg(t.strong)
		}
		r := spread("  "+lead+t.fg(it.color).Render(it.glyph)+" "+label.Render(it.label)+"  "+dim.Render(it.hint), t.fg(t.text).Render(it.key)+"  ", w)
		if n == m.pal.sel {
			r = sel.Width(w).MaxWidth(w).Render(r)
		}
		rows = append(rows, r)
	}
	foot := dim.Render("  ↑↓ move   enter run   esc close")
	n := m.rows()
	if len(rows) >= n {
		rows = rows[:n-1]
	}
	for len(rows) < n-1 {
		rows = append(rows, "")
	}
	return m.panel(w, append(rows, foot))
}
