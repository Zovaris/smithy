//go:build harness

package tui

import (
	"fmt"
	"image/color"
	"math"
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

// headRows is the header's height: the 16×8-pixel anvil of the dashboard header, four text rows.
const headRows = 4

// header is the anvil band: the dashboard's shaded anvil sprite on a steel dither band, with the
// name beside it; the band's waves and the ember streak move only while something runs.
func (m chatView) header(w int) []string {
	t := m.th
	name := "smithy"
	if Version != "" {
		name += " " + Version
	}
	if t.plain || t.ramp == nil || !t.g.unicode {
		out := make([]string, headRows)
		out[1] = ansi.Truncate("  "+name, w, "")
		return out
	}
	f := 0.0
	if m.animating() {
		f = float64(m.frame)
	}
	s := smallPlate
	ax := m.pad()
	nx := ax + s.w() + 2
	ph := headRows * 2
	px := func(x, y int) color.Color {
		if s.on(x-ax, y) {
			return dpick([]color.Color{t.ramp[3], t.ramp[4], t.ramp[5], t.muted, t.text, t.strong}, s.shade(x-ax, y), x, y)
		}
		v := 0.28 + 0.2*math.Sin(float64(x)*0.09+f*0.18+float64(y)*0.5) + 0.12*math.Sin(float64(x)*0.031-f*0.07)
		if x < nx+len(name)+3 {
			v *= 0.3
		}
		if f > 0 && y == ph-3 {
			hx := math.Mod(f*1.3, float64(w+40)) - 20
			if d := math.Abs(float64(x) - hx); d < 6 && d+(1-bayerAt(x, y))*4 < 5 {
				return t.accent
			}
		}
		return dpick(t.ramp, clamp01(v), x, y)
	}
	nameR := []rune(name)
	out := make([]string, headRows)
	for row := range out {
		var b strings.Builder
		for x := 0; x < w; x++ {
			top, bot := px(x, row*2), px(x, row*2+1)
			if row == 2 && x >= nx && x < nx+len(nameR) {
				fg := t.strong
				if x >= nx+len("smithy") {
					fg = t.dim
				}
				b.WriteString(lipgloss.NewStyle().Foreground(fg).Background(bot).Bold(x < nx+len("smithy")).Render(string(nameR[x-nx])))
				continue
			}
			b.WriteString(lipgloss.NewStyle().Foreground(top).Background(bot).Render("▀"))
		}
		out[row] = b.String()
	}
	return out
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
	if n := len(m.noise); n > 0 {
		left = dim.Render(m.noise[n-1].at.Format("15:04") + " " + m.noise[n-1].text)
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

// footer is the mockups' hint row: the keys on the left; agent, branch and cost on the right.
func (m chatView) footer(w int) string {
	t := m.th
	dim, key := t.fg(t.dim), t.fg(t.text)
	hint := func(k, what string) string { return key.Render(k) + dim.Render(" "+what) }
	p := strings.Repeat(" ", m.pad())
	hints := []string{hint("enter", "send"), hint("shift+enter", "newline"), hint("v", "mode: "+m.mode.String()), hint("tab", "agents"), hint("ctrl+k", "palette"), hint("?", "help")}
	var info []string
	if a := m.agentLabel(); a != "" {
		info = append(info, a)
	}
	if m.branch != "" {
		info = append(info, m.branch)
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
}

// floatBox is the Palette mockup's panel: a query row and a key row on the focus tier around
// grouped rows on the overlay tier, the selected row on the border tier.
func (m chatView) floatBox(w, h int, query, placeholder string, items []boxItem, sel int, foot string) []string {
	t := m.th
	tier := func(c color.Color) lipgloss.Style {
		if c == nil {
			return lipgloss.NewStyle()
		}
		return lipgloss.NewStyle().Background(c)
	}
	row := func(st lipgloss.Style, s string) string { return fill(st, w, s) }
	dim := t.fg(t.dim)
	q := t.fg(t.strong).Render(query)
	if query == "" {
		q = dim.Render(placeholder)
	}
	out := []string{row(tier(t.focus), spread("  "+t.fg(t.accent).Render(t.g.prompt)+"  "+q, dim.Render("esc")+"  ", w)), row(tier(t.overlay), "")}
	body := h - 4
	rows := make([]string, 0, len(items))
	selRow := 0
	n := 0
	for _, it := range items {
		if it.label == "" {
			if len(rows) > 0 {
				rows = append(rows, row(tier(t.overlay), ""))
			}
			rows = append(rows, row(tier(t.overlay), "  "+dim.Render(it.group)))
			continue
		}
		st, label := tier(t.overlay), t.fg(t.text)
		if n == sel {
			st, label, selRow = tier(t.border), t.fg(t.strong), len(rows)
		}
		left := "  " + t.fg(it.color).Render(it.glyph) + "  " + label.Render(it.label)
		if it.hint != "" {
			left += "  " + dim.Render(it.hint)
		}
		key := t.fg(t.muted).Render(it.key) + "  "
		left = ansi.Truncate(left, w-ansi.StringWidth(key)-2, "…")
		rows = append(rows, row(st, spread(left, key, w)))
		n++
	}
	start := 0
	if selRow >= body {
		start = selRow - body + 1
	}
	for i := start; i < len(rows) && i < start+body; i++ {
		out = append(out, rows[i])
	}
	out = append(out, row(tier(t.overlay), ""))
	return append(out, row(tier(t.focus), foot))
}

// overBox centers box over the dimmed screen rows, 15% from the top as in the mockup.
func (m chatView) overBox(screen []string, box []string, w int) []string {
	out := make([]string, len(screen))
	for i, r := range screen {
		out[i] = m.dimmed(r)
	}
	bw := 0
	if len(box) > 0 {
		bw = ansi.StringWidth(box[0])
	}
	x, y := (w-bw)/2, max(len(screen)*120/800, 1)
	for i, b := range box {
		if y+i < len(out) {
			out[y+i] = splice(out[y+i], b, x, w)
		}
	}
	return out
}

func (m chatView) boxSize(w, h int) (int, int) {
	return min(88, w-6), max(min(h-4, 30), 8)
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
	foot := spread("  "+keyHints(t, "↑↓", "move", "enter", "run", "tab", "next group"), t.fg(t.dim).Render(fmt.Sprintf("%d results", len(pal)))+"  ", bw)
	return m.overBox(screen, m.floatBox(bw, bh, m.pal.query, "Search actions, sessions, settings", items, m.pal.sel, foot), w)
}

// helpBox is ? and /help: every key and command.
func (m chatView) helpBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	items := []boxItem{{group: "KEYS"}}
	for _, k := range [][2]string{
		{"enter", "send; shift+enter or alt+enter for a new line"}, {"/", "commands"}, {"@", "a file from this folder"},
		{"tab", "agents and sessions"}, {"ctrl+k", "palette"}, {"ctrl+l", "agent and model"},
		{"v", "minimal · expandable · verbose (empty input)"}, {"pgup pgdn", "scroll; wheel too, home and end"}, {"click", "open a tool block or answer a decision"}, {"ctrl+c", "quit"},
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
	foot := "  " + keyHints(t, "↑↓", "scroll", "esc", "close")
	return m.overBox(screen, m.floatBox(bw, bh, m.pal.query, "Search keys and commands", items, m.pal.sel, foot), w)
}

// infoBox is a read-only panel: /connect, /remote, /web and /log.
func (m chatView) infoBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	items := []boxItem{}
	for _, l := range m.info.lines {
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
		}
		label, hint, _ := strings.Cut(l, "\t")
		items = append(items, boxItem{glyph: glyph, color: c, label: label, hint: hint})
	}
	sel := len(items)
	if m.info.scroll {
		sel = m.pal.sel
	}
	foot := "  " + keyHints(t, "esc", "close")
	if m.info.scroll {
		foot = "  " + keyHints(t, "↑↓", "scroll", "esc", "close")
	}
	return m.overBox(screen, m.floatBox(bw, bh, m.info.title, "", items, sel, foot), w)
}

// sidePanel is the Board mockup: the screen dimmed on the left, the agents panel on the right.
func (m chatView) sidePanel(screen []string, w int) []string {
	t := m.th
	pw := max(min(w*560/1280, w-20), min(44, w))
	x := w - pw
	rows := m.agentsRows(pw, len(screen))
	out := make([]string, len(screen))
	for i, r := range screen {
		out[i] = splice(m.dimmed(r), rows[i], x, w)
	}
	_ = t
	return out
}

// agentsRows draws the panel pw wide and h tall: every session with what it does now, then
// what waits on you.
func (m chatView) agentsRows(pw, h int) []string {
	t := m.th
	bg, sel := lipgloss.NewStyle(), lipgloss.NewStyle()
	if t.overlay != nil {
		bg, sel = bg.Background(t.overlay), sel.Background(t.border)
	}
	dim, strong := t.fg(t.dim), t.fg(t.strong)
	in := pw - 6
	line := func(st lipgloss.Style, s string) string {
		return fill(st, pw, "   "+ansi.Truncate(s, in, "…"))
	}
	running := 0
	for _, s := range m.sessions {
		if s.state == "running" {
			running++
		}
	}
	sum := fmt.Sprintf("%d running %s %d sessions", running, t.g.dot, len(m.sessions))
	if c := m.totalCost(); c > 0 {
		sum += fmt.Sprintf(" %s $%.2f", t.g.dot, c)
	}
	rows := []string{line(bg, ""), line(bg, spread(strong.Bold(true).Render("Agents")+"  "+dim.Render(sum), t.fg(t.text).Render("tab")+dim.Render(" close"), in)), line(bg, "")}
	for i, s := range m.sessions {
		st := bg
		if i == m.agentSel {
			st = sel
		}
		g, c := m.sessionGlyph(s.state)
		title := s.title
		if title == "" {
			title = s.id
		}
		when := s.state
		if !s.since.IsZero() && s.state == "running" {
			when = shortDur(time.Since(s.since).Truncate(time.Minute))
			if when == "0s" {
				when = "now"
			}
		}
		var whoParts []string
		for _, x := range []string{s.agent, s.model, s.id} {
			if x != "" {
				whoParts = append(whoParts, x)
			}
		}
		who := strings.Join(whoParts, " "+t.g.dot+" ")
		var bar string
		switch s.state {
		case "running":
			bar = t.loadingField(16, m.frame/2)
		case "waiting":
			bar = t.fg(t.warn).Render(strings.Repeat(t.g.shades[3], 16))
		case "failed":
			bar = t.fg(t.err).Render(strings.Repeat(t.g.shades[3], 16))
		default:
			bar = t.fg(t.muted).Render(strings.Repeat(t.g.shades[3], 16))
		}
		now := s.now
		if now == "" {
			now = s.kind
		}
		if i == m.current2() {
			now = t.fg(t.accent).Render(t.g.prompt) + " " + now
		}
		rows = append(rows,
			line(st, spread(t.fg(c).Render(g)+"  "+strong.Bold(true).Render(title), dim.Render(when), in)),
			line(st, "   "+spread(t.fg(t.muted).Render(who), bar, in-3)),
			line(st, "   "+t.fg(t.text).Render(now)),
			line(bg, ""))
	}
	if len(m.sessions) == 0 {
		rows = append(rows, line(bg, dim.Render("no sessions yet: type a message to start one")), line(bg, ""))
	}
	if len(m.pending) > 0 {
		rows = append(rows, line(bg, strong.Bold(true).Render("Waiting on you")+"  "+dim.Render(fmt.Sprintf("%d", len(m.pending)))), line(bg, ""))
		for _, d := range m.pending {
			rows = append(rows, line(bg, t.fg(t.warn).Render(t.g.wait)+"  "+t.fg(t.text).Render(d.Question)+"  "+dim.Render(strings.Join(d.Options, " "+t.g.dot+" "))))
		}
	}
	foot := line(bg, keyHints(t, "enter", "open", "s", "stop", "n", "new session", "↑↓", "move"))
	if len(rows) > h-1 {
		rows = rows[:h-1]
	}
	for len(rows) < h-1 {
		rows = append(rows, line(bg, ""))
	}
	return append(rows, foot)
}

// current2 is the index of the open session in m.sessions, -1 when none.
func (m chatView) current2() int {
	for i, s := range m.sessions {
		if s.id == m.current && m.current != "" {
			return i
		}
	}
	return -1
}

func (m chatView) totalCost() float64 {
	c := 0.0
	for _, v := range m.cost {
		c += v
	}
	return c
}

// spinRow is the transcript's last row while the open session works: the spinner and the
// shimmer over what it does now, as in Chat-Expandable.
func (m chatView) spinRow(w int) string {
	t := m.th
	spin := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	g := spin[m.frame%len(spin)]
	if !t.g.unicode {
		g = []string{"|", "/", "-", "\\"}[m.frame%4]
	}
	what, since := "working", time.Time{}
	for _, s := range m.sessions {
		if s.id == m.current {
			if s.now != "" {
				what = s.now
			}
			since = s.turn
		}
	}
	took := ""
	if !since.IsZero() {
		took = fmt.Sprintf("%.1fs", time.Since(since).Seconds())
	}
	n := ansi.StringWidth(what)
	pos := math.Mod(float64(m.frame)*0.6, float64(n+12)) - 6
	p := strings.Repeat(" ", m.pad())
	return spread(p+"  "+t.fg(t.accent).Render(g)+" "+t.shimmer(what, pos), t.fg(t.dim).Render(took)+p+"  ", w)
}

// fill lays s on st's background w cells wide, cut to fit. Unlike Style.Width it keeps the
// background behind text that carries its own styles: every reset inside s re-applies it.
func fill(st lipgloss.Style, w int, s string) string {
	s = ansi.Truncate(s, w, "")
	pad := strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
	c := st.GetBackground()
	if _, none := c.(lipgloss.NoColor); none || c == nil {
		return s + pad
	}
	r, g, b, _ := c.RGBA()
	on := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\x1b[0m", "\x1b[m"), "\x1b[m", "\x1b[m"+on)
	return on + s + pad + "\x1b[m"
}
