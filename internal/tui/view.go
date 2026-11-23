package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/charmbracelet/x/ansi"
)

func cardPath(batch, id string) string { return filepath.Join(cards.Home(), batch, id+".json") }

type rect struct{ x, y, w, h int }

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// View renders the dashboard.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "smithy · " + viewNames[m.view] + " · " + m.watchWord()
	return v
}

func (m Model) size() (int, int) { return max(m.width, 40), max(m.height, 12) }

// frame is where the header, the tab bar, the body and the footer go.
func (m Model) frame() (head []string, tabY int, body rect, footY int) {
	w, h := m.size()
	head = m.header(w)
	tabY = len(head)
	footY = h - 1
	body = rect{0, tabY + 1, w, max(footY-tabY-1, 3)}
	return
}

// panes splits the body for the current view: "main", plus "side" (Agents preview, Incoming
// skipped, Plans tasks) and "top" (Incoming sources) when there is room.
func (m Model) panes() map[string]rect {
	_, _, b, _ := m.frame()
	out := map[string]rect{}
	wide := b.w >= 120
	switch m.view {
	case vAgents, vPlans:
		if !wide {
			if m.pane[m.view] == 1 {
				out["side"] = b
			} else {
				out["main"] = b
			}
			return out
		}
		mw := b.w * 66 / 100
		if m.view == vPlans {
			mw = b.w * 40 / 100
		}
		out["main"] = rect{b.x, b.y, mw, b.h}
		out["side"] = rect{b.x + mw, b.y, b.w - mw, b.h}
	case vIncoming:
		th := min(len(m.snap.Watcher.Sources), 4) + 3
		rest := rect{b.x, b.y + th, b.w, b.h - th}
		out["top"] = rect{b.x, b.y, b.w, th}
		if wide {
			mw := b.w * 58 / 100
			out["main"] = rect{rest.x, rest.y, mw, rest.h}
			out["side"] = rect{rest.x + mw, rest.y, rest.w - mw, rest.h}
		} else {
			mh := rest.h * 3 / 5
			out["main"] = rect{rest.x, rest.y, rest.w, mh}
			out["side"] = rect{rest.x, rest.y + mh, rest.w, rest.h - mh}
		}
	default:
		out["main"] = b
	}
	return out
}

// cards are the Overview's equal cells: 3 columns from 150 cells, 2 from 80, else 1.
func (m Model) cardRects() []rect {
	_, _, b, _ := m.frame()
	cols := 1
	switch {
	case b.w >= 150:
		cols = 3
	case b.w >= 80:
		cols = 2
	}
	rows := 6 / cols
	cw, ch := b.w/cols, b.h/rows
	var out []rect
	for i := 0; i < 6; i++ {
		r := rect{b.x + (i%cols)*cw, b.y + (i/cols)*ch, cw, ch}
		if i%cols == cols-1 {
			r.w = b.w - (cols-1)*cw
		}
		if i/cols == rows-1 {
			r.h = b.h - (rows-1)*ch
		}
		out = append(out, r)
	}
	return out
}

func (m Model) render() string {
	w, h := m.size()
	head, tabY, _, footY := m.frame()
	layers := []*lipgloss.Layer{
		lipgloss.NewLayer(strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", w)+"\n", h), "\n")),
		lipgloss.NewLayer(strings.Join(head, "\n")),
		lipgloss.NewLayer(m.tabbar(w)).Y(tabY),
		lipgloss.NewLayer(m.footer(w)).Y(footY),
	}
	for _, l := range m.body() {
		layers = append(layers, l)
	}
	if m.toast != "" {
		t := m.th
		box := m.panelStyle(t.accent).Render(t.fg(t.text).Render(m.toast))
		layers = append(layers, lipgloss.NewLayer(box).X(max(w-lipgloss.Width(box)-2, 0)).Y(max(footY-lipgloss.Height(box), 0)).Z(2))
	}
	var over string
	switch {
	case m.textOpen:
		over = m.textBox()
	case m.helpOpen:
		over = m.helpBox(w, h)
	case m.modal:
		over = m.modalBox(w, h-tabY-2)
	}
	if over != "" {
		x := max((w-lipgloss.Width(over))/2, 0)
		y := tabY + 1 + max((footY-tabY-1-lipgloss.Height(over))/2, 0)
		layers = append(layers, lipgloss.NewLayer(over).X(x).Y(y).Z(3))
	}
	out := strings.Split(lipgloss.NewCompositor(layers...).Render(), "\n")
	for len(out) < h {
		out = append(out, "")
	}
	for i := range out[:h] {
		out[i] = pad(out[i], w)
	}
	return strings.Join(out[:h], "\n")
}

func (m Model) panelStyle(border color.Color) lipgloss.Style {
	t := m.th
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if !t.g.unicode {
		st = st.Border(lipgloss.NormalBorder())
	}
	if !t.plain && border != nil {
		st = st.BorderForeground(border)
	}
	return st
}

// pad fits a styled line to exactly w cells.
func pad(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

// box draws a rounded panel with its title and a badge in the top border, one blank column
// inside each side, and the given lines.
func (m Model) box(title, badge string, focused bool, border color.Color, r rect, lines []string) string {
	t := m.th
	bc := t.border
	if border != nil {
		bc = border
	}
	if focused {
		bc = t.accent
	}
	bs := t.fg(bc)
	tl, tr, bl, br, hz, vt := "╭", "╮", "╰", "╯", "─", "│"
	if !t.g.unicode {
		tl, tr, bl, br, hz, vt = "+", "+", "+", "+", "-", "|"
	}
	ts := t.fg(t.text).Bold(!t.plain)
	if focused {
		ts = t.fg(t.accent).Bold(true)
	}
	head := bs.Render(tl+hz+" ") + ts.Render(title)
	if badge != "" {
		head += t.fg(t.muted).Render(" " + t.g.dot + " " + badge)
	}
	head = ansi.Truncate(head+" ", r.w-1, "")
	head += bs.Render(strings.Repeat(hz, max(r.w-1-lipgloss.Width(head), 0)) + tr)
	inner := r.w - 4
	out := []string{head}
	for i := 0; i < r.h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, bs.Render(vt)+" "+pad(l, inner)+" "+bs.Render(vt))
	}
	out = append(out, bs.Render(bl+strings.Repeat(hz, max(r.w-2, 0))+br))
	return strings.Join(out[:max(r.h, 1)], "\n")
}

func layer(s string, r rect) *lipgloss.Layer { return lipgloss.NewLayer(s).X(r.x).Y(r.y) }

// body renders the current view into layers.
func (m Model) body() []*lipgloss.Layer {
	t := m.th
	v := m.view
	if v == vOverview {
		return m.overview()
	}
	ps := m.panes()
	var out []*lipgloss.Layer
	if r, ok := ps["top"]; ok {
		out = append(out, layer(m.box("Sources", "", false, nil, r, m.sourceLines(r.w-4)), r))
	}
	if r, ok := ps["main"]; ok {
		rs := m.rows(v)
		lines, _ := m.table(m.cols(v), rs, m.sel[v], m.pane[v] == 0, r.w-4, r.h-2, m.empty(v))
		title := viewNames[v]
		badge := fmt.Sprint(len(rs))
		switch v {
		case vAgents:
			badge += " " + t.g.dot + " sort " + agentSorts[m.agentSort]
		case vIncoming:
			title = "Queue"
		case vActivity:
			badge += " " + t.g.dot + " type " + activityTypes[m.actType]
		}
		if f := m.filter[v]; f != "" {
			badge += " " + t.g.dot + " filter “" + f + "”"
		}
		out = append(out, layer(m.box(title, badge, m.pane[v] == 0, nil, r, lines), r))
	}
	if r, ok := ps["side"]; ok {
		var lines []string
		title, badge := "", ""
		switch v {
		case vAgents:
			title = "Preview"
			lines = m.preview(r.w-4, r.h-2)
		case vIncoming:
			rs := m.rows2(v)
			title, badge = "Recently skipped", fmt.Sprint(len(rs))
			lines, _ = m.table([]col{{"issue", 9, false, 0}, {"title", 0, true, 0}, {"why", 24, false, 1}, {"seen", 8, false, 2}}, rs, m.sel2[v], m.pane[v] == 1, r.w-4, r.h-2,
				[]string{"", t.fg(t.muted).Italic(true).Render("Nothing skipped lately: every issue seen passed the filters.")})
		case vPlans:
			rs := m.rows2(v)
			title = "Tasks"
			if ps := m.rows(vPlans); m.sel[vPlans] < len(ps) {
				title = "Tasks " + t.g.dot + " " + ps[m.sel[vPlans]].cells[0].s
			}
			badge = fmt.Sprint(len(rs))
			lines, _ = m.table([]col{{"#", 3, false, 0}, {"status", 10, false, 0}, {"task", 0, true, 0}}, rs, m.sel2[v], m.pane[v] == 1, r.w-4, r.h-2,
				[]string{"", t.fg(t.muted).Italic(true).Render("Pick a plan on the left.")})
		}
		out = append(out, layer(m.box(title, badge, m.pane[v] == 1, nil, r, lines), r))
	}
	return out
}

func (m Model) empty(v view) []string {
	t := m.th
	mut := t.fg(t.muted).Italic(true)
	cmd := func(s string) string { return t.fg(t.accent).Render("$ " + s) }
	if m.filter[v] != "" {
		return []string{"", mut.Render("Nothing matches “" + m.filter[v] + "”."), t.fg(t.accent).Render("esc") + t.fg(t.muted).Render(" clears the filter")}
	}
	switch v {
	case vAgents:
		return []string{"", mut.Render(meaning[v]), cmd("smithy warden status")}
	case vIncoming:
		return []string{"", mut.Render("Nothing queued: issues from Linear or GitHub wait here for the cap and hours."), cmd("smithy auto poll")}
	case vNeeds:
		return []string{"", t.fg(t.ok).Render(t.g.pass + " Nothing needs you."), mut.Render(meaning[v])}
	case vActivity:
		return []string{"", mut.Render("No events yet. smithy auto and smithy cards set write ~/.smithy/events.jsonl.")}
	case vWorktrees:
		return []string{"", mut.Render("No worktrees: running workers' worktrees appear here.")}
	}
	return []string{"", mut.Render(meaning[v]), cmd(`smithy tasks add <dir> "<title>"`)}
}

// fitCols keeps the columns that fit w, most important first, in their original order.
func fitCols(cols []col, w int) ([]col, []int) {
	keep := make([]bool, len(cols))
	used := 2
	for p := 0; p <= 9; p++ {
		for i, c := range cols {
			if c.prio != p || keep[i] {
				continue
			}
			need := c.w + 2
			if c.flex {
				need = 24
			}
			if used+need <= w {
				keep[i] = true
				used += need
			}
		}
	}
	var out []col
	var idx []int
	flexW := w - used + 24
	for i, c := range cols {
		if keep[i] {
			if c.flex {
				c.w = max(flexW-2, 8)
			}
			out = append(out, c)
			idx = append(idx, i)
		}
	}
	return out, idx
}

// table draws a header and the rows that fit h around the selection; it returns the lines and
// the index of the first row shown.
func (m Model) table(cols []col, rs []row, sel int, focused bool, w, h int, empty []string) ([]string, int) {
	t := m.th
	if len(rs) == 0 {
		return empty, 0
	}
	cs, idx := fitCols(cols, w)
	var hb strings.Builder
	hb.WriteString("  ")
	for _, c := range cs {
		hb.WriteString(pad(c.name, c.w) + "  ")
	}
	lines := []string{t.fg(t.muted).Render(hb.String())}
	n := h - 1
	start := 0
	if sel >= n {
		start = sel - n + 1
	}
	for i := start; i < len(rs) && i < start+n; i++ {
		r := rs[i]
		var b strings.Builder
		if i == sel && focused {
			var p strings.Builder
			p.WriteString(t.g.sel + " ")
			for k, c := range cs {
				s := ""
				if idx[k] < len(r.cells) {
					s = r.cells[idx[k]].s
				}
				p.WriteString(pad(short(s, c.w), c.w) + "  ")
			}
			st := lipgloss.NewStyle().Reverse(true).Bold(true)
			if !t.plain {
				st = lipgloss.NewStyle().Bold(true).Foreground(t.surface).Background(t.accent)
			}
			lines = append(lines, st.Render(pad(p.String(), w)))
			continue
		}
		b.WriteString("  ")
		for k, c := range cs {
			var cl cell
			if idx[k] < len(r.cells) {
				cl = r.cells[idx[k]]
			}
			st := t.fg(cl.c)
			if cl.bold && !t.plain {
				st = st.Bold(true)
			}
			b.WriteString(st.Render(pad(short(cl.s, c.w), c.w)) + "  ")
		}
		lines = append(lines, b.String())
	}
	return lines, start
}

func (m Model) preview(w, h int) []string {
	t := m.th
	r, ok := m.selected()
	if m.pane[vAgents] == 1 {
		rs := m.rows(vAgents)
		if m.sel[vAgents] < len(rs) {
			r, ok = rs[m.sel[vAgents]], true
		}
	}
	if !ok {
		return []string{"", t.fg(t.muted).Italic(true).Render("Select a worker to see its timeline and log.")}
	}
	mut := t.fg(t.muted)
	lines := []string{t.fg(t.text).Bold(!t.plain).Render(r.title), mut.Render(r.cells[1].s), "", t.fg(t.accent).Render("timeline")}
	tl := r.timeline
	if len(tl) > 8 {
		tl = tl[len(tl)-8:]
	}
	for i, e := range tl {
		g := t.g.tee
		if i == len(tl)-1 {
			g = t.g.elbow
		}
		lines = append(lines, mut.Render(g+" "+e[0]+"  ")+t.fg(t.text).Render(e[1]))
	}
	if len(tl) == 0 {
		lines = append(lines, mut.Render("no events yet"))
	}
	lines = append(lines, "", t.fg(t.accent).Render("log")+mut.Render("  l opens it all"))
	logs := r.logTail
	if r.logPath != "" {
		logs = tail(r.logPath, 40)
	}
	room := h - len(lines)
	if len(logs) > room {
		logs = logs[len(logs)-max(room, 0):]
	}
	if len(logs) == 0 {
		lines = append(lines, mut.Render("no log yet"))
	}
	for _, l := range logs {
		lines = append(lines, mut.Render(short(l, w)))
	}
	return lines
}

func (m Model) sourceLines(w int) []string {
	t := m.th
	wt := m.snap.Watcher
	mut, val := t.fg(t.muted), t.fg(t.text)
	var out []string
	for _, s := range wt.Sources {
		name := s
		if s == "linear" && wt.Team != "" {
			name += " (" + wt.Team + ")"
		}
		last := "never polled"
		next := "-"
		var lp time.Time
		for _, e := range m.snap.Events {
			if e.Kind == "poll" && e.Source == s {
				lp = e.T
			}
		}
		if lp.IsZero() && wt.Polled {
			lp = wt.LastPoll
		}
		if !lp.IsZero() {
			last = "polled " + ago(m.snap.At.Sub(lp))
			next = fmt.Sprintf("next in %ds", max(int(lp.Add(time.Duration(wt.Every)*time.Second).Sub(m.snap.At).Seconds()), 0))
		}
		rate := "rate n/a"
		if n, ok := m.snap.Rates[s]; ok {
			rate = fmt.Sprintf("%d requests left", n)
		}
		dot := t.fg(t.ok).Render(t.g.on)
		if !wt.Alive {
			dot = t.fg(t.err).Render(t.g.on)
		}
		out = append(out, dot+" "+val.Render(fmt.Sprintf("%-14s", name))+mut.Render(fmt.Sprintf("%-18s %-14s %s", last, next, rate)))
	}
	if len(out) == 0 {
		out = []string{mut.Render("No sources configured: smithy config set auto.sources github")}
	}
	return out
}

func (m Model) tabLabel(v view) string {
	label := fmt.Sprintf(" %d %s ", v+1, viewNames[v])
	if m.compactTabs() && v != m.view {
		label = fmt.Sprintf(" %d ", v+1)
	}
	return label
}

func (m Model) compactTabs() bool {
	w, _ := m.size()
	n := 2
	for v := view(0); v < nViews; v++ {
		n += len([]rune(viewNames[v])) + 6
	}
	return n > w
}

// tabbar is the row of view pills under the header: the active one filled.
func (m Model) tabbar(w int) string {
	t := m.th
	var b strings.Builder
	b.WriteString(" ")
	needs := len(m.rows(vNeeds))
	for v := view(0); v < nViews; v++ {
		l := m.tabLabel(v)
		switch {
		case v == m.view:
			st := lipgloss.NewStyle().Bold(true).Reverse(true)
			if !t.plain {
				st = lipgloss.NewStyle().Bold(true).Foreground(t.surface).Background(t.accent)
			}
			b.WriteString(st.Render(l))
		case v == vNeeds && needs > 0:
			b.WriteString(t.fg(t.warn).Bold(!t.plain).Render(l))
		default:
			b.WriteString(t.fg(t.muted).Render(l))
		}
		if v == vNeeds && needs > 0 {
			b.WriteString(t.fg(t.warn).Bold(!t.plain).Render(fmt.Sprintf("%d ", needs)))
		}
		b.WriteString(" ")
	}
	return pad(b.String(), w)
}

func (m Model) tabAt(x, y int) (view, bool) {
	_, tabY, _, _ := m.frame()
	if y != tabY {
		return 0, false
	}
	needs := len(m.rows(vNeeds))
	pos := 1
	for v := view(0); v < nViews; v++ {
		wd := lipgloss.Width(m.tabLabel(v))
		if x >= pos && x < pos+wd {
			return v, true
		}
		pos += wd + 1
		if v == vNeeds && needs > 0 {
			pos += len(fmt.Sprint(needs)) + 1
		}
	}
	return 0, false
}

// rowAt maps a click in the main table to its row.
func (m Model) rowAt(x, y int) (int, bool) {
	r, ok := m.panes()["main"]
	if !ok || !r.has(x, y) || m.view == vOverview {
		return 0, false
	}
	_, start := m.table(m.cols(m.view), m.rows(m.view), m.sel[m.view], true, r.w-4, r.h-2, nil)
	i := start + y - r.y - 2
	if i < start || i >= len(m.rows(m.view)) {
		return 0, false
	}
	return i, true
}

func (m Model) footer(w int) string {
	t := m.th
	kst := t.fg(t.accent)
	lbl := t.fg(t.muted)
	if m.filtering {
		return pad(" "+m.input.View()+lbl.Render("   enter keep "+t.g.dot+" esc clear"), w)
	}
	render := func(bs []key.Binding) string {
		var parts []string
		for _, b := range bs {
			h := b.Help()
			parts = append(parts, kst.Render(h.Key)+" "+lbl.Render(h.Desc))
		}
		return strings.Join(parts, "  ")
	}
	var left string
	switch {
	case m.textOpen:
		left = kst.Render("↑↓ pgup pgdn") + " " + lbl.Render("scroll") + "  " + render([]key.Binding{keys.Close})
	case m.modal:
		left = render([]key.Binding{keys.Close, keys.Open, keys.Copy, keys.Cmd, keys.Log})
	default:
		left = render(context(m.view))
	}
	if m.status != "" {
		left = t.fg(t.warn).Render(m.status)
	}
	right := kst.Render("tab") + " " + lbl.Render("views") + "  " + render([]key.Binding{keys.Help, keys.Quit})
	if lipgloss.Width(left)+lipgloss.Width(right)+4 > w {
		right = render([]key.Binding{keys.Help})
	}
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
	return pad(" "+left+strings.Repeat(" ", gap)+right, w)
}

func (m Model) modalBox(w, h int) string {
	t := m.th
	r, ok := m.selected()
	if !ok {
		return ""
	}
	bw := min(w-6, 100)
	inner := bw - 4
	lines := []string{t.fg(t.accent).Bold(true).Render(r.title), ""}
	kw := 0
	for _, kv := range r.detail {
		kw = max(kw, len(kv[0]))
	}
	for _, kv := range r.detail {
		k := t.fg(t.muted).Render(fmt.Sprintf("%-*s  ", kw, kv[0]))
		v := t.fg(t.text).Render(kv[1])
		if kv[0] == "url" || kv[0] == "pr" || kv[0] == "ref" {
			v = t.fg(t.accent).Underline(!t.plain).Render(kv[1])
		}
		lines = append(lines, ansi.Truncate(k+v, inner, "…"))
	}
	if len(lines) > h-2 {
		lines = append(lines[:max(h-3, 1)], t.fg(t.muted).Render("…"))
	}
	st := m.panelStyle(t.accent).Width(bw)
	if !t.plain {
		st = st.Background(t.surface)
	}
	return st.Render(strings.Join(lines, "\n"))
}

func (m Model) textBox() string {
	t := m.th
	title := t.fg(t.accent).Bold(true).Render(m.textTitle)
	pct := t.fg(t.muted).Render(fmt.Sprintf("%3.0f%%", m.text.ScrollPercent()*100))
	st := m.panelStyle(t.accent)
	if !t.plain {
		st = st.Background(t.surface)
	}
	return st.Render(title + "\n\n" + m.text.View() + "\n" + pct)
}

func (m Model) helpBox(w, h int) string {
	t := m.th
	k, l := t.fg(t.accent), t.fg(t.muted)
	lines := []string{t.fg(t.accent).Bold(true).Render("smithy tui " + t.g.dot + " help"), ""}
	for _, b := range []key.Binding{keys.Next, keys.Prev, keys.Jump, keys.Up, keys.Left, keys.Enter, keys.Close, keys.Open,
		keys.Log, keys.Copy, keys.Cmd, keys.Sort, keys.Type, keys.Pause, keys.Filter, keys.Refresh, keys.Help, keys.Quit} {
		lines = append(lines, k.Render(fmt.Sprintf("%-9s", b.Help().Key))+l.Render(b.Help().Desc))
	}
	lines = append(lines, k.Render(fmt.Sprintf("%-9s", "mouse"))+l.Render("click a tab or a row, double-click opens, wheel moves"), "")
	for v := view(0); v < nViews; v++ {
		lines = append(lines, t.fg(t.text).Bold(!t.plain).Render(fmt.Sprintf("%d %-11s", v+1, viewNames[v]))+l.Render(meaning[v]))
	}
	if len(lines) > h-4 {
		lines = lines[:max(h-4, 1)]
	}
	bw := min(w-6, 110)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], bw-4, "…")
	}
	st := m.panelStyle(t.accent).Width(bw)
	if !t.plain {
		st = st.Background(t.surface)
	}
	return st.Render(strings.Join(lines, "\n"))
}
