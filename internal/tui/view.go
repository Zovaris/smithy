package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/charmbracelet/x/ansi"
)

func cardPath(batch, id string) string { return filepath.Join(cards.Home(), batch, id+".json") }

func homeDir() string { h, _ := os.UserHomeDir(); return h }

type rect struct{ x, y, w, h int }

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// View renders the dashboard.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "smithy · " + m.watchWord()
	return v
}

func (m Model) size() (int, int) { return max(m.width, 40), max(m.height, 12) }

// layout is where everything goes: header rows, the tab bar row, panel rects and the footer row.
func (m Model) layout() (head []string, tabY int, rects map[panel]rect, footY int) {
	w, h := m.size()
	head = m.header(w)
	tabY = len(head)
	footY = h - 1
	top := tabY + 1
	bodyH := max(footY-top, 3)
	rects = map[panel]rect{}
	if w >= 110 && bodyH >= 14 {
		lw, rw := w*29/100, w*30/100
		cw := w - lw - rw
		lh, ch, rh := bodyH/2, bodyH*3/5, bodyH/2
		rects[pIncoming] = rect{0, top, lw, lh}
		rects[pPlans] = rect{0, top + lh, lw, bodyH - lh}
		rects[pAgents] = rect{lw, top, cw, ch}
		rects[pActivity] = rect{lw, top + ch, cw, bodyH - ch}
		rects[pNeeds] = rect{lw + cw, top, rw, rh}
		rects[pWorktrees] = rect{lw + cw, top + rh, rw, bodyH - rh}
	} else {
		rects[m.focus] = rect{0, top, w, bodyH}
	}
	return
}

func (m Model) render() string {
	w, h := m.size()
	head, tabY, rects, footY := m.layout()
	layers := []*lipgloss.Layer{
		lipgloss.NewLayer(strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", w)+"\n", h), "\n")),
		lipgloss.NewLayer(strings.Join(head, "\n")),
		lipgloss.NewLayer(m.tabbar(w)).Y(tabY),
		lipgloss.NewLayer(m.footer(w)).Y(footY),
	}
	for p, r := range rects {
		layers = append(layers, lipgloss.NewLayer(m.box(p, r)).X(r.x).Y(r.y))
	}
	if m.toast != "" {
		t := m.th
		box := m.panelStyle(t.accent).Render(t.fg(t.text).Render(m.toast))
		layers = append(layers, lipgloss.NewLayer(box).X(max(w-lipgloss.Width(box)-2, 0)).Y(max(footY-lipgloss.Height(box), 0)).Z(2))
	}
	var over string
	switch {
	case m.logOpen:
		over = m.logBox()
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

func (m Model) panelStyle(border interface{}) lipgloss.Style {
	t := m.th
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if !t.g.unicode {
		st = st.Border(lipgloss.NormalBorder())
	}
	if !t.plain {
		if c, ok := border.(interface{ RGBA() (r, g, b, a uint32) }); ok {
			st = st.BorderForeground(c)
		}
	}
	return st
}

// pad fits a styled line to exactly w cells.
func pad(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

func (m Model) panelAt(x, y int) (panel, bool) {
	_, _, rects, _ := m.layout()
	for p, r := range rects {
		if r.has(x, y) {
			return p, true
		}
	}
	return 0, false
}

func (m Model) rowAt(p panel, y int) (int, bool) {
	_, _, rects, _ := m.layout()
	r := rects[p]
	_, owner := m.panelBody(p, r.w-4, r.h-2)
	i := y - r.y - 1
	if i < 0 || i >= len(owner) || owner[i] < 0 {
		return 0, false
	}
	return owner[i], true
}

func (m Model) tabAt(x, y int) (panel, bool) {
	_, tabY, _, _ := m.layout()
	if y != tabY {
		return 0, false
	}
	pos := 1
	for p := panel(0); p < nPanels; p++ {
		wd := lipgloss.Width(m.tabLabel(p))
		if x >= pos && x < pos+wd {
			return p, true
		}
		pos += wd + 1
	}
	return 0, false
}

func (m Model) tabLabel(p panel) string {
	n := len(m.entries(p))
	badge := ""
	if n > 0 {
		badge = fmt.Sprintf(" (%d)", n)
	}
	if m.compactTabs() && p != m.focus {
		return fmt.Sprintf(" %d%s ", p+1, badge)
	}
	return fmt.Sprintf(" %d %s%s ", p+1, tabs[p], badge)
}

// compactTabs is true when the full tab names don't fit: only the focused one keeps its name.
func (m Model) compactTabs() bool {
	w, _ := m.size()
	n := 1
	for p := panel(0); p < nPanels; p++ {
		n += len([]rune(tabs[p])) + 10
	}
	return n > w
}

// tabbar is the thin rule under the header carrying the numbered panel tabs.
func (m Model) tabbar(w int) string {
	t := m.th
	hz := "─"
	if !t.g.unicode {
		hz = "-"
	}
	var b strings.Builder
	b.WriteString(t.fg(t.border).Render(hz))
	for p := panel(0); p < nPanels; p++ {
		l := m.tabLabel(p)
		switch {
		case p == m.focus:
			st := lipgloss.NewStyle().Bold(true).Reverse(true)
			if !t.plain {
				st = lipgloss.NewStyle().Bold(true).Foreground(t.surface).Background(t.accent)
			}
			b.WriteString(st.Render(l))
		case p == pNeeds && len(m.entries(pNeeds)) > 0:
			b.WriteString(t.fg(t.warn).Bold(true).Render(l))
		default:
			b.WriteString(t.fg(t.muted).Render(l))
		}
		b.WriteString(t.fg(t.border).Render(hz))
	}
	return b.String() + t.fg(t.border).Render(strings.Repeat(hz, max(w-lipgloss.Width(b.String()), 0)))
}

func (m Model) counts() (working, waiting, stuck, ready, ciRunning int) {
	for _, b := range m.snap.Batches {
		for _, it := range b.Items {
			switch it.Stage {
			case "working", "starting":
				working++
			case "waiting":
				waiting++
			case "stuck", "decide":
				stuck++
			case "verify":
				switch it.CI {
				case "pending", "running", "queued", "in_progress":
					ciRunning++
				case "red", "fail", "failure":
				default:
					ready++
				}
			}
		}
	}
	return
}

func (m Model) watchWord() string {
	wt := m.snap.Watcher
	switch {
	case wt.Paused:
		return "paused"
	case wt.Alive:
		return "watching"
	case wt.Polled:
		return "stale"
	}
	return "not running"
}

// watcherLine is the watcher's state, with the fix when it is not running.
func (m Model) watcherLine() string {
	t := m.th
	wt := m.snap.Watcher
	mut, val := t.fg(t.muted), t.fg(t.text)
	var src []string
	for _, s := range wt.Sources {
		if s == "linear" && wt.Team != "" {
			s += " (" + wt.Team + ")"
		}
		src = append(src, s)
	}
	sources := or(strings.Join(src, ", "))
	switch {
	case wt.Paused:
		return t.fg(t.warn).Bold(true).Render("‖ paused") + mut.Render(" "+t.g.dot+" intake held "+t.g.dot+" p resumes")
	case wt.Alive:
		next := wt.LastPoll.Add(time.Duration(wt.Every) * time.Second).Sub(m.snap.At)
		return t.fg(t.ok).Render(t.g.on+" watching ") + val.Render(sources) + mut.Render(fmt.Sprintf(" %s next poll %ds", t.g.dot, max(int(next.Seconds()), 0)))
	case wt.Polled:
		return t.fg(t.err).Bold(true).Render(t.g.on+" stale") + mut.Render(" "+t.g.dot+" last poll "+ago(m.snap.At.Sub(wt.LastPoll))+" "+t.g.dot+" start: ") + val.Render("tend auto")
	}
	return t.fg(t.err).Render(t.g.off+" not running") + mut.Render(" "+t.g.dot+" "+sources+" "+t.g.dot+" start: ") + val.Render("tend auto")
}

func (m Model) capBar(w int) string {
	t := m.th
	if t.plain || !t.g.unicode {
		n := int(m.capPos*float64(w) + 0.5)
		return "[" + strings.Repeat("#", n) + strings.Repeat(".", max(w-n, 0)) + "]"
	}
	bar := progress.New(progress.WithColors(t.forge...), progress.WithoutPercentage(),
		progress.WithFillCharacters('█', '░'), progress.WithWidth(w))
	return bar.ViewAs(m.capPos)
}

func (m Model) statusRows() (watcher, today, agents, prs string) {
	t := m.th
	wt := m.snap.Watcher
	mut, val := t.fg(t.muted), t.fg(t.text)
	working, waiting, stuck, ready, ciRunning := m.counts()
	st := mut.Render(fmt.Sprintf("%d stuck", stuck))
	if stuck > 0 {
		st = t.fg(t.err).Bold(true).Render(fmt.Sprintf("%d stuck", stuck))
	}
	rd := mut.Render(fmt.Sprintf("%d ready for you", ready))
	if ready > 0 {
		rd = t.fg(t.ok).Bold(true).Render(fmt.Sprintf("%d ready for you", ready))
	}
	dot := mut.Render(" " + t.g.dot + " ")
	return m.watcherLine(),
		m.capBar(13) + "  " + val.Render(fmt.Sprintf("%d / %d", wt.Today, wt.Cap)) + mut.Render(" launched"),
		val.Render(fmt.Sprintf("%d working", working)) + dot + val.Render(fmt.Sprintf("%d waiting", waiting)) + dot + st,
		rd + dot + val.Render(fmt.Sprintf("%d CI running", ciRunning))
}

// header is the anvil, the name and the status: 7 rows from 110 columns, 6 from 80, 4 below.
func (m Model) header(w int) []string {
	t := m.th
	mut := t.fg(t.muted)
	label := func(s string) string { return mut.Render(fmt.Sprintf("%-9s", s)) }
	meta := mut.Render("autonomous forge " + t.g.dot + " " + Version + " " + t.g.dot + " " + or(m.snap.Repo))
	clock := mut.Render(m.snap.At.Local().Format("15:04:05"))
	watcher, today, agents, prs := m.statusRows()
	word := m.wordmark()
	if !t.g.unicode {
		word = []string{t.fg(t.accent).Bold(true).Render("SMITHY"), "", ""}
	}
	if w < 80 {
		return []string{pad(" "+word[0]+"  "+clock, w), pad(" "+word[1], w), pad(" "+word[2], w), pad(" "+watcher, w)}
	}
	if w < 110 {
		anvil := m.anvil(smallAnvil)
		name := []string{word[0], word[1], word[2], meta}
		var out []string
		for i := range anvil {
			l := " " + anvil[i] + "  " + name[i]
			if i == 0 {
				l += strings.Repeat(" ", max(w-lipgloss.Width(l)-lipgloss.Width(clock)-1, 1)) + clock
			}
			out = append(out, pad(l, w))
		}
		return append(out, pad(" "+watcher+mut.Render("   ")+today, w), pad(" "+agents+mut.Render("   ")+prs, w))
	}
	anvil := m.anvil(bigAnvil)
	name := []string{"", word[0], word[1], word[2], meta, "", ""}
	status := []string{"", label("watcher") + watcher, label("today") + today, label("agents") + agents, label("PRs") + prs, "", label("") + clock}
	sw := 0
	for _, s := range status {
		sw = max(sw, lipgloss.Width(s))
	}
	left := 1 + bigAnvil.w() + 3
	nameW := max(w-left-sw-5, 16)
	var out []string
	for i := range anvil {
		l := " " + anvil[i] + "   " + pad(name[i], nameW) + "    " + status[i]
		out = append(out, pad(l, w))
	}
	return out
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
	case m.logOpen:
		left = kst.Render("↑↓ pgup pgdn") + " " + lbl.Render("scroll") + "  " + render([]key.Binding{keys.Close})
	case m.modal:
		left = render([]key.Binding{keys.Close, keys.Open, keys.Copy, keys.Log})
	default:
		left = render(context(m.focus))
	}
	if f := m.filter[m.focus]; f != "" && !m.modal {
		left = t.fg(t.warn).Render("filter: "+f) + "  " + left
	}
	if m.status != "" {
		left = t.fg(t.warn).Render(m.status)
	}
	right := render([]key.Binding{keys.Jump, keys.Help, keys.Quit})
	if lipgloss.Width(left)+lipgloss.Width(right)+4 > w {
		right = render([]key.Binding{keys.Help})
	}
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(right)-2, 1)
	return pad(" "+left+strings.Repeat(" ", gap)+right, w)
}

// box draws a panel: rounded border, a numbered title with a count badge in the top border,
// one blank column inside each side, and its rows.
func (m Model) box(p panel, r rect) string {
	t := m.th
	focused := p == m.focus
	bc := t.border
	if focused {
		bc = t.accent
	}
	bs := t.fg(bc)
	tl, tr, bl, br, hz, vt := "╭", "╮", "╰", "╯", "─", "│"
	if !t.g.unicode {
		tl, tr, bl, br, hz, vt = "+", "+", "+", "+", "-", "|"
	}
	title := t.fg(t.text).Render(fmt.Sprintf("%d %s", p+1, titles[p]))
	if focused {
		st := lipgloss.NewStyle().Bold(true).Reverse(true)
		if !t.plain {
			st = lipgloss.NewStyle().Bold(true).Foreground(t.accent)
		}
		title = st.Render(fmt.Sprintf(" %d %s ", p+1, titles[p]))
	}
	n := len(m.visible(p))
	badge := t.fg(t.muted).Render(fmt.Sprintf(" %s %d ", t.g.dot, n))
	if p == pNeeds && n > 0 {
		badge = t.fg(t.warn).Bold(true).Render(fmt.Sprintf(" %s %d ", t.g.dot, n))
	}
	head := bs.Render(tl+hz+" ") + title + badge
	head = ansi.Truncate(head, r.w-1, "")
	head += bs.Render(strings.Repeat(hz, max(r.w-1-lipgloss.Width(head), 0)) + tr)
	inner := r.w - 4
	lines, _ := m.panelBody(p, inner, r.h-2)
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

// panelBody is the window of a panel's lines that fits h rows, with the entry each line belongs to.
func (m Model) panelBody(p panel, w, h int) ([]string, []int) {
	t := m.th
	es := m.visible(p)
	mut := t.fg(t.muted)
	if len(es) == 0 {
		if m.filter[p] != "" {
			return []string{"", mut.Italic(true).Render("Nothing matches “" + m.filter[p] + "”."), t.fg(t.accent).Render("esc") + mut.Render(" clears the filter")}, []int{-1, -1, -1}
		}
		lines := []string{"", mut.Italic(true).Render(meaning[p])}
		switch p {
		case pIncoming:
			lines = append(lines, t.fg(t.accent).Render("$ smithy auto poll"))
		case pAgents:
			lines = append(lines, t.fg(t.accent).Render("$ smithy warden status"))
		case pNeeds:
			lines = []string{"", t.fg(t.ok).Render(t.g.pass + " Nothing needs you."), mut.Italic(true).Render(meaning[p])}
		case pPlans:
			lines = append(lines, t.fg(t.accent).Render(`$ smithy tasks add <dir> "<title>"`))
		}
		own := make([]int, len(lines))
		for i := range own {
			own[i] = -1
		}
		return lines, own
	}
	var lines []string
	var own []int
	selLine := 0
	for i, e := range es {
		if e.head != "" {
			if len(lines) > 0 {
				lines, own = append(lines, ""), append(own, -1)
			}
			lines, own = append(lines, t.fg(t.text).Bold(!t.plain).Render(ansi.Truncate(e.head, w, "…"))), append(own, -1)
		}
		sel := p == m.focus && i == m.sel[p]
		if sel {
			selLine = len(lines)
			s := ansi.Truncate(t.g.sel+" "+e.tree+e.icon+" "+e.text, w, "…")
			s += strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
			st := lipgloss.NewStyle().Reverse(true).Bold(true)
			if !t.plain {
				st = lipgloss.NewStyle().Bold(true).Foreground(t.surface).Background(t.accent)
			}
			lines = append(lines, st.Render(s))
		} else {
			lines = append(lines, "  "+mut.Render(e.tree)+t.fg(e.color).Render(e.icon)+" "+t.fg(t.text).Render(ansi.Truncate(e.text, w-4-lipgloss.Width(e.tree), "…")))
		}
		own = append(own, i)
		if e.meta != "" {
			lines, own = append(lines, "    "+mut.Render(ansi.Truncate(e.meta, w-4, "…"))), append(own, i)
			if e.tree != "" {
				lines[len(lines)-1] = "  " + mut.Render(ansi.Truncate(e.meta, w-2, "…"))
			}
		}
	}
	start := 0
	if selLine+2 > h {
		start = min(selLine+2-h, max(len(lines)-h, 0))
	}
	end := min(start+h, len(lines))
	return lines[start:end], own[start:end]
}

func (m Model) modalBox(w, h int) string {
	t := m.th
	e, ok := m.selected()
	if !ok {
		return ""
	}
	bw := min(w-6, 100)
	inner := bw - 4
	lines := []string{t.fg(t.accent).Bold(true).Render(e.title), ""}
	kw := 0
	for _, kv := range e.detail {
		kw = max(kw, len(kv[0]))
	}
	for _, kv := range e.detail {
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

func (m Model) logBox() string {
	t := m.th
	title := t.fg(t.accent).Bold(true).Render("log ") + t.fg(t.muted).Render(home(m.logTitle))
	pct := t.fg(t.muted).Render(fmt.Sprintf("%3.0f%%", m.logView.ScrollPercent()*100))
	body := title + "\n\n" + m.logView.View() + "\n" + pct
	st := m.panelStyle(t.accent)
	if !t.plain {
		st = st.Background(t.surface)
	}
	return st.Render(body)
}

func (m Model) helpBox(w, h int) string {
	t := m.th
	k, l := t.fg(t.accent), t.fg(t.muted)
	lines := []string{t.fg(t.accent).Bold(true).Render("smithy tui " + t.g.dot + " help"), ""}
	for _, b := range []key.Binding{keys.Jump, keys.Next, keys.Prev, keys.Up, keys.Down, keys.Enter, keys.Close,
		keys.Open, keys.Log, keys.Copy, keys.Pause, keys.Filter, keys.Refresh, keys.Help, keys.Quit} {
		lines = append(lines, k.Render(fmt.Sprintf("%-8s", b.Help().Key))+l.Render(b.Help().Desc))
	}
	lines = append(lines, k.Render(fmt.Sprintf("%-8s", "mouse"))+l.Render("click selects, double-click opens, wheel scrolls"), "")
	for p := panel(0); p < nPanels; p++ {
		lines = append(lines, t.fg(t.text).Bold(true).Render(fmt.Sprintf("%d %-15s", p+1, titles[p]))+l.Render(meaning[p]))
	}
	if len(lines) > h-4 {
		lines = lines[:max(h-4, 1)]
	}
	bw := min(w-6, 96)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], bw-4, "…")
	}
	st := m.panelStyle(t.accent).Width(bw)
	if !t.plain {
		st = st.Background(t.surface)
	}
	return st.Render(strings.Join(lines, "\n"))
}
