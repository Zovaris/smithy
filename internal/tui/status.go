package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
)

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
	serve := ""
	switch sv := wt.Serve; sv.State {
	case "running":
		serve = mut.Render(fmt.Sprintf(" %s serve pid %d %s", t.g.dot, sv.PID, sv.Agent))
	case "stale":
		serve = t.fg(t.warn).Render(" " + t.g.dot + " serve stale")
	}
	start := "tend auto"
	if wt.Serve.State == "" || wt.Serve.State == "not running" {
		start = "smithy auto serve"
	}
	switch {
	case wt.Paused:
		return t.fg(t.warn).Bold(true).Render("‖ paused") + mut.Render(" "+t.g.dot+" intake held "+t.g.dot+" p resumes")
	case wt.Alive:
		next := wt.LastPoll.Add(time.Duration(wt.Every) * time.Second).Sub(m.snap.At)
		return t.fg(t.ok).Render(t.g.on+" watching ") + val.Render(sources) + mut.Render(fmt.Sprintf(" %s next poll %ds", t.g.dot, max(int(next.Seconds()), 0))) + serve
	case wt.Polled:
		return t.fg(t.err).Bold(true).Render(t.g.on+" stale") + mut.Render(" "+t.g.dot+" last poll "+ago(m.snap.At.Sub(wt.LastPoll))+" "+t.g.dot+" start: ") + val.Render(start) + serve
	}
	return t.fg(t.err).Render(t.g.off+" not running") + mut.Render(" "+t.g.dot+" "+sources+" "+t.g.dot+" start: ") + val.Render(start) + serve
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
	if m.snap.Demo {
		pill := lipgloss.NewStyle().Bold(true).Reverse(true)
		if !t.plain {
			pill = lipgloss.NewStyle().Bold(true).Foreground(t.surface).Background(t.warn)
		}
		meta = pill.Render(" DEMO ") + " " + meta
	}
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
