package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/charmbracelet/x/ansi"
)

func cardPath(batch, id string) string { return filepath.Join(cards.Home(), batch, id+".json") }

// View renders the dashboard.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.WindowTitle = "smithy · " + aliveWord(m.snap.Watcher)
	return v
}

func (m Model) render() string {
	w, h := max(m.width, 40), max(m.height, 12)
	head := m.header(w, h)
	foot := m.footer(w)
	bodyH := max(h-lipgloss.Height(head)-lipgloss.Height(foot), 3)
	var body string
	if w >= 110 && bodyH >= 14 {
		body = m.wide(w, bodyH)
	} else {
		body = m.stacked(w, bodyH)
	}
	screen := lipgloss.JoinVertical(lipgloss.Left, head, body, foot)
	if !m.modal {
		return screen
	}
	box := m.modalBox(w, bodyH)
	x := max((w-lipgloss.Width(box))/2, 0)
	y := lipgloss.Height(head) + max((bodyH-lipgloss.Height(box))/2, 0)
	return lipgloss.NewCompositor(lipgloss.NewLayer(screen), lipgloss.NewLayer(box).X(x).Y(y).Z(1)).Render()
}

// pad fits a styled line to exactly w cells.
func pad(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

func (m Model) statusDot() string {
	t := m.th
	wt := m.snap.Watcher
	switch {
	case wt.Alive:
		return t.fg(t.ok).Render(t.g.on + " alive")
	case wt.Polled:
		return t.fg(t.err).Bold(true).Render(t.g.on + " stale")
	}
	return t.fg(t.muted).Render(t.g.off + " never polled")
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

// wordWidth is the width of the wordmark the header draws at the current size.
func (m Model) wordWidth() int {
	if m.width < 100 {
		return len([]rune(wordB[0]))
	}
	return len([]rune(wordA[0]))
}

// counts is how many batch items run and how many are blocked.
func (m Model) counts() (running, blocked int) {
	for _, it := range m.items() {
		switch it.item.Stage {
		case "working", "starting", "waiting", "verify":
			running++
		case "stuck", "decide":
			blocked++
		}
	}
	return
}

func peak(v []int) int {
	hi := 0
	for _, x := range v {
		hi = max(hi, x)
	}
	return hi
}

// header is the wordmark with its hatch field and the watcher strip: Concept A (6 rows) from
// 100 columns, Concept B (3 rows of word, 2 of status) below. The wordmark is never cut: it switches.
func (m Model) header(w, h int) string {
	t := m.th
	wt := m.snap.Watcher
	mut := t.fg(t.muted)
	val := t.fg(t.text).Bold(!t.plain)
	running, blocked := m.counts()
	label := func(s string) string {
		if s == "watcher" && m.focus == pWatcher {
			return t.fg(t.accent).Bold(true).Render(fmt.Sprintf("%-8s", s))
		}
		return mut.Render(fmt.Sprintf("%-8s", s))
	}
	// gut is the cell before the watcher row: the focus marker when the Watcher has focus.
	gut := " "
	if m.focus == pWatcher {
		gut = t.fg(t.accent).Bold(true).Render(t.g.sel)
	}
	poll := "never polled"
	if wt.Polled {
		poll = "polled " + ago(m.snap.At.Sub(wt.LastPoll))
	}
	watcher := m.statusDot() + mut.Render(" "+t.g.dot+" ") + val.Render(or(strings.Join(wt.Sources, ", "))) + mut.Render(" "+t.g.dot+" "+poll)
	today := fmt.Sprintf("%d/%d", wt.Today, wt.Cap)
	spark := t.fg(t.accent).Render(sparkline(wt.Hourly, 24, t.g.ticks)) + mut.Render(fmt.Sprintf("  peak %d/h", peak(wt.Hourly)))
	blk := mut.Render(fmt.Sprintf("%d blocked", blocked))
	if blocked > 0 {
		blk = t.fg(t.err).Bold(true).Render(fmt.Sprintf("%d blocked", blocked))
	}
	queue := val.Render(fmt.Sprintf("%d queued", len(m.snap.Queue))) + mut.Render(" "+t.g.dot+" ") +
		val.Render(fmt.Sprintf("%d running", running)) + mut.Render(" "+t.g.dot+" ") + blk
	clock := mut.Render(m.snap.At.Local().Format("Mon 02 Jan 15:04:05"))
	meta := mut.Render("forge " + t.g.dot + " " + Version)

	if !t.g.unicode {
		word := t.fg(t.accent).Bold(true).Render("SMITHY") + "  " + meta
		return strings.Join([]string{
			pad(" "+word+"   "+clock, w),
			pad(gut+label("watcher")+watcher, w),
			pad(" "+label("today")+m.capBar(10)+"  "+val.Render(today)+"   "+queue, w),
		}, "\n")
	}
	if w < 100 {
		word := m.wordmark(wordB)
		field := m.field(3)
		side := []string{field[0], field[1] + " " + mut.Render(Version+" "+t.g.dot+" ") + val.Render(fmt.Sprintf("%d running", running)), field[2]}
		var lines []string
		for i := range word {
			l := " " + word[i] + "  " + side[i]
			if i == 0 {
				gap := max(w-lipgloss.Width(l)-lipgloss.Width(clock)-1, 1)
				l += strings.Repeat(" ", gap) + clock
			}
			lines = append(lines, pad(l, w))
		}
		lines = append(lines,
			pad(gut+label("watcher")+watcher, w),
			pad(" "+label("today")+m.capBar(10)+"  "+val.Render(today)+mut.Render("   ")+queue, w))
		return strings.Join(lines, "\n")
	}
	word := m.wordmark(wordA)
	field := m.field(3)
	left := []string{field[0], field[1], field[2], "", meta, ""}
	status := []string{
		"",
		label("watcher") + watcher,
		label("today") + m.capBar(14) + "  " + val.Render(today),
		label("24h") + spark,
		label("queue") + queue,
		mut.Render(strings.Repeat(" ", 8)) + clock,
	}
	showField := w >= 1+len([]rune(wordA[0]))+2+fieldWidth+3+44
	var lines []string
	for i := range word {
		l := " " + word[i]
		if showField {
			l += "  " + pad(left[i], fieldWidth)
		}
		if i == 0 && !showField {
			status[0] = meta
		}
		if i == 1 {
			l += "  " + gut + status[i]
		} else {
			l += "   " + status[i]
		}
		lines = append(lines, pad(l, w))
	}
	return strings.Join(lines, "\n")
}

func maxInt(v []int) int {
	hi := 0
	for _, x := range v {
		hi = max(hi, x)
	}
	return hi
}

func (m Model) footer(w int) string {
	t := m.th
	hm := help.New()
	hm.Styles = help.DefaultStyles(m.dark)
	if t.plain {
		hm.Styles = help.Styles{}
	}
	hm.SetWidth(w - 2)
	var line string
	if m.showHelp {
		line = hm.FullHelpView(keys.FullHelp())
	} else {
		line = hm.ShortHelpView(keys.ShortHelp())
	}
	if m.status != "" && !m.showHelp {
		line = t.fg(t.warn).Render(m.status)
	}
	var out []string
	for _, l := range strings.Split(line, "\n") {
		out = append(out, pad(" "+l, w))
	}
	return strings.Join(out, "\n")
}

// wide is three columns: Queue over Tasks, Batches, Decisions.
func (m Model) wide(w, h int) string {
	lw := w * 27 / 100
	rw := w * 25 / 100
	cw := w - lw - rw
	qh := h / 2
	left := lipgloss.JoinVertical(lipgloss.Left, m.box(pQueue, lw, qh), m.box(pTasks, lw, h-qh))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, m.box(pBatches, cw, h), m.box(pDecisions, rw, h))
}

// stacked puts the panels one under another: the focused one gets the room, the rest a summary line.
func (m Model) stacked(w, h int) string {
	order := []panel{pQueue, pBatches, pTasks, pDecisions}
	focus := m.focus
	if focus == pWatcher {
		focus = pBatches
	}
	var parts []string
	big := h - 3*(len(order)-1)
	for _, p := range order {
		if p == focus && big >= 3 {
			parts = append(parts, m.box(p, w, big))
		} else {
			parts = append(parts, m.box(p, w, 3))
		}
	}
	return strings.Join(strings.Split(lipgloss.JoinVertical(lipgloss.Left, parts...), "\n")[:h], "\n")
}

// box draws a rounded panel with its title in the top border and its rows inside.
func (m Model) box(p panel, w, h int) string {
	t := m.th
	focused := p == m.focus
	bc := t.border
	if focused {
		bc = t.accent
	} else if p == pDecisions && len(m.snap.Decisions) > 0 {
		bc = t.warn
	}
	bs := t.fg(bc)
	tl, tr, bl, br, hz, vt := "╭", "╮", "╰", "╯", "─", "│"
	if !t.g.unicode {
		tl, tr, bl, br, hz, vt = "+", "+", "+", "+", "-", "|"
	}
	title := t.fg(t.muted).Render(titles[p])
	if focused {
		title = t.fg(t.accent).Bold(true).Render(titles[p])
	}
	if p == pDecisions && len(m.snap.Decisions) > 0 {
		title = t.fg(t.warn).Bold(true).Render(titles[p])
	}
	head := bs.Render(tl+hz+" ") + title + t.fg(t.muted).Render(" "+t.g.dot+" "+m.count(p)) + " "
	head = ansi.Truncate(head, w-1, "")
	head += bs.Render(strings.Repeat(hz, max(w-1-lipgloss.Width(head), 0)) + tr)
	inner := w - 4
	content, at := m.lines(p, inner)
	avail := h - 2
	start := 0
	if focused && at >= avail {
		start = at - avail + 1
	}
	out := []string{head}
	for i := 0; i < avail; i++ {
		l := ""
		if j := start + i; j < len(content) {
			l = content[j]
		}
		out = append(out, bs.Render(vt)+" "+pad(l, inner)+" "+bs.Render(vt))
	}
	if avail >= 1 && h >= 3 {
		out = append(out, bs.Render(bl+strings.Repeat(hz, max(w-2, 0))+br))
	}
	return strings.Join(out[:h], "\n")
}

func (m Model) count(p panel) string {
	switch p {
	case pWatcher:
		return aliveWord(m.snap.Watcher)
	case pQueue:
		return fmt.Sprintf("%d queued", len(m.snap.Queue))
	case pBatches:
		return plural(len(m.snap.Batches), "batch", "batches") + " " + m.th.g.dot + " " + plural(len(m.items()), "item", "items")
	case pTasks:
		return plural(len(m.snap.Tasks), "list", "lists")
	case pDecisions:
		return fmt.Sprintf("%d pending", len(m.snap.Decisions))
	}
	return ""
}

// empty is one sentence and the command that changes it.
func (m Model) empty(say, cmd string) ([]string, int) {
	t := m.th
	return []string{"", t.fg(t.muted).Italic(true).Render(say), t.fg(t.accent).Render("$ " + cmd)}, -1
}

func (m Model) stage(s string) (string, color.Color) {
	t := m.th
	switch s {
	case "merged", "reported", "verify":
		return t.g.on, t.ok
	case "stuck", "decide":
		return t.g.fail, t.err
	case "working", "starting", "waiting":
		return t.g.half, t.warn
	}
	return t.g.off, t.muted
}

func (m Model) ci(s string) string {
	t := m.th
	switch s {
	case "green", "pass", "success":
		return t.fg(t.ok).Render(t.g.pass + " " + s)
	case "red", "fail", "failure":
		return t.fg(t.err).Render(t.g.fail + " " + s)
	}
	return t.fg(t.muted).Render(t.g.dot + " " + s)
}

// selRow renders the selected row: marker plus reverse video, so selection never relies on color.
func (m Model) selRow(plain string, w int) string {
	t := m.th
	s := t.g.sel + " " + plain
	s = ansi.Truncate(s, w, "…")
	s += strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
	st := lipgloss.NewStyle().Reverse(true).Bold(true)
	if !t.plain {
		st = st.Foreground(t.accent)
	}
	return st.Render(s)
}

// lines renders a panel's rows cut to width and returns the selected row's line index (-1 if none).
func (m Model) lines(p panel, w int) ([]string, int) {
	t := m.th
	mut := t.fg(t.muted)
	txt := t.fg(t.text)
	focused := p == m.focus
	sel := m.sel[p]
	var rows []string
	at := -1
	add := func(i int, styled string) {
		if focused && i == sel {
			at = len(rows)
			rows = append(rows, m.selRow(ansi.Strip(styled), w))
			return
		}
		rows = append(rows, "  "+ansi.Truncate(styled, w-2, "…"))
	}
	switch p {
	case pQueue:
		if len(m.snap.Queue) == 0 {
			return m.empty("Nothing queued.", "smithy auto poll")
		}
		for i, q := range m.snap.Queue {
			add(i, txt.Render(q.Title))
			rows = append(rows, "    "+mut.Render(ansi.Truncate(q.Source+" "+q.ID+" "+t.g.dot+" "+or(q.Model)+" "+t.g.dot+" "+or(q.Speed), w-4, "…")))
		}
	case pBatches:
		if len(m.snap.Batches) == 0 {
			return m.empty("No warden batches running.", "smithy warden status")
		}
		i := 0
		for _, b := range m.snap.Batches {
			done := 0
			var dots strings.Builder
			for _, it := range b.Items {
				g, c := m.stage(it.Stage)
				dots.WriteString(t.fg(c).Render(g))
				if it.Stage == "merged" || it.Stage == "reported" {
					done++
				}
			}
			rows = append(rows, t.fg(t.text).Bold(true).Render(b.Name)+mut.Render(fmt.Sprintf("  %d/%d done  ", done, len(b.Items)))+dots.String())
			for j, it := range b.Items {
				branch := t.g.tee
				if j == len(b.Items)-1 {
					branch = t.g.elbow
				}
				g, c := m.stage(it.Stage)
				pr := mut.Render(shortPR(it.PR))
				idW := max(min(w-46, 22), 8)
				line := mut.Render(branch+" ") + t.fg(c).Render(g+" ") + txt.Render(fmt.Sprintf("%-*s", idW, ansi.Truncate(it.ID, idW, "…"))) +
					t.fg(c).Render(fmt.Sprintf(" %-8s", it.Stage)) + mut.Render(fmt.Sprintf(" %-9s %-6s ", ansi.Truncate(it.State, 9, "…"), it.Model)) +
					pad(m.ci(it.CI), 8) + " " + pr
				if focused && i == sel {
					at = len(rows)
					rows = append(rows, m.selRow(ansi.Strip(line), w))
				} else {
					rows = append(rows, "  "+ansi.Truncate(line, w-2, "…"))
				}
				i++
			}
		}
	case pTasks:
		if len(m.snap.Tasks) == 0 {
			return m.empty("No task lists for this repo.", `smithy tasks add <dir> "<title>"`)
		}
		for i, tl := range m.snap.Tasks {
			var parts []string
			for _, s := range statuses(tl.Counts) {
				c := t.muted
				switch s {
				case "done":
					c = t.ok
				case "doing":
					c = t.warn
				case "blocked":
					c = t.err
				}
				parts = append(parts, t.fg(c).Render(fmt.Sprintf("%d %s", tl.Counts[s], s)))
			}
			doneN := tl.Counts["done"]
			meter := ""
			if tl.Total > 0 {
				n := doneN * 8 / tl.Total
				meter = t.fg(t.ok).Render(strings.Repeat(t.g.shades[3], n)) + mut.Render(strings.Repeat(t.g.shades[0], 8-n)) + " "
			}
			add(i, meter+txt.Render(filepath.Base(tl.Dir)))
			rows = append(rows, "    "+ansi.Truncate(strings.Join(parts, mut.Render(" "+t.g.dot+" ")), w-4, "…"))
		}
	case pDecisions:
		if len(m.snap.Decisions) == 0 {
			return []string{"", t.fg(t.ok).Render(t.g.pass + " Nothing waiting on you."), mut.Render("Decisions agents take alone land here.")}, -1
		}
		for i, d := range m.snap.Decisions {
			f := strings.Split(d, " · ")
			q, meta := d, ""
			if len(f) >= 3 {
				q, meta = f[2], f[0]+" "+t.g.dot+" "+f[1]
			}
			add(i, t.fg(t.warn).Render("? ")+txt.Render(q))
			if meta != "" {
				rows = append(rows, "    "+mut.Render(ansi.Truncate(meta, w-4, "…")))
			}
		}
	}
	return rows, at
}

func shortPR(pr string) string {
	if i := strings.LastIndex(pr, "/pull/"); i >= 0 {
		return "#" + pr[i+len("/pull/"):]
	}
	return pr
}

func (m Model) modalBox(w, h int) string {
	t := m.th
	d := m.detail()
	bw := min(w-6, 84)
	inner := bw - 4
	lines := []string{t.fg(t.accent).Bold(true).Render(titles[m.focus]+" "+t.g.dot+" details") + "  " + t.fg(t.muted).Render("esc close · o open"), ""}
	kw := 0
	for _, kv := range d {
		kw = max(kw, len(kv[0]))
	}
	for _, kv := range d {
		k := t.fg(t.muted).Render(fmt.Sprintf("%-*s  ", kw, kv[0]))
		v := t.fg(t.text).Render(kv[1])
		if kv[0] == "url" || kv[0] == "pr" || kv[0] == "ref" {
			v = t.fg(t.accent).Underline(true).Render(kv[1])
		}
		lines = append(lines, ansi.Truncate(k+v, inner, "…"))
	}
	if len(lines) > h-2 {
		lines = lines[:max(h-2, 1)]
	}
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(bw)
	if !t.plain {
		st = st.BorderForeground(t.accent).Background(t.surface)
	}
	if !t.g.unicode {
		st = st.Border(lipgloss.NormalBorder())
	}
	return st.Render(strings.Join(lines, "\n"))
}
