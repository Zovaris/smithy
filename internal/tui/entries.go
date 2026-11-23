package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// entry is one selectable row of a panel, with everything the keys act on.
type entry struct {
	head   string // group line drawn before this row when it starts a group (Agents' batches)
	tree   string // tree branch drawn before the icon
	icon   string
	color  color.Color
	text   string // main line, plain
	meta   string // second line, plain and dim
	search string
	url    string
	copy   string
	title  string
	detail [][2]string
	log    string
}

// visible is the panel's entries after its filter.
func (m Model) visible(p panel) []entry {
	es := m.entries(p)
	f := strings.ToLower(strings.TrimSpace(m.filter[p]))
	if f == "" {
		return es
	}
	var out []entry
	head, last := "", ""
	for _, e := range es {
		if e.head != "" {
			head = e.head
		}
		if strings.Contains(strings.ToLower(e.search+" "+e.text+" "+e.meta), f) {
			e.head = ""
			if head != last {
				e.head, last = head, head
			}
			out = append(out, e)
		}
	}
	return out
}

func (m Model) entries(p panel) []entry {
	switch p {
	case pIncoming:
		return m.incoming()
	case pAgents:
		return m.agents()
	case pNeeds:
		return m.needs()
	case pActivity:
		return m.activityEntries()
	case pWorktrees:
		return m.worktreeEntries()
	}
	return m.plans()
}

func (m Model) incoming() []entry {
	t := m.th
	var out []entry
	for _, q := range m.snap.Queue {
		body := strings.Split(strings.TrimSpace(q.Body), "\n")
		if len(body) > 8 {
			body = append(body[:8], "…")
		}
		d := [][2]string{{"issue", q.Source + " " + q.ID}, {"title", q.Title}, {"model", or(q.Model)},
			{"speed", or(q.Speed)}, {"queued", q.QueuedAt.Local().Format("2006-01-02 15:04")}, {"url", q.URL}}
		for i, l := range body {
			if l = strings.TrimSpace(l); l != "" || i == 0 {
				k := ""
				if i == 0 {
					k = "text"
				}
				d = append(d, [2]string{k, l})
			}
		}
		out = append(out, entry{icon: t.g.off, color: t.accent, text: q.ID + "  " + q.Title,
			meta:   q.Source + " " + t.g.dot + " model " + or(q.Model) + " " + t.g.dot + " speed " + or(q.Speed) + " " + t.g.dot + " queued " + ago(m.snap.At.Sub(q.QueuedAt)),
			search: q.Source + " " + q.Model + " " + q.Speed, url: q.URL, copy: q.URL, title: "Issue " + q.ID, detail: d})
	}
	return out
}

func (m Model) beat(it Item) string {
	b := it.LastBeat
	if b == "" && it.Updated != nil {
		b = *it.Updated
	}
	if tm, err := time.Parse(time.RFC3339, b); err == nil {
		return ago(m.snap.At.Sub(tm))
	}
	return "-"
}

func (m Model) itemDetail(it Item) [][2]string {
	d := [][2]string{{"batch", it.Batch}, {"item", it.ID}, {"title", or(it.Title)}, {"issue", or(it.Source)},
		{"branch", or(it.Branch)}, {"worktree", or(it.Worktree)}, {"stage", it.Stage}, {"state", it.State},
		{"phase", it.Phase}, {"progress", it.Progress}, {"model", it.Model}, {"ci", it.CI}, {"pr", it.PR},
		{"last beat", m.beat(it)}, {"note", or(it.Text)}, {"card", cardPath(it.Batch, it.ID)}}
	if lines := tail(it.Log, 15); len(lines) > 0 {
		d = append(d, [2]string{"", ""}, [2]string{"log", "last 15 lines of " + it.Log})
		for _, l := range lines {
			d = append(d, [2]string{"", l})
		}
	}
	return d
}

func (m Model) stageLook(s string) (string, color.Color) {
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

func ciWord(t theme, s string) string {
	switch s {
	case "green", "pass", "success":
		return t.g.pass + " " + s
	case "red", "fail", "failure":
		return t.g.fail + " " + s
	}
	return s
}

func (m Model) agents() []entry {
	t := m.th
	var out []entry
	for _, b := range m.snap.Batches {
		done := 0
		for _, it := range b.Items {
			if it.Stage == "merged" || it.Stage == "reported" {
				done++
			}
		}
		head := fmt.Sprintf("%s  %d/%d done", b.Name, done, len(b.Items))
		for j, it := range b.Items {
			g, c := m.stageLook(it.Stage)
			branch := t.g.tee
			if j == len(b.Items)-1 {
				branch = t.g.elbow
			}
			issue := it.Source
			if issue == "" {
				issue = it.ID
			}
			text := fmt.Sprintf("%-11s %-12s %-8s %-9s %s", ansi.Truncate(issue, 11, "…"), ansi.Truncate(or(it.Title), 12, "…"), it.Stage, ciWord(t, it.CI), shortPR(it.PR))
			cont := t.g.pipe + " "
			if j == len(b.Items)-1 {
				cont = "  "
			}
			meta := cont + "  " + or(it.Branch) + " " + t.g.dot + " " + it.State + " " + t.g.dot + " beat " + m.beat(it) + " " + t.g.dot + " " + it.Model
			e := entry{tree: branch + " ", icon: g, color: c, text: text, meta: meta, search: b.Name + " " + it.ID + " " + it.Title + " " + it.Branch,
				url: it.PR, copy: or(it.Worktree), title: b.Name + "/" + it.ID, detail: m.itemDetail(it), log: it.Log}
			if it.Worktree == "" {
				e.copy = cardPath(it.Batch, it.ID)
			}
			if it.PR != "-" {
				e.copy = it.PR
			}
			if j == 0 {
				e.head = head
			}
			out = append(out, e)
		}
	}
	return out
}

func (m Model) needs() []entry {
	t := m.th
	var out []entry
	for _, d := range m.snap.Decisions {
		f := strings.Split(d, " · ")
		q, meta := d, ""
		if len(f) >= 3 {
			q, meta = f[2], f[0]+" "+t.g.dot+" "+f[1]+" "+t.g.dot+" "+strings.Join(f[3:], " · ")
		}
		names := []string{"date", "by", "decision", "why", "ref"}
		var det [][2]string
		ref := ""
		for j, v := range f {
			n := "field"
			if j < len(names) {
				n = names[j]
			}
			if r, ok := strings.CutPrefix(v, "ref: "); ok {
				v, ref = r, r
			}
			det = append(det, [2]string{n, v})
		}
		out = append(out, entry{icon: "?", color: t.warn, text: "decide: " + q, meta: meta, url: ref, copy: q,
			title: "Decision", detail: det, search: "decision"})
	}
	for _, b := range m.snap.Batches {
		for _, it := range b.Items {
			switch it.Stage {
			case "verify":
				out = append(out, entry{icon: t.g.pass, color: t.ok, text: "PR " + shortPR(it.PR) + " ready for you " + t.g.dot + " " + or(it.Title),
					meta: b.Name + "/" + it.ID + " " + t.g.dot + " CI " + it.CI, url: it.PR, copy: it.PR, title: "PR " + shortPR(it.PR),
					detail: m.itemDetail(it), log: it.Log, search: "pr review merge"})
			case "stuck", "decide":
				why := or(it.Text)
				out = append(out, entry{icon: t.g.fail, color: t.err, text: it.Stage + ": " + b.Name + "/" + it.ID + " " + t.g.dot + " " + why,
					meta: it.State + " " + t.g.dot + " beat " + m.beat(it), url: it.PR, copy: cardPath(it.Batch, it.ID), title: b.Name + "/" + it.ID,
					detail: m.itemDetail(it), log: it.Log, search: "blocked"})
			}
		}
	}
	return out
}

func (m Model) activityEntries() []entry {
	t := m.th
	var out []entry
	for _, e := range m.snap.Activity {
		icon, c := t.g.dot, t.muted
		switch e.Kind {
		case "poll":
			icon = t.g.off
		case "queued":
			icon, c = "+", t.accent
		case "launched":
			icon, c = t.g.half, t.warn
		default:
			icon, c = m.stageLook(e.Kind)
		}
		out = append(out, entry{icon: icon, color: c, text: e.At.Local().Format("15:04") + "  " + e.Text, url: e.URL, copy: e.URL,
			title: "Event", detail: [][2]string{{"time", e.At.Local().Format("2006-01-02 15:04:05")}, {"kind", e.Kind}, {"event", e.Text}, {"url", or(e.URL)}},
			search: e.Kind})
	}
	return out
}

func home(p string) string {
	if h, err := filepath.Abs(filepath.Join(homeDir(), "")); err == nil && h != "" && strings.HasPrefix(p, h+"/") {
		return "~" + p[len(h):]
	}
	return p
}

func (m Model) worktreeEntries() []entry {
	t := m.th
	var out []entry
	for _, w := range m.snap.Worktrees {
		state, c, icon := "clean", t.ok, t.g.on
		if w.Dirty {
			state, c, icon = "dirty", t.warn, t.g.half
		}
		if w.Orphan {
			state, c, icon = "orphaned "+t.g.dot+" "+state, t.warn, "!"
		}
		age := "-"
		if w.Age > 0 {
			age = ago(w.Age)
		}
		d := [][2]string{{"path", w.Path}, {"branch", or(w.Branch)}, {"item", or(w.Item)}, {"age", age}, {"state", state}, {"", ""}, {"git status", ""}}
		if len(w.Status) == 0 {
			d = append(d, [2]string{"", "clean"})
		}
		for _, l := range w.Status {
			d = append(d, [2]string{"", l})
		}
		out = append(out, entry{icon: icon, color: c, text: home(w.Path), meta: or(w.Branch) + " " + t.g.dot + " " + or(w.Item) + " " + t.g.dot + " " + age + " " + t.g.dot + " " + state,
			copy: w.Path, title: "Worktree", detail: d, search: w.Branch + " " + w.Item + " " + state})
	}
	return out
}

func (m Model) plans() []entry {
	t := m.th
	var out []entry
	for _, tl := range m.snap.Tasks {
		var parts []string
		for _, s := range statuses(tl.Counts) {
			parts = append(parts, fmt.Sprintf("%d %s", tl.Counts[s], s))
		}
		d := [][2]string{{"dir", tl.Dir}}
		for _, tk := range tl.Tasks {
			d = append(d, [2]string{tk.NN, tk.Status + "  " + tk.Title})
		}
		meter := ""
		if tl.Total > 0 {
			n := tl.Counts["done"] * 8 / tl.Total
			meter = strings.Repeat(t.g.shades[3], n) + strings.Repeat(t.g.shades[0], 8-n) + " "
		}
		c := t.muted
		if tl.Counts["doing"] > 0 {
			c = t.warn
		} else if tl.Total > 0 && tl.Counts["done"] == tl.Total {
			c = t.ok
		}
		out = append(out, entry{icon: t.g.dot, color: c, text: meter + filepath.Base(tl.Dir), meta: strings.Join(parts, " "+t.g.dot+" "),
			copy: tl.Dir, title: "Plan " + filepath.Base(tl.Dir), detail: d})
	}
	return out
}
