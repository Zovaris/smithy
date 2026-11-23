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
	bar := progress.New(progress.WithColors(t.ember[2], t.ember[4], t.ember[6]), progress.WithoutPercentage(),
		progress.WithFillCharacters('━', '─'), progress.WithWidth(w))
	return bar.ViewAs(m.capPos)
}

// header is the animated art with the watcher's vitals, or one line when space is short.
func (m Model) header(w, h int) string {
	t := m.th
	wt := m.snap.Watcher
	mut := t.fg(t.muted)
	txt := t.fg(t.text)
	clock := mut.Render(m.snap.At.Local().Format("Mon 02 Jan  15:04:05"))
	ruleC := t.border
	if m.focus == pWatcher {
		ruleC = t.accent
	}
	rule := func() string {
		label := " Watcher "
		if m.focus == pWatcher {
			label = " " + t.g.sel + " Watcher "
		}
		return t.fg(ruleC).Render(strings.Repeat(t.g.bar, 2)+label+strings.Repeat(t.g.bar, max(w-len([]rune(label))-2, 0))) + ""
	}
	today := fmt.Sprintf("%d/%d today", wt.Today, wt.Cap)
	if w < 100 || h < 28 {
		logo := t.fg(t.accent).Bold(true).Render("smithy") + " " + mut.Render(Version)
		left := logo + "  " + m.statusDot() + mut.Render("  "+t.g.dot+" ") + txt.Render(today)
		gap := max(w-lipgloss.Width(left)-lipgloss.Width(clock)-2, 1)
		return pad(" "+left+strings.Repeat(" ", gap)+clock, w) + "\n" + pad(rule(), w)
	}
	art := strings.Split(m.art(), "\n")
	sw := w - artWidth() - 4
	barW := max(min(sw-18, 40), 8)
	sources := or(strings.Join(wt.Sources, ", "))
	poll := "never polled"
	if wt.Polled {
		poll = "polled " + ago(m.snap.At.Sub(wt.LastPoll))
	}
	chart := braille(wt.Daily, max(wt.Cap, maxInt(wt.Daily)), wt.Cap, min(7, max(sw-34, 2)), 1)[0]
	if !t.g.unicode {
		chart = sparkline(wt.Daily, 14, t.g.ticks)
	}
	stats := []string{
		m.statusDot() + mut.Render("  "+sources+"  "+t.g.dot+"  "+poll),
		txt.Render(fmt.Sprintf("%-10s", today)) + " " + m.capBar(barW),
		mut.Render("hours ") + txt.Render(hoursWord(wt)) + mut.Render("  "+t.g.dot+"  every ") + txt.Render(fmt.Sprintf("%ds", wt.Every)),
		mut.Render("24h ") + t.fg(t.accent).Render(sparkline(wt.Hourly, 24, t.g.ticks)) + mut.Render("  14d ") + t.fg(t.warn).Render(chart),
		t.fg(t.accent).Bold(true).Render("smithy") + " " + mut.Render(Version) + "   " + clock,
	}
	var lines []string
	for i := range art {
		lines = append(lines, pad(" "+art[i]+"   "+stats[i], w))
	}
	return strings.Join(append(lines, pad(rule(), w)), "\n")
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
