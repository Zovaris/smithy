//go:build harness

package chat

import (
	"context"
	"fmt"
	"image/color"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/tui/kit"
	"github.com/Zovaris/smithy/internal/usage"
)

// usageAPI is the daemon's GET /v1/usage: the token report smithy stats reads from the events log.
type usageAPI interface {
	Usage(ctx context.Context, session string) (events.Report, error)
}

// usageMsg carries the daemon's token report to /usage.
type usageMsg struct{ r events.Report }

// openUsage is /usage: the report modal on the open session's period, or today's without one.
func (m chatView) openUsage() (tea.Model, tea.Cmd) {
	a, ok := m.cfg.(usageAPI)
	if !ok {
		m.system("/usage needs a daemon this chat can reach")
		return m, nil
	}
	m.usage, m.overlay, m.pal = nil, overlayUsage, palette{}
	if m.current == "" {
		m.pal.sel = 1
	}
	id := m.current
	return m, func() tea.Msg {
		ctx, cancel := daemonCtx()
		defer cancel()
		r, err := a.Usage(ctx, id)
		if err != nil {
			return sessionErrMsg{what: "/usage", err: err}
		}
		return usageMsg{r}
	}
}

// usagePeriods are the periods /usage steps through with left and right.
var usagePeriods = []string{"this session", "today", "7 days", "30 days", "all time"}

func (m chatView) usageKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.overlay = overlayNone
	case "left", "h":
		m.pal.sel = (m.pal.sel + len(usagePeriods) - 1) % len(usagePeriods)
	case "right", "l", "tab":
		m.pal.sel = (m.pal.sel + 1) % len(usagePeriods)
	}
	return m, nil
}

// usagePeriod is the tokens of period i of r.
func usagePeriod(r events.Report, i int) events.Tokens {
	return [...]events.Tokens{r.Session, r.Today, r.Week, r.Month, r.All}[i]
}

// notRecorded is how /usage names the runs logged before agents and models were recorded.
func notRecorded(k, what string) (string, bool) {
	if k == "unknown" || k == "" {
		return "earlier runs, " + what + " not recorded", true
	}
	return k, false
}

// usageBox is /usage: period chips, four headline tiles, spend (or tokens) per day over the last
// 30 days, the split by agent and by model with bars, and the token mix. Bars grow in as it opens.
func (m chatView) usageBox(screen []string, w, h int) []string {
	t := m.th
	bw := max(min(120, w-6), min(w, 40))
	bg := on(t.Raised)
	row := func(s string) string { return fill(bg, bw, s) }
	title := t.Fg(t.Surface).Bold(true).Render(" Usage") + t.Fg(t.Surface).Render("   tokens and spend from the events log")
	out := []string{fill(on(t.Accent), bw, spread(title, t.Fg(t.Surface).Render("esc  "), bw)), row("")}
	chips := "  "
	for i, p := range usagePeriods {
		if i == m.pal.sel {
			chips += m.chip(" "+p+" ", t.Surface, t.Accent, true)
		} else {
			chips += m.chip(" "+p+" ", t.Muted, t.Overlay, false)
		}
		chips += bg.Render(" ")
	}
	out = append(out, row(spread(chips, t.Fg(t.Dim).Render("←→ period  "), bw)), row(""))
	if m.usage == nil {
		out = append(out, row("  "+t.Fg(t.Muted).Render("reading token use…")), row(""))
		return m.overBox(screen, out, w)
	}
	r := *m.usage
	p := usagePeriod(r, m.pal.sel)
	grow := m.mo.pos
	if m.mo.shown != overlayUsage {
		grow = 1
	}
	out = append(out, m.usageTiles(r, p, bw)...)
	out = append(out, row(""))
	out = append(out, m.usageDays(r, bw, grow)...)
	out = append(out, row(""))
	out = append(out, m.usageSplit(p, bw, grow)...)
	out = append(out, row(""))
	tot := p.Total.Input + p.Total.Output + p.Total.CacheRead + p.Total.CacheWrite
	if tot > 0 {
		out = append(out, row("  "+t.Fg(t.Strong).Bold(true).Render("Tokens")+"  "+t.Fg(t.Muted).Render(
			kilo(p.Total.CacheRead+p.Total.CacheWrite)+" cache "+t.G.Dot+" "+kilo(p.Total.Input)+" in "+t.G.Dot+" "+kilo(p.Total.Output)+" out")))
		n := bw - 4
		seg := func(v int64, c color.Color) string {
			k := int(float64(v) / float64(tot) * float64(n) * grow)
			if v > 0 {
				k = max(k, 1)
			}
			return m.cells(k, c)
		}
		bar := seg(p.Total.CacheRead+p.Total.CacheWrite, t.Warn) + seg(p.Total.Input, t.Info) + seg(p.Total.Output, t.Ok)
		out = append(out, row("  "+bar), row(""))
	}
	if p.Runs == 0 {
		out = append(out, row("  "+t.Fg(t.Muted).Render("no token use recorded in this period: agents report it when a run or turn ends")), row(""))
	}
	out = append(out, fill(on(t.Overlay), bw, "  "+keyHints(t, "←→", "period", "esc", "close")))
	return m.overBox(screen, out, w)
}

// cells is n flat cells in c; # without colour.
func (m chatView) cells(n int, c color.Color) string {
	if n <= 0 {
		return ""
	}
	if m.th.Plain || c == nil {
		return strings.Repeat("#", n)
	}
	return on(c).Render(strings.Repeat(" ", n))
}

// usageTiles is the four headline numbers of period p, each over a tick in its own colour.
func (m chatView) usageTiles(r events.Report, p events.Tokens, bw int) []string {
	t := m.th
	tw := (bw - 4 - 3*2) / 4
	cost := "—"
	if p.Total.CostUSD != nil {
		cost = fmt.Sprintf("$%.2f", *p.Total.CostUSD)
	}
	todayCost := ""
	if r.Today.Total.CostUSD != nil {
		todayCost = fmt.Sprintf("$%.2f today", *r.Today.Total.CostUSD)
	}
	in := p.Total.Input + p.Total.CacheRead + p.Total.CacheWrite
	hit := ""
	if in > 0 {
		hit = fmt.Sprintf("%d%% of input", p.Total.CacheRead*100/in)
	}
	tiles := []struct {
		big, lab, sub string
		c             color.Color
	}{
		{cost, "spent", todayCost, t.Accent},
		{fmt.Sprint(p.Runs), "runs", fmt.Sprintf("%d today", r.Today.Runs), t.Info},
		{kilo(p.Total.Output), "tokens out", kilo(r.Today.Total.Output) + " today", t.Ok},
		{kilo(p.Total.CacheRead), "cache read", hit, t.Warn},
	}
	ov := on(t.Overlay)
	var out []string
	for line := range 4 {
		s := on(t.Raised).Render("  ")
		for i, tl := range tiles {
			var c string
			switch line {
			case 0:
				c = t.Fg(tl.c).Render(strings.Repeat("▀", 2))
				if !t.G.Unicode {
					c = ""
				}
			case 1:
				c = " " + t.Fg(t.Strong).Bold(true).Render(tl.big)
			case 2:
				c = " " + t.Fg(tl.c).Render(tl.lab)
			case 3:
				c = " " + t.Fg(t.Dim).Render(tl.sub)
			}
			s += fill(ov, tw, c)
			if i < 3 {
				s += on(t.Raised).Render("  ")
			}
		}
		out = append(out, fill(on(t.Raised), bw, s))
	}
	return out
}

// usageDays is the last 30 days as bars: spend when costs are known, else tokens; today in the
// strong colour, the rest in the accent.
func (m chatView) usageDays(r events.Report, bw int, grow float64) []string {
	t := m.th
	bg := on(t.Raised)
	row := func(s string) string { return fill(bg, bw, s) }
	if len(r.Days) == 0 {
		return nil
	}
	val := func(d events.DayUse) float64 { return d.Cost }
	what, unit := "Spend per day", func(v float64) string { return fmt.Sprintf("$%.2f", v) }
	peak := 0.0
	for _, d := range r.Days {
		peak = max(peak, d.Cost)
	}
	if peak == 0 {
		val = func(d events.DayUse) float64 { return float64(d.Tokens) }
		what, unit = "Tokens per day", func(v float64) string { return kilo(int64(v)) }
		for _, d := range r.Days {
			peak = max(peak, float64(d.Tokens))
		}
	}
	peakDay := ""
	for _, d := range r.Days {
		if val(d) == peak && peak > 0 {
			peakDay = d.Day
		}
	}
	right := ""
	if peak > 0 {
		right = t.Fg(t.Dim).Render("max "+unit(peak)+" "+t.G.Dot+" "+peakDay) + "  "
	}
	out := []string{row(spread("  "+t.Fg(t.Strong).Bold(true).Render(what)+t.Fg(t.Dim).Render("  last 30 days"), right, bw))}
	const rows = 6
	cw := max((bw-12)/len(r.Days), 1)
	ticks := []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	if !t.G.Unicode {
		ticks = []string{" ", ".", ".", "-", "-", "=", "=", "#", "#"}
	}
	for lv := rows - 1; lv >= 0; lv-- {
		axis := "     "
		if lv == rows-1 && peak > 0 {
			axis = fmt.Sprintf("%-5s", kit.Short(unit(peak), 5))
		}
		if lv == 0 {
			axis = fmt.Sprintf("%-5s", "0")
		}
		s := "  " + t.Fg(t.Dim).Render(axis) + t.Fg(t.Border).Render(t.G.Pipe)
		for i, d := range r.Days {
			hgt := 0.0
			if peak > 0 {
				hgt = val(d) / peak * rows * grow
			}
			c := t.Accent
			if i == len(r.Days)-1 {
				c = t.Strong
			}
			cell := " "
			switch {
			case hgt >= float64(lv+1):
				cell = ticks[8]
			case hgt > float64(lv):
				cell = ticks[int((hgt-float64(lv))*8)]
			}
			if lv == 0 && val(d) == 0 {
				cell, c = ticks[1], t.Border
			}
			s += t.Fg(c).Render(strings.Repeat(cell, max(cw-1, 1))) + " "
		}
		out = append(out, row(s))
	}
	first := r.Days[0].Day
	mid := r.Days[len(r.Days)/2].Day
	span := cw * len(r.Days)
	lab := "        " + t.Fg(t.Dim).Render(first[5:]) + strings.Repeat(" ", max(span/2-5, 1)) + t.Fg(t.Dim).Render(mid[5:])
	out = append(out, row(spread(lab, t.Fg(t.Strong).Render("today")+strings.Repeat(" ", max(bw-span-10, 2)), bw)))
	return out
}

// usageSplit is the period's use by agent and by model side by side: each a name, its spend (or
// tokens) and a bar against the period's total; runs without a recorded agent or model come last,
// muted.
func (m chatView) usageSplit(p events.Tokens, bw int, grow float64) []string {
	t := m.th
	half := (bw - 6) / 2
	bg := on(t.Raised)
	tot := func(u usage.Usage) float64 {
		if p.Total.CostUSD != nil && *p.Total.CostUSD > 0 {
			if u.CostUSD == nil {
				return 0
			}
			return *u.CostUSD
		}
		return float64(u.Input + u.Output + u.CacheRead + u.CacheWrite)
	}
	all := tot(p.Total)
	show := func(u usage.Usage) string {
		if p.Total.CostUSD != nil && *p.Total.CostUSD > 0 {
			return usage.Cost(u.CostUSD)
		}
		return kilo(int64(tot(u)))
	}
	col := func(head, what string, mp map[string]usage.Usage) []string {
		o := []string{fill(bg, half, t.Fg(t.Strong).Bold(true).Render(head))}
		keys := byTokens(mp)
		slices.SortStableFunc(keys, func(a, b string) int {
			_, ua := notRecorded(a, what)
			_, ub := notRecorded(b, what)
			switch {
			case ua && !ub:
				return 1
			case ub && !ua:
				return -1
			}
			return 0
		})
		for _, k := range keys {
			u := mp[k]
			name, faded := notRecorded(k, what)
			ns, bc, vc := t.Fg(t.Text), t.Accent, t.Strong
			if faded {
				ns, bc, vc = t.Fg(t.Muted).Italic(true), t.Muted, t.Muted
			}
			o = append(o, fill(bg, half, spread(ns.Render(kit.Short(name, half-12)), t.Fg(vc).Render(show(u)), half)))
			f := 0.0
			if all > 0 {
				f = tot(u) / all
			}
			n := int(f * float64(half) * grow)
			if t.G.Unicode && !t.Plain {
				o = append(o, fill(bg, half, t.Fg(bc).Render(strings.Repeat("▀", n))+t.Fg(t.Overlay).Render(strings.Repeat("▀", half-n))))
			} else {
				o = append(o, fill(bg, half, strings.Repeat("#", n)))
			}
		}
		return o
	}
	a, b := col("By agent", "agent", p.Agents), col("By model", "model", p.Models)
	for len(a) < len(b) {
		a = append(a, fill(bg, half, ""))
	}
	for len(b) < len(a) {
		b = append(b, fill(bg, half, ""))
	}
	out := make([]string, len(a))
	for i := range a {
		out[i] = fill(bg, bw, bg.Render("  ")+a[i]+bg.Render("  ")+b[i])
	}
	return out
}

// usagePanel lays the report out by period as text lines, each with its total, then per agent
// and per model; `smithy chat` keeps it for narrow screens and tests.
func usagePanel(r events.Report) *infoPanel {
	var out []string
	for _, p := range []struct {
		head string
		t    events.Tokens
	}{{"THIS SESSION", r.Session}, {"TODAY", r.Today}, {"LAST 7 DAYS", r.Week}, {"ALL TIME", r.All}} {
		if p.t.Runs == 0 {
			continue
		}
		out = append(out, "#"+p.head, fmt.Sprintf("total · %d run%s\t%s", p.t.Runs, plural(p.t.Runs), tokenLine(p.t.Total)))
		for _, k := range byTokens(p.t.Agents) {
			n, _ := notRecorded(k, "agent")
			out = append(out, "agent "+n+"\t"+tokenLine(p.t.Agents[k]))
		}
		for _, k := range byTokens(p.t.Models) {
			n, _ := notRecorded(k, "model")
			out = append(out, "model "+n+"\t"+tokenLine(p.t.Models[k]))
		}
	}
	if len(out) == 0 {
		out = []string{"no token use recorded yet: agents report it when a run or turn ends"}
	}
	return &infoPanel{title: "Usage", lines: out, scroll: true}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// tokenLine is "in 12.0k · out 3.4k · cache 56.0k · $1.25", the cost only when reported.
func tokenLine(u usage.Usage) string {
	s := "in " + kilo(u.Input) + " · out " + kilo(u.Output) + " · cache " + kilo(u.CacheRead+u.CacheWrite)
	if u.CostUSD != nil {
		s += " · " + usage.Cost(u.CostUSD)
	}
	return s
}

func kilo(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// byTokens is the keys of m, the most tokens first.
func byTokens(m map[string]usage.Usage) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	tot := func(u usage.Usage) int64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }
	sort.Slice(ks, func(i, j int) bool {
		if a, b := tot(m[ks[i]]), tot(m[ks[j]]); a != b {
			return a > b
		}
		return ks[i] < ks[j]
	})
	return ks
}
