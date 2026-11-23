package tui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

var cardTitles = []string{"Throughput", "Agents now", "Daily cap", "CI health", "Lead time", "Needs you"}

// overview is the dashboard: six equal cards.
func (m Model) overview() []*lipgloss.Layer {
	t := m.th
	var out []*lipgloss.Layer
	for i, r := range m.cardRects() {
		w, h := r.w-4, r.h-2
		var lines []string
		badge := ""
		var border color.Color
		switch i {
		case 0:
			lines, badge = m.cardThroughput(w, h)
		case 1:
			lines, badge = m.cardAgents(w, h)
		case 2:
			lines, badge = m.cardCap(w, h)
		case 3:
			lines, badge = m.cardCI(w, h)
		case 4:
			lines, badge = m.cardLead(w, h)
		case 5:
			lines, badge = m.cardNeeds(w, h)
			if len(m.rows(vNeeds)) > 0 {
				border = t.warn
			}
		}
		out = append(out, layer(m.box(cardTitles[i], badge, m.sel[vOverview] == i, border, r, lines), r))
	}
	return out
}

func (m Model) cardThroughput(w, h int) ([]string, string) {
	t := m.th
	a, b := throughput(m.snap.Events, m.snap.At)
	sum := func(v []int) int {
		n := 0
		for _, x := range v {
			n += x
		}
		return n
	}
	mut := t.fg(t.muted)
	legend := t.fg(t.accent).Render(t.g.bar+" launched "+fmt.Sprint(sum(a))) + "   " + t.fg(t.ok).Render(t.g.bar+" ready or merged "+fmt.Sprint(sum(b)))
	if sum(a)+sum(b) == 0 {
		return []string{legend, "", mut.Italic(true).Render("No launches or PRs in the last 24 h.")}, "24 h"
	}
	ch := max(h-3, 1)
	lines := []string{legend}
	lines = append(lines, braille2(a, b, w, ch, t.accent, t.ok, t)...)
	axis := "-24h"
	axis += strings.Repeat(" ", max(w-len(axis)-3, 1)) + "now"
	lines = append(lines, mut.Render(axis))
	return lines, "per hour, 24 h"
}

func (m Model) cardAgents(w, h int) ([]string, string) {
	t := m.th
	counts := map[string]int{}
	total := 0
	var live []Item
	for _, it := range m.items() {
		st := stageOf(it)
		if st == "merged" {
			continue
		}
		counts[st]++
		total++
		if st != "queued" {
			live = append(live, it)
		}
	}
	order := []string{"queued", "building", "checking", "pr_open", "ready", "blocked"}
	if total == 0 {
		return []string{"", t.fg(t.muted).Italic(true).Render("No workers right now."), t.fg(t.accent).Render("$ smithy warden status")}, "0"
	}
	var bar strings.Builder
	used := 0
	for k, st := range order {
		n := counts[st] * w / total
		if k == len(order)-1 {
			n = w - used
		}
		if counts[st] > 0 && n == 0 {
			n = 1
		}
		n = min(n, w-used)
		used += n
		g := "█"
		if !t.g.unicode {
			g = string("qbcprx"[k])
		}
		bar.WriteString(t.fg(m.stageColor(st)).Render(strings.Repeat(g, n)))
	}
	lines := []string{bar.String(), ""}
	var leg []string
	for _, st := range order {
		leg = append(leg, t.fg(m.stageColor(st)).Render(t.g.on)+" "+t.fg(t.text).Render(fmt.Sprintf("%d", counts[st]))+" "+t.fg(t.muted).Render(st))
	}
	lines = append(lines, strings.Join(leg[:3], "   "), strings.Join(leg[3:], "   "), "")
	sort.SliceStable(live, func(i, j int) bool { return m.beatAge(live[i]) < m.beatAge(live[j]) })
	spin := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if !t.g.unicode {
		spin = []string{"|", "/", "-", "\\"}
	}
	for k, it := range live {
		if k >= 3 || len(lines) >= h {
			break
		}
		st := stageOf(it)
		g := spin[(m.ticks+k)%len(spin)]
		if st == "blocked" {
			g = t.g.fail
		} else if st == "ready" {
			g = t.g.pass
		}
		lines = append(lines, t.fg(m.stageColor(st)).Render(g)+" "+t.fg(t.text).Render(short(it.ID+" "+it.Title, w-22))+
			t.fg(t.muted).Render("  "+st+" "+t.g.dot+" "+m.beat(it)))
	}
	return lines, fmt.Sprintf("%d live", total)
}

func (m Model) cardCap(w, h int) ([]string, string) {
	t := m.th
	wt := m.snap.Watcher
	big := t.fg(t.text).Bold(!t.plain).Render(fmt.Sprintf("%d / %d", wt.Today, wt.Cap)) + t.fg(t.muted).Render(" launched today")
	lines := []string{big, m.capBar(w), ""}
	week := wt.Daily
	if len(week) > 7 {
		week = week[len(week)-7:]
	}
	var nums []string
	for _, n := range week {
		nums = append(nums, fmt.Sprint(n))
	}
	lines = append(lines, t.fg(t.muted).Render("7 days  ")+t.fg(t.accent).Render(sparkline(week, 7, t.g.ticks))+t.fg(t.muted).Render("  "+strings.Join(nums, " ")))
	hours := "any time"
	if wt.Hours != "" {
		hours = hoursWord(wt)
	}
	lines = append(lines, t.fg(t.muted).Render("hours   ")+t.fg(t.text).Render(hours))
	if wt.Paused {
		lines = append(lines, t.fg(t.warn).Bold(!t.plain).Render("intake paused · p resumes"))
	}
	pct := 0
	if wt.Cap > 0 {
		pct = wt.Today * 100 / wt.Cap
	}
	return lines, fmt.Sprintf("%d%%", pct)
}

func hoursWord(w Watcher) string {
	if w.Hours == "" {
		return "any time"
	}
	if w.OpenNow {
		return w.Hours + " (open now)"
	}
	return w.Hours + " (closed now)"
}

func (m Model) cardCI(w, h int) ([]string, string) {
	t := m.th
	runs := ciRuns(m.snap.Events, 20)
	if len(runs) == 0 {
		return []string{"", t.fg(t.muted).Italic(true).Render("No CI results yet."), t.fg(t.muted).Render("Workers record them with smithy cards set ci=…")}, "0 runs"
	}
	var b strings.Builder
	green := 0
	var took []time.Duration
	full, hollow := "■", "□"
	if !t.g.unicode {
		full, hollow = "#", "x"
	}
	for _, r := range runs {
		if r.green {
			green++
			b.WriteString(t.fg(t.ok).Render(full) + " ")
		} else {
			b.WriteString(t.fg(t.err).Render(hollow) + " ")
		}
		if r.took > 0 {
			took = append(took, r.took)
		}
	}
	rate := green * 100 / len(runs)
	rc := t.ok
	if rate < 80 {
		rc = t.warn
	}
	if rate < 50 {
		rc = t.err
	}
	lines := []string{b.String(), "",
		t.fg(rc).Bold(!t.plain).Render(fmt.Sprintf("%d%%", rate)) + t.fg(t.muted).Render(fmt.Sprintf(" green over the last %d runs", len(runs))),
		t.fg(t.muted).Render("median CI time ") + t.fg(t.text).Render(dur(percentile(took, 0.5))) + t.fg(t.muted).Render("   p90 ") + t.fg(t.text).Render(dur(percentile(took, 0.9)))}
	return lines, fmt.Sprintf("%d runs", len(runs))
}

func (m Model) cardLead(w, h int) ([]string, string) {
	t := m.th
	toReady, toMerge := leadTimes(m.snap.Events)
	mut := t.fg(t.muted)
	row := func(name string, ds []time.Duration) []string {
		if len(ds) == 0 {
			return []string{t.fg(t.text).Render(name), mut.Render("  no data yet")}
		}
		return []string{t.fg(t.text).Render(name) + mut.Render(fmt.Sprintf("  %d items", len(ds))),
			mut.Render("  median ") + t.fg(t.text).Bold(!t.plain).Render(fmt.Sprintf("%-7s", dur(percentile(ds, 0.5)))) +
				mut.Render(" p90 ") + t.fg(t.text).Render(fmt.Sprintf("%-7s", dur(percentile(ds, 0.9)))) + "  " +
				t.fg(t.accent).Render(sparkline(histogram(ds, 10), 10, t.g.ticks))}
	}
	lines := append(row("queued → PR ready", toReady), "")
	lines = append(lines, row("PR ready → merged", toMerge)...)
	return lines, "7 days"
}

func (m Model) cardNeeds(w, h int) ([]string, string) {
	t := m.th
	rs := m.rows(vNeeds)
	if len(rs) == 0 {
		return []string{"", t.fg(t.ok).Render(t.g.pass + " Nothing needs you."), t.fg(t.muted).Italic(true).Render("Decisions, PRs to review and blocked work land here.")}, "0"
	}
	lines := []string{t.fg(t.warn).Bold(!t.plain).Render(plural(len(rs), "item waits", "items wait") + " for you"), ""}
	for k, r := range rs {
		if k >= 3 || len(lines)+1 >= h {
			break
		}
		lines = append(lines, t.fg(r.cells[0].c).Render(r.cells[0].s)+" "+t.fg(t.text).Render(short(r.cells[1].s, w-2)))
	}
	if len(rs) > 3 {
		lines = append(lines, t.fg(t.muted).Render(fmt.Sprintf("+%d more · enter or 4 for the list", len(rs)-3)))
	}
	return lines, fmt.Sprint(len(rs))
}
