package tui

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/events"
)

type cell struct {
	s    string
	c    color.Color
	bold bool
}

// col is a table column: a fixed width, or flex (takes the rest); lower prio is dropped last.
type col struct {
	name string
	w    int
	flex bool
	prio int
}

// row is one selectable line of a view, with everything the keys act on.
type row struct {
	cells    []cell
	search   string
	url      string
	copy     string
	cmd      string
	title    string
	detail   [][2]string
	logPath  string
	logTail  []string
	text     []string
	timeline [][2]string
	decision int    // pending decision number (Needs you), 0 otherwise
	md       string // markdown the reader shows on enter
	mdTitle  string
	chip     string // status shown in the modal title
	chipC    color.Color
	explain  []string // what the row means and what the keys do, for the modal
}

func (m Model) cols(v view) []col {
	switch v {
	case vAgents:
		return []col{{"issue", 10, false, 0}, {"title", 0, true, 0}, {"repo", 10, false, 4}, {"branch", 16, false, 5},
			{"model", 6, false, 3}, {"speed", 6, false, 6}, {"stage", 8, false, 0}, {"CI", 9, false, 1}, {"PR", 6, false, 2},
			{"beat", 8, false, 2}, {"age", 5, false, 7}}
	case vIncoming:
		return []col{{"issue", 9, false, 0}, {"title", 0, true, 0}, {"source", 7, false, 2}, {"model", 6, false, 1}, {"speed", 6, false, 3}, {"queued", 8, false, 1}}
	case vNeeds:
		return []col{{"", 2, false, 0}, {"what", 0, true, 0}, {"where", 22, false, 1}, {"keys", 22, false, 2}}
	case vDone:
		return []col{{"issue", 10, false, 0}, {"title", 0, true, 0}, {"PR", 7, false, 1}, {"result", 8, false, 0}, {"took", 8, false, 1}, {"when", 9, false, 2}}
	case vActivity:
		return []col{{"when", 8, false, 0}, {"kind", 9, false, 0}, {"what", 0, true, 0}, {"where", 22, false, 1}}
	case vWorktrees:
		return []col{{"path", 0, true, 0}, {"branch", 20, false, 1}, {"item", 16, false, 2}, {"age", 8, false, 3}, {"dirty", 6, false, 1}, {"↑↓", 7, false, 4}, {"state", 9, false, 0}}
	case vPlans:
		return []col{{"plan", 0, true, 0}, {"progress", 12, false, 0}, {"done", 6, false, 1}}
	}
	return nil
}

// rows is a view's main list after its filter (and, for Agents, its sort).
func (m Model) rows(v view) []row {
	var rs []row
	switch v {
	case vAgents:
		rs = m.agentRows()
	case vIncoming:
		rs = m.incomingRows()
	case vNeeds:
		rs = m.needRows()
	case vDone:
		rs = m.doneRows()
	case vActivity:
		rs = m.activityRows()
	case vWorktrees:
		rs = m.worktreeRows()
	case vPlans:
		rs = m.planRows()
	}
	f := strings.ToLower(strings.TrimSpace(m.filter[v]))
	if f == "" {
		return rs
	}
	var out []row
	for _, r := range rs {
		hay := r.search
		for _, c := range r.cells {
			hay += " " + c.s
		}
		if strings.Contains(strings.ToLower(hay), f) {
			out = append(out, r)
		}
	}
	return out
}

// rows2 is a view's second pane: Incoming's skipped issues, a plan's tasks.
func (m Model) rows2(v view) []row {
	t := m.th
	switch v {
	case vIncoming:
		var out []row
		es := m.snap.Events
		for i := len(es) - 1; i >= 0 && len(out) < 40; i-- {
			e := es[i]
			if e.Kind != "skipped" {
				continue
			}
			reason, title, _ := strings.Cut(e.Detail, ": ")
			out = append(out, row{cells: []cell{{s: e.ID, c: t.muted}, {s: title, c: t.text}, {s: reason, c: t.warn}, {s: ago(m.snap.At.Sub(e.T)), c: t.muted}},
				title: "Ignored " + e.ID, chip: "ignored", chipC: t.muted,
				explain: []string{"Seen by smithy auto but not started: " + reason + ".", "Add the auto label (auto.label) or change the filters to start it."}, detail: [][2]string{{"issue", e.Source + " " + e.ID}, {"title", title}, {"why", reason}, {"seen", e.T.Local().Format("2006-01-02 15:04")}}})
		}
		return out
	case vPlans:
		ps := m.rows(vPlans)
		i := m.sel[vPlans]
		if i >= len(ps) || i >= len(m.snap.Tasks) {
			return nil
		}
		tl := m.planList(ps[i].copy)
		var out []row
		for _, tk := range tl.Tasks {
			c := m.statusColor(tk.Status)
			text := strings.Split(tl.Text[filepath.Base(tk.File)], "\n")
			if tl.Text == nil {
				text = readLines(tk.File, 400)
			}
			out = append(out, row{cells: []cell{{s: tk.NN, c: t.muted}, {s: "[" + tk.Status + "]", c: c, bold: true}, {s: tk.Title, c: t.text}},
				title: tk.NN + " · " + tk.Title, text: text, copy: tk.File, md: strings.Join(text, "\n"), mdTitle: home(tk.File)})
		}
		return out
	}
	return nil
}

func (m Model) planList(dir string) TaskList {
	for _, tl := range m.snap.Tasks {
		if tl.Dir == dir {
			return tl
		}
	}
	return TaskList{}
}

func readLines(file string, n int) []string {
	b, err := os.ReadFile(file)
	if err != nil {
		return []string{"cannot read " + file}
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return cleanAll(lines)
}

func (m Model) statusColor(s string) color.Color {
	t := m.th
	switch s {
	case "done":
		return t.ok
	case "doing":
		return t.warn
	case "blocked":
		return t.err
	}
	return t.muted
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

func (m Model) beatAge(it Item) time.Duration {
	b := it.LastBeat
	if b == "" && it.Updated != nil {
		b = *it.Updated
	}
	if tm, err := time.Parse(time.RFC3339, b); err == nil {
		return m.snap.At.Sub(tm)
	}
	return 1 << 62
}

// stageOf is the stage a worker shows: queued, building, checking, pr_open, ready, blocked, merged.
func stageOf(it Item) string {
	switch {
	case it.Stage == "queued" || it.Stage == "held" || it.Stage == "starting":
		return "queued"
	case it.Stage == "stuck" || it.Stage == "decide":
		return "blocked"
	case it.Stage == "merged" || it.Stage == "reported" || it.Stage == "dropped":
		return "merged"
	case it.State == "ready":
		return "ready"
	case it.State == "pr_open":
		return "pr_open"
	case strings.Contains(it.Phase, "check") || strings.Contains(it.Phase, "verify") || strings.Contains(it.Phase, "test"):
		return "checking"
	}
	return "building"
}

var stageOrder = []string{"blocked", "ready", "pr_open", "checking", "building", "queued", "merged"}

func (m Model) stageColor(s string) color.Color {
	t := m.th
	switch s {
	case "ready", "merged":
		return t.ok
	case "pr_open", "checking":
		return t.accent
	case "building":
		return t.warn
	case "blocked":
		return t.err
	}
	return t.muted
}

func (m Model) ciCell(s string) cell {
	t := m.th
	switch s {
	case "green", "pass", "success":
		return cell{s: t.g.pass + " green", c: t.ok}
	case "red", "fail", "failure":
		return cell{s: t.g.fail + " red", c: t.err}
	case "pending", "running", "queued", "in_progress":
		return cell{s: t.g.half + " running", c: t.warn}
	}
	return cell{s: "-", c: t.muted}
}

func (m Model) items() []Item {
	var out []Item
	for _, b := range m.snap.Batches {
		out = append(out, b.Items...)
	}
	return out
}

func (m Model) timeline(it Item) [][2]string {
	var out [][2]string
	for _, e := range m.snap.Events {
		if e.Batch == it.Batch && e.Item == it.ID || (e.Kind == "queued" && it.Source != "" && e.ID == it.Source) {
			what := e.Kind
			if e.Detail != "" && e.Kind != "queued" && e.Kind != "launched" {
				what += " " + e.Detail
			}
			out = append(out, [2]string{e.T.Local().Format("15:04"), what})
		}
	}
	if len(out) == 0 && it.Updated != nil {
		if tm, err := time.Parse(time.RFC3339, *it.Updated); err == nil {
			out = append(out, [2]string{tm.Local().Format("15:04"), it.State})
		}
	}
	return out
}

func (m Model) itemDetail(it Item) [][2]string {
	d := [][2]string{{"batch", it.Batch}, {"item", it.ID}, {"title", or(it.Title)}, {"issue", or(it.Source)}, {"repo", or(it.Repo)},
		{"branch", or(it.Branch)}, {"worktree", or(it.Worktree)}, {"stage", stageOf(it)}, {"state", it.State},
		{"phase", it.Phase}, {"model", it.Model}, {"speed", or(it.Speed)}, {"ci", it.CI}, {"pr", it.PR},
		{"last beat", m.beat(it)}, {"note", or(it.Text)}}
	if !m.snap.Demo {
		d = append(d, [2]string{"card", cardPath(it.Batch, it.ID)})
	}
	if tl := m.timeline(it); len(tl) > 0 {
		d = append(d, [2]string{"", ""}, [2]string{"timeline", ""})
		for _, e := range tl {
			d = append(d, [2]string{"", e[0] + "  " + e[1]})
		}
	}
	lines := it.LogTail
	if it.Log != "" {
		lines = tail(it.Log, 15)
	}
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	if len(lines) > 0 {
		d = append(d, [2]string{"", ""}, [2]string{"log", "last lines"})
		for _, l := range lines {
			d = append(d, [2]string{"", l})
		}
	}
	return d
}

func short(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func (m Model) agentRows() []row {
	t := m.th
	its := m.items()
	sort.SliceStable(its, func(i, j int) bool {
		a, b := its[i], its[j]
		switch agentSorts[m.agentSort] {
		case "beat":
			return m.beatAge(a) < m.beatAge(b)
		case "age":
			return a.Age > b.Age
		case "issue":
			return a.Source+a.ID < b.Source+b.ID
		}
		return indexOf(stageOrder, stageOf(a)) < indexOf(stageOrder, stageOf(b))
	})
	var out []row
	if r, ok := m.orchestratorRow(); ok {
		out = append(out, r)
	}
	for _, it := range its {
		st := stageOf(it)
		issue := it.Source
		if issue == "" || strings.Contains(issue, "/") {
			issue = it.ID
		}
		branch := it.Branch
		if i := strings.LastIndex(branch, "/"); i >= 0 {
			branch = branch[i+1:]
		}
		beat := m.beat(it)
		bc := t.muted
		if m.beatAge(it) > 15*time.Minute && st != "merged" && st != "queued" {
			bc = t.err
		}
		pr := shortPR(it.PR)
		logPath := it.Log
		if m.snap.Demo {
			logPath = ""
		}
		out = append(out, row{
			cells: []cell{{s: issue, c: t.text, bold: true}, {s: or(it.Title), c: t.text}, {s: or(it.Repo), c: t.muted}, {s: or(branch), c: t.muted},
				{s: it.Model, c: t.muted}, {s: or(it.Speed), c: t.muted}, {s: st, c: m.stageColor(st), bold: true}, m.ciCell(it.CI),
				{s: pr, c: t.accent}, {s: beat, c: bc}, {s: it.Age, c: t.muted}},
			chip: st, chipC: m.stageColor(st),
			search: it.Batch + " " + it.Branch, url: it.PR, copy: firstNonDash(it.PR, it.Worktree, it.Branch), title: it.Batch + "/" + it.ID,
			detail: m.itemDetail(it), logPath: logPath, logTail: it.LogTail, timeline: m.timeline(it),
			cmd: "smithy cards get " + sh(it.Batch) + " " + sh(it.ID)})
	}
	return out
}

// orchestratorRow is the running serve orchestrator (kind orchestrator), from serve.json or
// orchestrator.json; ok is false when none runs.
func (m Model) orchestratorRow() (row, bool) {
	t := m.th
	sv := m.snap.Watcher.Serve
	if !sv.SessionOn || sv.SessionPID <= 0 {
		return row{}, false
	}
	elapsed := "-"
	if !sv.SessionAt.IsZero() {
		elapsed = dur(m.snap.At.Sub(sv.SessionAt))
	}
	dir := filepath.Base(sv.SessionDir)
	if sv.SessionDir == "" {
		dir = "-"
	}
	logPath := sv.SessionLog
	if m.snap.Demo {
		logPath = ""
	}
	return row{
		cells: []cell{{s: "serve", c: t.text, bold: true}, {s: fmt.Sprintf("orchestrator · %s · pid %d · %s", or(sv.Agent), sv.SessionPID, dir), c: t.text},
			{s: "-", c: t.muted}, {s: "-", c: t.muted}, {s: "-", c: t.muted}, {s: "-", c: t.muted},
			{s: "orchestr", c: t.accent, bold: true}, {s: "-", c: t.muted}, {s: "-", c: t.muted}, {s: "-", c: t.muted}, {s: elapsed, c: t.muted}},
		chip: "orchestrator", chipC: t.accent, search: "orchestrator serve " + sv.SessionDir, copy: sv.SessionLog,
		title: "orchestrator pid " + fmt.Sprint(sv.SessionPID),
		detail: [][2]string{{"kind", "orchestrator"}, {"agent", or(sv.Agent)}, {"pid", fmt.Sprint(sv.SessionPID)}, {"elapsed", elapsed},
			{"task dir", or(home(sv.SessionDir))}, {"session log", or(home(sv.SessionLog))}},
		logPath: logPath,
		explain: []string{"The headless session smithy auto serve started to run tend auto once on the task dir.",
			"l opens its session log. Stopping serve never stops it."}}, true
}

func firstNonDash(ss ...string) string {
	for _, s := range ss {
		if s != "" && s != "-" {
			return s
		}
	}
	return ""
}

func indexOf(l []string, s string) int {
	for i, x := range l {
		if x == s {
			return i
		}
	}
	return len(l)
}

func (m Model) incomingRows() []row {
	t := m.th
	var out []row
	for _, q := range m.snap.Queue {
		body := strings.Split(strings.TrimSpace(q.Body), "\n")
		if len(body) > 8 {
			body = append(body[:8], "…")
		}
		d := [][2]string{{"source", q.Source}, {"key", or(q.Key)}, {"issue", q.ID}, {"title", q.Title}, {"model", or(q.Model)},
			{"speed", or(q.Speed)}, {"queued", q.QueuedAt.Local().Format("2006-01-02 15:04")}, {"url", q.URL}, {"", ""}}
		for i, l := range body {
			k := ""
			if i == 0 {
				k = "text"
			}
			d = append(d, [2]string{k, l})
		}
		out = append(out, row{cells: []cell{{s: q.ID, c: t.text, bold: true}, {s: q.Title, c: t.text}, {s: q.Source, c: t.muted},
			{s: or(q.Model), c: t.muted}, {s: or(q.Speed), c: t.muted}, {s: ago(m.snap.At.Sub(q.QueuedAt)), c: t.muted}},
			url: q.URL, copy: q.URL, title: q.ID + " · " + q.Title, detail: d, chip: "queued", chipC: t.accent,
			explain: []string{"Waits for the daily cap and the hours window; smithy auto take starts it."}})
	}
	return out
}

func (m Model) needRows() []row {
	t := m.th
	var out []row
	for n, d := range m.snap.Decisions {
		f := strings.Split(d, " · ")
		q, by, why, ref, rejected := d, "self", "", "", ""
		for j, v := range f {
			switch {
			case j == 1:
				by = strings.TrimPrefix(v, "by ")
			case j == 2:
				q = v
			case strings.HasPrefix(v, "why: "):
				why = strings.TrimPrefix(v, "why: ")
			case strings.HasPrefix(v, "ref: "):
				ref = strings.TrimPrefix(v, "ref: ")
			case strings.HasPrefix(v, "rejected: "):
				rejected = strings.TrimPrefix(v, "rejected: ")
			case j == 3:
				why = v
			}
		}
		question, choice, _ := strings.Cut(q, " → ")
		byWord := map[string]string{"timeout": "the agent, after nobody answered in time", "self": "the agent on its own",
			"consult": "the agent, after asking smithy consult"}[by]
		if byWord == "" {
			byWord = by
		}
		chip, chipC, what := "decide", t.warn, "Keep this decision? "+q
		if rejected != "" {
			chip, chipC, what = "rejected", t.err, "Rejected, waiting for the agent: "+q
		}
		cmd := fmt.Sprintf("smithy decisions confirm %d", n+1)
		det := [][2]string{{"question", question}, {"decided", or(choice)}, {"by", byWord}, {"why", or(why)}}
		if ref != "" {
			det = append(det, [2]string{"ref", ref})
		}
		if rejected != "" {
			det = append(det, [2]string{"you said", rejected})
		}
		if len(f) > 0 {
			det = append(det, [2]string{"date", f[0]})
		}
		out = append(out, row{cells: []cell{{s: "?", c: chipC, bold: true}, {s: what, c: t.text}, {s: "decided by " + by, c: t.muted}, {s: "a accept · x reject", c: t.accent}},
			url: ref, copy: q, cmd: cmd, title: "Decision " + fmt.Sprint(n+1) + ": " + question, detail: det, search: "decision " + chip,
			decision: n + 1, chip: chip, chipC: chipC,
			explain: []string{
				"An agent had to choose without you and went on with: " + or(choice) + ".",
				"a accepts it (smithy decisions confirm " + fmt.Sprint(n+1) + "): nothing changes, the entry is closed.",
				"x rejects it with one line of why: the entry stays pending with your reason, and the agent",
				"that took it sees it in smithy decisions pending and decides again.",
			}})
	}
	for _, it := range m.items() {
		switch st := stageOf(it); st {
		case "ready", "pr_open":
			if st == "pr_open" && it.CI != "green" {
				continue
			}
			cmd := "gh pr merge " + sh(it.PR)
			out = append(out, row{cells: []cell{{s: t.g.pass, c: t.ok, bold: true}, {s: "Review PR " + shortPR(it.PR) + ": " + or(it.Title), c: t.text},
				{s: it.Batch + "/" + it.ID, c: t.muted}, {s: "o open · y merge cmd", c: t.accent}},
				url: it.PR, copy: it.PR, cmd: cmd, title: "PR " + shortPR(it.PR) + " · " + or(it.Title), detail: m.itemDetail(it), logTail: it.LogTail,
				search: "pr review merge", chip: "ready for review", chipC: t.ok,
				explain: []string{"CI is " + it.CI + ". Review it on GitHub (o), then merge there or with the command y copies:",
					cmd, "smithy never merges from here."}})
		case "blocked":
			cmd := fmt.Sprintf("smithy cards set %s %s state=working %s", sh(it.Batch), sh(it.ID), sh("note=unblocked: <what changed>"))
			logPath := it.Log
			if m.snap.Demo {
				logPath = ""
			}
			out = append(out, row{cells: []cell{{s: t.g.fail, c: t.err, bold: true}, {s: "Unblock " + it.ID + ": " + or(it.Text), c: t.text},
				{s: it.Batch + "/" + it.ID, c: t.muted}, {s: "l log · y resume cmd", c: t.accent}},
				url: it.PR, copy: or(it.Worktree), cmd: cmd, title: it.Batch + "/" + it.ID + " · " + or(it.Title), detail: m.itemDetail(it),
				logPath: logPath, logTail: it.LogTail, search: "blocked stuck", chip: it.Stage, chipC: t.err,
				explain: []string{"The worker stopped: " + or(it.Text) + ".", "Fix the cause (worktree " + or(it.Worktree) + "), then resume it with the command y copies."}})
		}
	}
	return out
}

// feed is the event list for Activity: the event log, else what can be derived from the state.
func (m Model) feed() []events.Event {
	if len(m.snap.Events) > 0 {
		return m.snap.Events
	}
	var out []events.Event
	for i := len(m.snap.Activity) - 1; i >= 0; i-- {
		a := m.snap.Activity[i]
		out = append(out, events.Event{T: a.At, Kind: a.Kind, Detail: a.Text, PR: a.URL})
	}
	return out
}

func (m Model) kindLook(k, detail string) (string, color.Color) {
	t := m.th
	switch k {
	case "poll":
		return t.g.off, t.muted
	case "queued":
		return "+", t.accent
	case "skipped":
		return "-", t.muted
	case "launched":
		return t.g.half, t.warn
	case "ci":
		c := m.ciCell(detail)
		return strings.Fields(c.s)[0], c.c
	case "merged", "report":
		return t.g.pass, t.ok
	case "stage":
		if detail == "blocked" || detail == "failed" || detail == "needs_decision" {
			return t.g.fail, t.err
		}
		return t.g.on, t.accent
	}
	return t.g.dot, t.muted
}

func (m Model) activityRows() []row {
	t := m.th
	es := m.feed()
	want := activityTypes[m.actType]
	var out []row
	for i := len(es) - 1; i >= 0 && len(out) < 500; i-- {
		e := es[i]
		if want != "all" && e.Kind != want {
			continue
		}
		icon, c := m.kindLook(e.Kind, e.Detail)
		what := e.Detail
		switch e.Kind {
		case "queued", "launched", "skipped", "report":
			what = strings.TrimSpace(e.ID + " " + e.Detail)
		case "stage", "ci", "merged":
			what = e.Item + " " + e.Detail
			if e.PR != "" {
				what += " " + shortPR(e.PR)
			}
		}
		where := e.Source
		if e.Batch != "" {
			where = e.Batch
		}
		out = append(out, row{cells: []cell{{s: ago(m.snap.At.Sub(e.T)), c: t.muted}, {s: icon + " " + e.Kind, c: c}, {s: what, c: t.text}, {s: where, c: t.muted}},
			url: e.PR, copy: e.PR, title: "Event", search: e.Kind,
			detail: [][2]string{{"time", e.T.Local().Format("2006-01-02 15:04:05")}, {"kind", e.Kind}, {"what", what}, {"source", or(e.Source)},
				{"issue", or(e.ID)}, {"batch", or(e.Batch)}, {"item", or(e.Item)}, {"pr", or(e.PR)}}})
	}
	return out
}

func home(p string) string {
	h, _ := os.UserHomeDir()
	if h != "" && strings.HasPrefix(p, h+"/") {
		return "~" + p[len(h):]
	}
	return p
}

func (m Model) worktreeRows() []row {
	t := m.th
	var out []row
	for _, w := range m.snap.Worktrees {
		dirty := cell{s: "clean", c: t.muted}
		if w.Dirty {
			dirty = cell{s: "dirty", c: t.warn}
		}
		state := cell{s: "live", c: t.ok}
		if w.Orphan {
			state = cell{s: "orphaned", c: t.warn, bold: true}
		}
		age := "-"
		if w.Age > 0 {
			age = ago(w.Age)
		}
		ab := fmt.Sprintf("%d↑ %d↓", w.Ahead, w.Behind)
		d := [][2]string{{"path", w.Path}, {"branch", or(w.Branch)}, {"item", or(w.Item)}, {"age", age}, {"state", state.s + ", " + dirty.s},
			{"ahead/behind", ab}, {"", ""}, {"git status", ""}}
		if len(w.Status) == 0 {
			d = append(d, [2]string{"", "clean"})
		}
		for _, l := range w.Status {
			d = append(d, [2]string{"", l})
		}
		out = append(out, row{cells: []cell{{s: home(w.Path), c: t.text}, {s: or(w.Branch), c: t.muted}, {s: or(w.Item), c: t.muted},
			{s: age, c: t.muted}, dirty, {s: ab, c: t.muted}, state},
			copy: w.Path, cmd: "git worktree remove " + sh(w.Path), title: home(w.Path), detail: d, chip: state.s, chipC: state.c})
	}
	return out
}

func (m Model) planRows() []row {
	t := m.th
	var out []row
	for _, tl := range m.snap.Tasks {
		done := tl.Counts["done"]
		bar := ""
		if tl.Total > 0 {
			n := done * 10 / tl.Total
			bar = strings.Repeat(t.g.shades[3], n) + strings.Repeat(t.g.shades[0], 10-n)
		}
		c := t.muted
		if tl.Counts["doing"] > 0 {
			c = t.warn
		} else if tl.Total > 0 && done == tl.Total {
			c = t.ok
		}
		d := [][2]string{{"dir", tl.Dir}}
		for _, s := range statuses(tl.Counts) {
			d = append(d, [2]string{s, fmt.Sprint(tl.Counts[s])})
		}
		spec, ok := tl.Text["spec.md"]
		if !ok && tl.Text == nil {
			if b, err := os.ReadFile(filepath.Join(tl.Dir, "spec.md")); err == nil {
				spec = strings.Join(cleanAll(strings.Split(string(b), "\n")), "\n")
			}
		}
		if spec == "" {
			var b strings.Builder
			fmt.Fprintf(&b, "# %s\n\nNo spec.md in %s. Its tasks:\n\n", filepath.Base(tl.Dir), tl.Dir)
			for _, tk := range tl.Tasks {
				fmt.Fprintf(&b, "- **%s** %s · %s\n", tk.NN, tk.Title, tk.Status)
			}
			spec = b.String()
		}
		out = append(out, row{cells: []cell{{s: filepath.Base(tl.Dir), c: t.text, bold: true}, {s: bar, c: c}, {s: fmt.Sprintf("%d/%d", done, tl.Total), c: t.muted}},
			copy: tl.Dir, title: "Plan " + filepath.Base(tl.Dir), detail: d, md: spec, mdTitle: home(filepath.Join(tl.Dir, "spec.md"))})
	}
	return out
}

// doneRows is finished work, newest first: from the event log, plus finished cards it lacks.
func (m Model) doneRows() []row {
	t := m.th
	type job struct {
		issue, title, pr, result, batch, item string
		start, end                            time.Time
	}
	titles := map[string]string{}
	issues := map[string]string{}
	starts := map[string]time.Time{}
	jobs := map[string]*job{}
	var order []string
	for _, e := range m.snap.Events {
		k := e.Batch + "/" + e.Item
		switch {
		case e.Kind == "queued":
			titles[e.Source+":"+e.ID] = e.Detail
		case e.Kind == "launched":
			starts[k] = e.T
			issues[k] = e.ID
			if tt := titles[e.Source+":"+e.ID]; tt != "" {
				titles[k] = tt
			} else if e.Detail != "" {
				titles[k] = e.Detail
			}
		case e.Kind == "merged" || (e.Kind == "stage" && (e.Detail == "ready" || e.Detail == "blocked" || e.Detail == "dropped" || e.Detail == "failed")):
			res := map[string]string{"merged": "merged", "ready": "ready", "blocked": "blocked", "failed": "blocked", "dropped": "closed"}[e.Detail]
			if e.Kind == "merged" {
				res = "merged"
			}
			j, ok := jobs[k]
			if !ok {
				j = &job{batch: e.Batch, item: e.Item}
				jobs[k] = j
				order = append(order, k)
			}
			j.result, j.end = res, e.T
			if e.PR != "" {
				j.pr = e.PR
			}
		}
	}
	for _, it := range m.items() {
		k := it.Batch + "/" + it.ID
		res := map[string]string{"merged": "merged", "reported": "merged", "dropped": "closed"}[it.Stage]
		if _, ok := jobs[k]; ok || res == "" || it.Updated == nil {
			continue
		}
		end, _ := time.Parse(time.RFC3339, *it.Updated)
		jobs[k] = &job{batch: it.Batch, item: it.ID, result: res, end: end, pr: it.PR}
		order = append(order, k)
	}
	var js []*job
	for _, k := range order {
		j := jobs[k]
		j.issue, j.title, j.start = issues[k], titles[k], starts[k]
		for _, it := range m.items() {
			if it.Batch == j.batch && it.ID == j.item && j.title == "" {
				j.title = it.Title
			}
		}
		js = append(js, j)
	}
	sort.SliceStable(js, func(a, b int) bool { return js[a].end.After(js[b].end) })
	var out []row
	for _, j := range js {
		took := "-"
		if !j.start.IsZero() && j.end.After(j.start) {
			took = dur(j.end.Sub(j.start))
		}
		c := map[string]color.Color{"merged": t.ok, "ready": t.accent, "closed": t.muted, "blocked": t.err}[j.result]
		issue := or(j.issue)
		if issue == "-" {
			issue = j.item
		}
		pr := shortPR(or(j.pr))
		out = append(out, row{cells: []cell{{s: issue, c: t.text, bold: true}, {s: or(j.title), c: t.text}, {s: pr, c: t.accent},
			{s: j.result, c: c, bold: true}, {s: took, c: t.muted}, {s: ago(m.snap.At.Sub(j.end)), c: t.muted}},
			url: j.pr, copy: firstNonDash(j.pr, j.batch+"/"+j.item), title: issue + " · " + or(j.title), chip: j.result, chipC: c,
			detail: [][2]string{{"issue", issue}, {"title", or(j.title)}, {"batch", j.batch}, {"item", j.item}, {"pr", or(j.pr)},
				{"result", j.result}, {"started", stampOr(j.start)}, {"finished", stampOr(j.end)}, {"took", took}}})
	}
	return out
}

func stampOr(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}
