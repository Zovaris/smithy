package stats

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/text"
	"github.com/Zovaris/smithy/internal/tui/kit"
	"github.com/Zovaris/smithy/internal/usage"
	"github.com/charmbracelet/colorprofile"
)

var cardTitles = []string{"Work per hour", "Agents now", "Daily cap", "CI health", "How long it takes", "Needs you"}

// overview is the dashboard: six equal cards.
func (m Model) overview() []*lipgloss.Layer {
	t := m.th
	var out []*lipgloss.Layer
	for i, r := range m.cardRects() {
		w, h := r.w-4, r.h-3
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
				border = t.Warn
			}
		}
		out = append(out, layer(m.box(cardTitles[i], badge, m.sel[vOverview] == i, border, r, lines), r))
	}
	return out
}

// niceTop rounds a maximum up to 1, 2 or 5 times a power of ten.
func niceTop(v int) int {
	if v <= 1 {
		return 1
	}
	for p := 1; ; p *= 10 {
		for _, k := range []int{1, 2, 5} {
			if k*p >= v {
				return k * p
			}
		}
	}
}

// cardThroughput is a mirrored bar chart of the last 24 h, one column per hour on a shared
// baseline: launched grows up, PRs ready or merged hang down, on one "nice" scale labelled top
// and bottom, dotted gridlines on empty cells, x labels every 6 h and a gradient from the
// baseline (dim) to the tip (bright). Under 48 columns it is a pair of sparklines.
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
	mut := t.Fg(t.Muted)
	upC, downC := t.Info, t.Ok
	if t.Plain {
		upC = nil
	}
	up, down := "▇", "▇"
	if !t.G.Unicode {
		up, down = "^", "v"
	}
	legend := t.Fg(upC).Render("launched "+up) + " " + t.Fg(t.Text).Bold(!t.Plain).Render(fmt.Sprint(sum(a))) + "   " +
		t.Fg(downC).Render("ready/merged "+down) + " " + t.Fg(t.Text).Bold(!t.Plain).Render(fmt.Sprint(sum(b)))
	if sum(a)+sum(b) == 0 {
		return []string{legend, "", mut.Italic(true).Render("No launches or PRs in the last 24 h.")}, "last 24 h"
	}
	top := 1
	for i := range a {
		top = max(top, a[i], b[i])
	}
	top = niceTop(top)
	ys := fmt.Sprint(top)
	axisW := len(ys) + 1
	if w < 48 || h < 7 {
		return []string{legend,
			mut.Render("up   ") + t.Fg(upC).Render(kit.Sparkline(a, w-12, t.G.Ticks)),
			mut.Render("down ") + t.Fg(downC).Render(kit.Sparkline(b, w-12, t.G.Ticks)),
			mut.Render("     -24h" + strings.Repeat(" ", max(w-21, 1)) + "now")}, "last 24 h"
	}
	step := 2
	cols := min(24, (w-axisW)/step)
	a, b = a[24-cols:], b[24-cols:]
	ch := h - 3
	hu := (ch - 1) / 2
	hd := ch - 1 - hu
	blocks := []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	ascii := !t.G.Unicode
	if ascii {
		blocks = []string{" ", ".", ".", ":", ":", "|", "|", "#", "#"}
	}
	rich := !t.Plain && m.profile >= colorprofile.ANSI256
	upRamp := []color.Color{upC}
	downRamp := []color.Color{downC}
	if rich {
		upRamp = lipgloss.Blend1D(max(hu, 2), t.Ramp[5], t.Info)
		downRamp = lipgloss.Blend1D(max(hd, 2), lipgloss.Color("#1F5A36"), t.Ok)
	}
	grid := mut.Render("·")
	if ascii {
		grid = mut.Render(".")
	}
	vl, tk, cross, hz, corner, tick := "│", "┤", "┼", "─", "└", "┬"
	if ascii {
		vl, tk, cross, hz, corner, tick = "|", "+", "+", "-", "+", "+"
	}
	var lines []string
	lines = append(lines, legend)
	// up half: row 0 is the top
	for r := 0; r < hu; r++ {
		lab := strings.Repeat(" ", axisW-1) + vl
		if r == 0 {
			lab = fmt.Sprintf("%*s", axisW-1, ys) + tk
		}
		var sb strings.Builder
		sb.WriteString(mut.Render(lab))
		for i := 0; i < cols; i++ {
			v := a[i]
			lvl := v*hu*8/top - (hu-1-r)*8
			if v > 0 && r == hu-1 {
				lvl = max(lvl, 1)
			}
			lvl = max(min(lvl, 8), 0)
			cell := " "
			switch {
			case lvl > 0:
				cell = t.Fg(upRamp[min(hu-1-r, len(upRamp)-1)]).Render(blocks[lvl])
			case r == 0:
				cell = grid
			}
			sb.WriteString(cell)
			if step == 2 {
				sb.WriteString(" ")
			}
		}
		lines = append(lines, sb.String())
	}
	base := fmt.Sprintf("%*s", axisW-1, "0") + cross + strings.Repeat(hz, cols*step)
	lines = append(lines, mut.Render(base))
	// down half: row 0 hangs right under the baseline
	for d := 0; d < hd; d++ {
		lab := strings.Repeat(" ", axisW-1) + vl
		if d == hd-1 {
			lab = fmt.Sprintf("%*s", axisW-1, ys) + tk
		}
		var sb strings.Builder
		sb.WriteString(mut.Render(lab))
		for i := 0; i < cols; i++ {
			v := b[i]
			lvl := v*hd*8/top - d*8
			if v > 0 && d == 0 {
				lvl = max(lvl, 1)
			}
			lvl = max(min(lvl, 8), 0)
			c := downRamp[min(d, len(downRamp)-1)]
			cell := " "
			switch {
			case lvl == 8:
				cell = t.Fg(c).Render(blocks[8])
			case lvl > 0 && ascii:
				cell = t.Fg(c).Render(blocks[lvl])
			case lvl > 0 && t.Plain:
				cell = "▀"
				if lvl < 4 {
					cell = "▔"
				}
			case lvl > 0:
				cell = lipgloss.NewStyle().Foreground(c).Reverse(true).Render(blocks[8-lvl])
			case d == hd-1:
				cell = grid
			}
			sb.WriteString(cell)
			if step == 2 {
				sb.WriteString(" ")
			}
		}
		lines = append(lines, sb.String())
	}
	axis := []rune(strings.Repeat(hz, cols*step))
	labels := []rune(strings.Repeat(" ", cols*step+6))
	type mark struct {
		i   int
		lab string
	}
	marks := []mark{{0, fmt.Sprintf("-%dh", cols)}}
	for _, k := range []int{18, 12, 6} {
		if i := cols - 1 - k; i > 3 {
			marks = append(marks, mark{i, fmt.Sprintf("-%dh", k)})
		}
	}
	marks = append(marks, mark{cols - 1, "now"})
	for k, mk := range marks {
		x := mk.i * step
		if x < len(axis) {
			axis[x] = []rune(tick)[0]
		}
		at := max(x-1, 0)
		if k == 0 {
			at = x
		}
		if k == len(marks)-1 {
			at = x - len(mk.lab) + 1
		}
		for i, r := range mk.lab {
			if at+i >= 0 && at+i < len(labels) {
				labels[at+i] = r
			}
		}
	}
	lines = append(lines, mut.Render(strings.Repeat(" ", axisW-1)+corner+string(axis)), mut.Render(strings.Repeat(" ", axisW)+strings.TrimRight(string(labels), " ")))
	return lines, "last 24 h"
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
	var orch string
	if sv := m.snap.Watcher.Serve; sv.SessionOn && sv.SessionPID > 0 {
		el := ""
		if !sv.SessionAt.IsZero() {
			el = " " + t.G.Dot + " " + dur(m.snap.At.Sub(sv.SessionAt))
		}
		orch = t.Fg(t.Accent).Render(t.G.On) + " " + t.Fg(t.Strong).Render("orchestrator") +
			t.Fg(t.Muted).Render(kit.Short(fmt.Sprintf("  %s pid %d%s", or(sv.Agent), sv.SessionPID, el), max(w-14, 10)))
	}
	if total == 0 {
		if orch != "" {
			return []string{orch, "", t.Fg(t.Muted).Italic(true).Render("No workers yet.")}, "1 live"
		}
		return []string{"", t.Fg(t.Muted).Italic(true).Render("No workers right now."), t.Fg(t.Accent).Render("$ smithy warden status")}, "0"
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
		if !t.G.Unicode {
			g = string("qbcprx"[k])
		}
		bar.WriteString(t.Fg(m.stageColor(st)).Render(strings.Repeat(g, n)))
	}
	lines := []string{bar.String(), ""}
	var leg []string
	for _, st := range order {
		leg = append(leg, t.Fg(m.stageColor(st)).Render(t.G.On)+" "+t.Fg(t.Text).Render(fmt.Sprintf("%d", counts[st]))+" "+t.Fg(t.Muted).Render(st))
	}
	lines = append(lines, strings.Join(leg[:3], "   "), strings.Join(leg[3:], "   "), "")
	if orch != "" {
		lines = append(lines, orch)
	}
	sort.SliceStable(live, func(i, j int) bool { return m.beatAge(live[i]) < m.beatAge(live[j]) })
	for k, it := range live {
		if k >= 3 || len(lines) >= h {
			break
		}
		st := stageOf(it)
		g, gc := t.G.On, m.stageColor(st)
		switch {
		case it.Stage == "decide":
			g, gc = t.G.Wait, t.Warn
		case st == "blocked":
			g = t.G.Fail
		case st == "ready":
			g = t.G.Pass
		}
		title := kit.Short(it.ID+" "+it.Title, w-22)
		name := t.Fg(t.Text).Render(title)
		if m.motion && running(st) {
			name = t.Shimmer(title, m.band(len([]rune(title)), k))
		}
		lines = append(lines, t.Fg(gc).Render(g)+" "+name+
			t.Fg(t.Muted).Render("  "+st+" "+t.G.Dot+" "+m.beat(it)))
	}
	return lines, fmt.Sprintf("%d live", total)
}

func (m Model) cardCap(w, h int) ([]string, string) {
	t := m.th
	wt := m.snap.Watcher
	big := t.Fg(t.Text).Bold(!t.Plain).Render(fmt.Sprintf("%d / %d", wt.Today, wt.Cap)) + t.Fg(t.Muted).Render(" launched today")
	lines := []string{big, m.capBar(w), ""}
	week := wt.Daily
	if len(week) > 7 {
		week = week[len(week)-7:]
	}
	var nums []string
	for _, n := range week {
		nums = append(nums, fmt.Sprint(n))
	}
	lines = append(lines, t.Fg(t.Muted).Render("7 days  ")+t.Fg(t.Accent).Render(kit.Sparkline(week, 7, t.G.Ticks))+t.Fg(t.Muted).Render("  "+strings.Join(nums, " ")))
	hours := "any time"
	if wt.Hours != "" {
		hours = hoursWord(wt)
	}
	lines = append(lines, t.Fg(t.Muted).Render("hours   ")+t.Fg(t.Text).Render(hours))
	if wt.Paused {
		lines = append(lines, t.Fg(t.Warn).Bold(!t.Plain).Render("intake paused · p resumes"))
	}
	lines = append(lines, "", m.spendLine())
	if len(wt.Spend) > 0 {
		cents := make([]int, len(wt.Spend))
		var nums []string
		for i, v := range wt.Spend {
			cents[i] = int(v*100 + 0.5)
			nums = append(nums, fmt.Sprintf("%.0f", v))
		}
		lines = append(lines, t.Fg(t.Muted).Render("7 days  ")+t.Fg(t.Accent).Render(kit.Sparkline(cents, 7, t.G.Ticks))+t.Fg(t.Muted).Render("  $"+strings.Join(nums, " ")))
	}
	pct := 0
	if wt.Cap > 0 {
		pct = wt.Today * 100 / wt.Cap
	}
	return lines, fmt.Sprintf("%d%%", pct)
}

// spendLine is "Spend today": the reported cost so far, against auto.max_cost_per_day when set.
func (m Model) spendLine() string {
	t := m.th
	wt := m.snap.Watcher
	today := "-"
	over := false
	if n := len(wt.Spend); n > 0 {
		today = usage.Cost(&wt.Spend[n-1])
		over = wt.SpendCap != nil && wt.Spend[n-1] >= *wt.SpendCap
	}
	s := t.Fg(t.Muted).Render("spend   ") + t.Fg(t.Text).Bold(!t.Plain).Render(today) + t.Fg(t.Muted).Render(" today")
	if wt.SpendCap != nil {
		s += t.Fg(t.Muted).Render(" · cap " + usage.Cost(wt.SpendCap))
	}
	if over {
		s += t.Fg(t.Err).Bold(!t.Plain).Render(" · reached")
	}
	return s
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
		return []string{"", t.Fg(t.Muted).Italic(true).Render("No CI results yet."), t.Fg(t.Muted).Render("Workers record them with smithy cards set ci=…")}, "0 runs"
	}
	var b strings.Builder
	green := 0
	var took []time.Duration
	full, hollow := "■", "□"
	if !t.G.Unicode {
		full, hollow = "#", "x"
	}
	for _, r := range runs {
		if r.green {
			green++
			b.WriteString(t.Fg(t.Ok).Render(full) + " ")
		} else {
			b.WriteString(t.Fg(t.Err).Render(hollow) + " ")
		}
		if r.took > 0 {
			took = append(took, r.took)
		}
	}
	rate := green * 100 / len(runs)
	rc := t.Ok
	if rate < 80 {
		rc = t.Warn
	}
	if rate < 50 {
		rc = t.Err
	}
	lines := []string{b.String(), "",
		t.Fg(rc).Bold(!t.Plain).Render(fmt.Sprintf("%d%%", rate)) + t.Fg(t.Muted).Render(fmt.Sprintf(" green over the last %d runs", len(runs))),
		t.Fg(t.Muted).Render("median CI time ") + t.Fg(t.Text).Render(dur(percentile(took, 0.5))) + t.Fg(t.Muted).Render("   p90 ") + t.Fg(t.Text).Render(dur(percentile(took, 0.9)))}
	return lines, fmt.Sprintf("%d runs", len(runs))
}

func (m Model) cardLead(w, h int) ([]string, string) {
	t := m.th
	toReady, toMerge := leadTimes(m.snap.Events)
	mut := t.Fg(t.Muted)
	row := func(name string, ds []time.Duration) []string {
		if len(ds) == 0 {
			return []string{t.Fg(t.Text).Bold(!t.Plain).Render(name) + mut.Render(": no data yet")}
		}
		return []string{
			t.Fg(t.Text).Bold(!t.Plain).Render(name) + mut.Render(": median ") + t.Fg(t.Text).Bold(!t.Plain).Render(dur(percentile(ds, 0.5))) +
				mut.Render(" (slowest 10%: ") + t.Fg(t.Text).Render(dur(percentile(ds, 0.9))) + mut.Render(")"),
			mut.Render("  spread, fast → slow  ") + t.Fg(t.Accent).Render(kit.Sparkline(histogram(ds, 12), 12, t.G.Ticks)) + mut.Render(fmt.Sprintf("  %d items", len(ds))),
		}
	}
	lines := append(row("issue → PR ready", toReady), "")
	lines = append(lines, row("PR ready → merged", toMerge)...)
	return lines, "last 7 days"
}

func (m Model) cardNeeds(w, h int) ([]string, string) {
	t := m.th
	rs := m.rows(vNeeds)
	if len(rs) == 0 {
		return []string{"", t.Fg(t.Ok).Render(t.G.Pass + " Nothing needs you."), t.Fg(t.Muted).Italic(true).Render("Decisions, PRs to review and blocked work land here.")}, "0"
	}
	lines := []string{t.Fg(t.Warn).Bold(!t.Plain).Render(text.Plural(len(rs), "item waits", "items wait") + " for you"), ""}
	for k, r := range rs {
		if k >= 3 || len(lines)+1 >= h {
			break
		}
		lines = append(lines, t.Fg(r.cells[0].c).Render(r.cells[0].s)+" "+t.Fg(t.Text).Render(kit.Short(r.cells[1].s, w-2)))
	}
	if len(rs) > 3 {
		lines = append(lines, t.Fg(t.Muted).Render(fmt.Sprintf("+%d more · enter or 4 for the list", len(rs)-3)))
	}
	return lines, fmt.Sprint(len(rs))
}
