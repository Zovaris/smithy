//go:build harness

package tui

import (
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// chatFrameMsg advances the motion one frame; frames run only while something works.
type chatFrameMsg struct{}

func frameTick() tea.Cmd {
	return tea.Tick(time.Second/fps, func(time.Time) tea.Msg { return chatFrameMsg{} })
}

// animating reports whether anything on screen moves: a session working or a tool running.
func (m chatView) animating() bool {
	if m.welcome != welcomeOff || m.shimmer > 0 {
		return true
	}
	for _, s := range m.sessions {
		if s.state == "running" {
			return true
		}
	}
	for i := len(m.msgs) - 1; i >= 0 && i >= len(m.msgs)-8; i-- {
		if c := m.msgs[i]; c.tool != nil && c.tool.Status == "running" {
			return true
		}
	}
	return false
}

// working reports whether the open session runs a turn with no running tool on screen to
// show it, so the transcript adds a spinner row.
func (m chatView) working() bool {
	if m.current == "" {
		return false
	}
	for _, s := range m.sessions {
		if s.id == m.current && s.state == "running" {
			n := len(m.msgs)
			return n == 0 || m.msgs[n-1].tool == nil || m.msgs[n-1].tool.Status != "running"
		}
	}
	return false
}

// pad is the side gutter: 24px of the 1280px mockups, three cells at 100 columns and up.
func (m chatView) pad() int {
	if m.width >= 100 {
		return 3
	}
	return 1
}

// headRows is the header's height: one bar and a spacer row.
const headRows = 2

// header is one bar on the raised surface: the name, version and project on the left, a state
// pill and the clock in the corner. Model, branch, tokens and cost live in the footer and the
// current tool in the transcript, so none of them repeat here. Only the pill's dot moves, a slow
// breath while sessions run; the text never shifts.
func (m chatView) header(w int) []string {
	project := ""
	if m.dir != "" {
		project = filepath.Base(m.dir)
	}
	pill := m.pill
	if tag := strings.TrimPrefix(strings.Join([]string{m.goalTag(), m.tendTag()}, "  "), "  "); strings.TrimSpace(tag) != "" {
		tag = strings.TrimSpace(tag)
		pill = func(bar lipgloss.Style) string {
			return bar.Foreground(m.th.dim).Render(tag) + bar.Render("  ") + m.pill(bar)
		}
	}
	return []string{m.th.headBar(w, m.pad(), project, pill, m.clockNow().Format("15:04")), ""}
}

// clockMsg redraws the header clock; it fires on each minute, nothing else runs while idle.
type clockMsg struct{}

func untilMinute(t time.Time) time.Duration {
	return t.Truncate(time.Minute).Add(time.Minute).Sub(t)
}

func clockTick() tea.Cmd {
	return tea.Tick(untilMinute(time.Now()), func(time.Time) tea.Msg { return clockMsg{} })
}

// clockNow is the header clock's time.
func (m chatView) clockNow() time.Time {
	if m.clock != nil {
		return m.clock()
	}
	return time.Now()
}

// pill is "idle", "● waiting" while a decision waits on you and nothing runs, or "● N running"
// with the dot breathing between dim and the accent.
func (m chatView) pill(bar lipgloss.Style) string {
	t := m.th
	n := 0
	for _, s := range m.sessions {
		if s.state == "running" {
			n++
		}
	}
	if n == 0 && len(m.pending) > 0 {
		return bar.Foreground(t.warn).Render(t.g.on) + bar.Foreground(t.text).Render(" waiting")
	}
	if n == 0 {
		return bar.Foreground(t.dim).Render("idle")
	}
	dot := t.accent
	if !t.plain && t.ramp != nil {
		ph := float64(m.frame) / (breathSecs * fps) * 2 * math.Pi
		dot = lipgloss.Blend1D(8, t.dim, t.accent)[int((1-math.Cos(ph))/2*7+0.5)]
	}
	return bar.Foreground(dot).Render(t.g.on) + bar.Foreground(t.text).Render(fmt.Sprintf(" %d running", n))
}

// kTok is a token count with a k or M suffix past a thousand.
func kTok(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// addStreamed counts n characters of agent text toward session id's live tok/s.
func (m *chatView) addStreamed(id string, n int) {
	if m.streamed == nil {
		m.streamed = map[string]int{}
	}
	m.streamed[id] += n
}

// tokRate is session id's output rate this turn: streamed characters at about four per token
// over the time since the turn began; 0 before the first text or second.
func (m chatView) tokRate(id string) float64 {
	for _, s := range m.sessions {
		if s.id == id && !s.turn.IsZero() {
			if el := time.Since(s.turn).Seconds(); el >= 1 {
				return float64(m.streamed[id]) / 4 / el
			}
		}
	}
	return 0
}

// statusLine is one dim row above the composer: the latest daemon note on the left, where the
// daemon runs and how to stop it on the right.
func (m chatView) statusLine(w int) string {
	t := m.th
	dim, key := t.fg(t.dim), t.fg(t.text)
	p := strings.Repeat(" ", m.pad())
	right := ""
	switch m.conn {
	case "connecting":
		right = t.fg(t.accent).Render(t.g.on) + dim.Render(" connecting")
	case "reconnecting":
		right = t.fg(t.err).Render(t.g.fail) + dim.Render(" reconnecting: "+short(m.connErr, 30))
	}
	where := ""
	switch m.where {
	case whereBackground:
		where = dim.Render("background daemon "+t.g.dot+" ") + key.Render("smithy stop") + dim.Render(" ends it")
	case whereStandalone:
		where = dim.Render("standalone daemon " + t.g.dot + " stops with this chat")
	case whereRemote:
		where = dim.Render(m.url)
	}
	if right != "" {
		right += dim.Render("  ")
	}
	right += where
	left := ""
	for i := len(m.noise) - 1; i >= 0; i-- {
		if n := m.noise[i]; !n.quiet {
			if time.Since(n.at) < noiseShown {
				left = dim.Render(n.text)
			}
			break
		}
	}
	if m.offset > 0 {
		left = t.fg(t.info).Render(fmt.Sprintf("→ %d rows up · end jumps back", m.offset))
	}
	room := w - 2*m.pad() - ansi.StringWidth(right) - 2
	if room < 10 {
		return ansi.Truncate(p+right, w, "")
	}
	left = ansi.Truncate(left, room, "…")
	return p + left + strings.Repeat(" ", max(w-2*m.pad()-ansi.StringWidth(left)-ansi.StringWidth(right), 1)) + right + p
}

// noiseShown is how long a daemon line stays in the status area; /log keeps them all.
const noiseShown = 20 * time.Second

// footer is the mockups' hint row: the keys on the left; agent, branch, tokens and cost on the right.
func (m chatView) footer(w int) string {
	t := m.th
	dim, key := t.fg(t.dim), t.fg(t.text)
	hint := func(k, what string) string { return key.Render(k) + dim.Render(" "+what) }
	p := strings.Repeat(" ", m.pad())
	hints := []string{hint("enter", "send"), hint("shift+enter", "newline"), hint("ctrl+o", "mode: "+m.mode.String()), hint("tab", "agents"), hint("ctrl+k", "palette"), hint("?", "help")}
	var info []string
	if a := m.agentLabel(); a != "" {
		info = append(info, a)
	}
	if m.branch != "" {
		info = append(info, m.branch)
	}
	if n, ok := m.toks[m.current]; ok {
		info = append(info, kTok(n)+" tok")
	}
	if c, ok := m.cost[m.current]; ok {
		info = append(info, fmt.Sprintf("$%.2f", c))
	}
	right := dim.Render(strings.Join(info, " "+t.g.dot+" ")) + p
	left := p + strings.Join(hints, "  ")
	for len(hints) > 2 && w-ansi.StringWidth(left)-ansi.StringWidth(right) < 1 {
		hints = hints[:len(hints)-1]
		left = p + strings.Join(hints, "  ")
	}
	if gap := w - ansi.StringWidth(left) - ansi.StringWidth(right); gap >= 1 {
		return left + strings.Repeat(" ", gap) + right
	}
	return ansi.Truncate(left, w, "")
}

// dimmed is a screen row behind an overlay: its text kept, its colors faded to the steel edge.
func (m chatView) dimmed(s string) string {
	t := m.th
	plain := strings.Map(func(r rune) rune {
		if r == '▀' || r == '▄' {
			return ' '
		}
		return r
	}, ansi.Strip(s))
	if t.plain || t.ramp == nil {
		return plain
	}
	return t.fg(t.ramp[len(t.ramp)-1]).Render(plain)
}

// splice lays box over row at column x, keeping the row's cells on both sides.
func splice(row, box string, x, w int) string {
	bw := ansi.StringWidth(box)
	left := ansi.Truncate(row, x, "")
	if lw := ansi.StringWidth(left); lw < x {
		left += strings.Repeat(" ", x-lw)
	}
	right := ""
	if ansi.StringWidth(row) > x+bw {
		right = ansi.TruncateLeft(row, x+bw, "")
	}
	return ansi.Truncate(left+box+right, w, "")
}

// boxItem is one row of a floating box: a group heading when label is "" and group is set.
type boxItem struct {
	group, glyph     string
	color            color.Color
	label, hint, key string
	static           bool // shown, never selected
}

// floatBox is the one panel every picker uses: a rounded frame in the border color around a
// title bar (prompt, query or title on the left, esc on the right), grouped rows with the
// selected one marked by an accent bar, and a key footer.
func (m chatView) floatBox(w, h int, query, placeholder string, items []boxItem, sel int, foot string) []string {
	t := m.th
	bg := lipgloss.NewStyle()
	if t.overlay != nil {
		bg = bg.Background(t.overlay)
	}
	edge := t.fg(t.dim)
	if t.border != nil && t.ramp != nil {
		edge = t.fg(t.ramp[len(t.ramp)-1])
	}
	if t.overlay != nil {
		edge = edge.Background(t.overlay)
	}
	in := max(w-2, 1)
	g := frameGlyphs(t.uni)
	line := func(l, r string) string { return edge.Render(l + strings.Repeat(g.h, in) + r) }
	row := func(s string) string { return edge.Render(g.v) + fill(bg, in, s) + edge.Render(g.v) }
	dim := t.fg(t.dim)
	q := t.fg(t.strong).Render(query)
	if query == "" {
		q = t.fg(t.text).Render(placeholder)
	}
	out := []string{line(g.tl, g.tr), row(spread(" "+t.fg(t.accent).Render(t.g.prompt)+" "+q, dim.Render("esc")+" ", in)), line(g.lt, g.rt)}
	body := max(h-8, 1)
	rows := make([]string, 0, len(items))
	selRow := 0
	n := 0
	for _, it := range items {
		if it.label == "" {
			if len(rows) > 0 {
				rows = append(rows, row(""))
			}
			for _, l := range strings.Split(ansi.Wordwrap(it.group, max(in-4, 1), ""), "\n") {
				rows = append(rows, row("   "+t.fg(t.muted).Bold(true).Render(ansi.Truncate(l, max(in-4, 1), "…"))))
			}
			continue
		}
		mark, label := "  ", t.fg(t.text)
		if it.static {
			label = t.fg(t.muted)
		} else if n == sel {
			mark, label, selRow = t.fg(t.accent).Render(g.sel)+" ", t.fg(t.strong).Bold(true), len(rows)
		}
		left := " " + mark + t.fg(it.color).Render(it.glyph) + "  " + label.Render(it.label)
		if it.hint != "" {
			left += "  " + dim.Render(it.hint)
		}
		key := t.fg(t.muted).Render(it.key) + "  "
		left = ansi.Truncate(left, max(in-ansi.StringWidth(key)-1, 1), "…")
		rows = append(rows, row(spread(left, key, in)))
		if !it.static {
			n++
		}
	}
	start := 0
	if selRow >= body {
		start = selRow - body + 1
	}
	out = append(out, row(""))
	for i := start; i < len(rows) && i < start+body; i++ {
		out = append(out, rows[i])
	}
	out = append(out, row(""), line(g.lt, g.rt), row(ansi.Truncate(foot, in, "…")), line(g.bl, g.br))
	return out
}

type boxFrame struct{ tl, tr, bl, br, lt, rt, h, v, sel string }

func frameGlyphs(uni bool) boxFrame {
	if uni {
		return boxFrame{"┌", "┐", "└", "┘", "├", "┤", "─", "│", "▌"}
	}
	return boxFrame{"+", "+", "+", "+", "+", "+", "-", "|", ">"}
}

// overBox centers box over the dimmed screen rows, 15% from the top as in the mockup, or higher
// when a tall box would otherwise run past the last row.
func (m chatView) overBox(screen []string, box []string, w int) []string {
	out := make([]string, len(screen))
	for i, r := range screen {
		out[i] = r
		if m.welcome == welcomeOff {
			out[i] = m.dimmed(r)
		}
	}
	bw := 0
	if len(box) > 0 {
		bw = ansi.StringWidth(box[0])
	}
	x, y := (w-bw)/2, max(min(len(screen)*120/800, len(screen)-len(box)), 1)
	for i, b := range box {
		if y+i < len(out) {
			out[y+i] = splice(out[y+i], b, x, w)
		}
	}
	return out
}

func (m chatView) boxSize(w, h int) (int, int) {
	return max(min(88, w-6, w*9/10), min(w, 20)), max(min(h-2, 32), 12)
}

func keyHints(t theme, pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, t.fg(t.text).Render(pairs[i])+t.fg(t.dim).Render(" "+pairs[i+1]))
	}
	return strings.Join(parts, "   ")
}

// paletteBox is ctrl+k: what waits on you, actions, sessions and settings.
func (m chatView) paletteBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	var items []boxItem
	group := ""
	pal := m.palItems()
	for _, it := range pal {
		if it.group != group {
			group = it.group
			items = append(items, boxItem{group: group})
		}
		items = append(items, boxItem{glyph: it.glyph, color: it.color, label: it.label, hint: it.hint, key: it.key})
	}
	foot := spread("  "+keyHints(t, "↑↓", "move", "enter", "run", "tab", "next group"), t.fg(t.dim).Render(fmt.Sprintf("%d results", len(pal)))+"  ", bw-2)
	title := "Search actions, sessions, settings"
	if m.picker != nil {
		title = m.connectTitle()
		foot = spread("  "+keyHints(t, "↑↓", "move", "enter", "connect", "esc", "close"), t.fg(t.dim).Render(fmt.Sprintf("%d rows", len(pal)))+"  ", bw-2)
	}
	return m.overBox(screen, m.floatBox(bw, bh, m.pal.query, title, items, m.pal.sel, foot), w)
}

// helpBox is ? and /help: every key and command.
func (m chatView) helpBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	items := []boxItem{{group: "KEYS"}}
	for _, k := range [][2]string{
		{"enter", "send; shift+enter or alt+enter for a new line"}, {"/", "commands"}, {"@", "a file from this folder"},
		{"tab", "agents and sessions"}, {"ctrl+k", "palette"}, {"ctrl+l", "agent and model"},
		{"ctrl+o", "minimal · expandable · verbose"}, {"? enter", "this help (or /help)"}, {"pgup pgdn", "scroll; wheel too, home and end"}, {"click", "open a tool block or answer a decision"},
		{"letter, enter", "an option's first letter alone answers the decision; typing is a message"},
		{"/name or $name", "run an installed skill on the rest of the line"}, {"ctrl+c", "quit"},
	} {
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: k[0], hint: k[1]})
	}
	items = append(items, boxItem{group: "COMMANDS"})
	q := strings.ToLower(m.pal.query)
	for _, c := range slashCommands {
		if q == "" || strings.Contains(c.name+" "+c.desc, q) {
			items = append(items, boxItem{glyph: "/", color: t.muted, label: "/" + c.name + " " + c.args, hint: c.desc})
		}
	}
	if s := m.setupSt; s != nil && len(s.Skills) > 0 {
		items = append(items, boxItem{group: "SKILLS · /skills turns them on for every session"})
		for _, sk := range s.Skills {
			if p := s.Paths[sk]; p != "" && (q == "" || strings.Contains(sk, q)) {
				items = append(items, boxItem{glyph: "/", color: t.muted, label: "/" + sk + "  $" + sk, hint: skillDir(p)})
			}
		}
	}
	foot := "  " + keyHints(t, "↑↓", "scroll", "esc", "close")
	return m.overBox(screen, m.floatBox(bw, bh, m.pal.query, "Search keys and commands", items, m.pal.sel, foot), w)
}

// infoBox is a read-only panel: /connect, /remote, /web and /log.
func (m chatView) infoBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	items := []boxItem{}
	lines := m.info.lines
	if p := m.info.set; p != nil {
		lines = append([]string{"#SETTING", "-> " + settingRow(*p)}, lines...)
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "#") {
			items = append(items, boxItem{group: strings.TrimPrefix(l, "#")})
			continue
		}
		glyph, c := t.g.dot, t.muted
		switch {
		case strings.HasPrefix(l, "ok "):
			glyph, c, l = t.g.pass, t.ok, l[3:]
		case strings.HasPrefix(l, "-> "):
			glyph, c, l = "→", t.info, l[3:]
		case strings.HasPrefix(l, "!! "):
			glyph, c, l = t.g.fail, t.err, l[3:]
		case strings.HasPrefix(l, "?? "):
			glyph, c, l = t.g.wait, t.warn, l[3:]
		}
		label, hint, _ := strings.Cut(l, "\t")
		items = append(items, boxItem{glyph: glyph, color: c, label: label, hint: hint})
	}
	sel := len(items)
	if m.info.set != nil {
		sel = 0
	} else if m.info.scroll {
		sel = m.pal.sel
	}
	foot := "  " + keyHints(t, "esc", "close")
	switch {
	case m.info.set != nil:
		foot = "  " + keyHints(t, "←→", "change", "enter", "save", "esc", "close")
	case m.info.scroll:
		foot = "  " + keyHints(t, "↑↓", "scroll", "esc", "close")
	}
	return m.overBox(screen, m.floatBox(bw, bh, m.info.title, "", items, sel, foot), w)
}

func (m chatView) totalCost() float64 {
	c := 0.0
	for _, v := range m.cost {
		c += v
	}
	return c
}

// spinRow is the transcript's last row while the open session works: a small four-cell
// pulse, what the session does now, and on the right the live tok/s and the turn's time.
func (m chatView) spinRow(w int) string {
	t := m.th
	g := m.pulse()
	what, since := "working", time.Time{}
	for _, s := range m.sessions {
		if s.id == m.current {
			if s.now != "" {
				what = s.now
			}
			since = s.turn
		}
	}
	var right []string
	if r := m.tokRate(m.current); r > 0 {
		right = append(right, fmt.Sprintf("%.0f tok/s", r))
	}
	if !since.IsZero() {
		right = append(right, fmt.Sprintf("%.1fs", time.Since(since).Seconds()))
	}
	p := strings.Repeat(" ", m.pad())
	return spread(p+"  "+g+" "+t.fg(t.muted).Render(what), t.fg(t.dim).Render(strings.Join(right, "  "))+p+"  ", w)
}

// pulse is the loader: four small cells with one lit cell drifting across, dim elsewhere.
func (m chatView) pulse() string {
	t := m.th
	on, off := "▪", "·"
	if !t.g.unicode {
		on, off = "*", "."
	}
	lit := (m.frame / 3) % 4
	var b strings.Builder
	for i := range 4 {
		if i == lit {
			b.WriteString(t.fg(t.accent).Render(on))
		} else {
			b.WriteString(t.fg(t.dim).Render(off))
		}
	}
	return b.String()
}

// fill lays s on st's background w cells wide, cut to fit. Unlike Style.Width it keeps the
// background behind text that carries its own styles: every reset inside s re-applies it.
func fill(st lipgloss.Style, w int, s string) string {
	s = ansi.Truncate(s, w, "")
	pad := strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
	c := st.GetBackground()
	if _, none := c.(lipgloss.NoColor); none {
		return s + pad
	}
	r, g, b, _ := c.RGBA()
	on := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\x1b[0m", "\x1b[m"), "\x1b[m", "\x1b[m"+on)
	return on + s + pad + "\x1b[m"
}
