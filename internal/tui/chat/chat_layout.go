//go:build harness

package chat

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

// sideMin is the narrowest screen that shows the sidebar.
const sideMin = 120

// sideW is the sidebar's width: 34 columns, 40 from 160 columns on.
func (m chatView) sideW() int {
	if m.width >= 160 {
		return 40
	}
	return 34
}

// activityLabel is a collapsed run of tool calls and thoughts as one line: "+ 2 commands, 1 thought".
func activityLabel(tools []chatTool, thoughts int) string {
	kinds := []string{"command", "edit", "read", "tool"}
	n := map[string]int{}
	for _, tl := range tools {
		switch tl.Name {
		case "Bash", "Shell":
			n["command"]++
		case "Edit", "Write", "MultiEdit", "Diff":
			n["edit"]++
		case "Read", "Grep", "Glob", "LS":
			n["read"]++
		default:
			n["tool"]++
		}
	}
	kinds, n["thought"] = append(kinds, "thought"), thoughts
	var parts []string
	for _, k := range kinds {
		if c := n[k]; c > 0 {
			if c > 1 {
				k += "s"
			}
			parts = append(parts, fmt.Sprintf("%d %s", c, k))
		}
	}
	return "+ " + strings.Join(parts, ", ")
}

// ctxWindows is each model family's context window in tokens, matched by substring in order.
var ctxWindows = []struct {
	match string
	size  int64
}{
	{"[1m]", 1_000_000}, {"claude", 200_000}, {"opus", 200_000}, {"sonnet", 200_000}, {"haiku", 200_000},
	{"gpt-5", 400_000}, {"gpt-4.1", 1_000_000}, {"gpt-4o", 128_000}, {"o3", 200_000}, {"o4", 200_000},
	{"gemini", 1_000_000}, {"deepseek", 128_000}, {"qwen", 128_000}, {"kimi", 256_000}, {"glm", 128_000},
}

// ctxWindow is model's context window; 0 when unknown.
func ctxWindow(model string) int64 {
	model = strings.ToLower(model)
	for _, c := range ctxWindows {
		if strings.Contains(model, c.match) {
			return c.size
		}
	}
	return 0
}

// kTokUp is a token count with a K or M suffix, as the context label shows it.
func kTokUp(n int64) string { return strings.ToUpper(kTok(n)) }

// ctxLabel is the context in use, "29.4K (3%)" with the share of model's window when known.
func ctxLabel(used int64, model string) string {
	if used <= 0 {
		return ""
	}
	if w := ctxWindow(model); w > 0 {
		return fmt.Sprintf("%s (%d%%)", kTokUp(used), (used*100+w/2)/w)
	}
	return kTokUp(used)
}

// turnFooter is the dim line under an answer: agent, model, how long the turn took, tok/s.
func turnFooter(agent, model string, took time.Duration, tps float64, sep string) string {
	var parts []string
	for _, s := range []string{agent, model} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	parts = append(parts, fmt.Sprintf("%.1fs", took.Seconds()))
	if tps >= 1 {
		parts = append(parts, fmt.Sprintf("%.0f tok/s", tps))
	}
	return strings.Join(parts, sep)
}

// session is the open session's state; ok false before the first message.
func (m chatView) session() (chatSession, bool) {
	for _, s := range m.sessions {
		if s.id == m.current && s.id != "" {
			return s, true
		}
	}
	return chatSession{}, false
}

// agentModel is "agent · model" for the open session, else the default.
func (m chatView) agentModel() string {
	if s, ok := m.session(); ok && s.agent != "" {
		return strings.Join(nonEmpty(s.agent, s.model), " "+m.th.G.Dot+" ")
	}
	return m.agentLabel()
}

// curModel is the open session's model, else the default one.
func (m chatView) curModel() string {
	if s, ok := m.session(); ok && s.model != "" {
		return s.model
	}
	if m.setupSt != nil {
		return m.setupSt.Default.Model
	}
	return ""
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ctxNow is the open session's context label for the footer and sidebar.
func (m chatView) ctxNow() string { return ctxLabel(m.ctx[m.current], m.curModel()) }

// sideOn reports whether the sidebar shows: wide enough and not turned off with /sidebar.
func (m chatView) sideOn() bool {
	if m.sideOff || m.width < sideMin {
		return false
	}
	s, _ := m.session()
	return len(m.pulse()) > 0 || m.ctx[m.current] > 0 || s.links != nil || len(s.prs) > 0 || len(m.queues[m.current]) > 0
}

// tw is the transcript's width: the screen less the sidebar when it shows.
func (m chatView) tw() int {
	w := max(m.width, 20)
	if m.sideOn() {
		w -= m.sideW() + sideGap
	}
	return w
}

// sideGap is the fixed gutter between the transcript's right edge and the sidebar.
const sideGap = 2

// sideX is the sidebar's first column: it ends on the page's right edge, the gutter before it.
func (m chatView) sideX() int { return max(m.width, 20) - m.pad() - m.sideW() }

// tabHit is where one tab sits in the tab strip: columns [x0, x1) open session id ("" is the
// unsent new session).
type tabHit struct {
	x0, x1 int
	id     string
}

// tabStrip is the row under the header bar: the sessions of this folder as tabs, the open one
// raised, bold and underlined, and a + that starts a new one. It returns where each tab and
// the + sit for clicks.
func (m chatView) tabStrip(w int) (string, []tabHit, int) {
	t := m.th
	p := strings.Repeat(" ", m.pad())
	type tab struct{ id, text string }
	var tabs []tab
	for _, s := range m.listed() {
		title := tabTitle(s.title)
		if title == "" {
			title = kit.Short(s.id, 8)
		}
		text := " " + title + " "
		if m.sessionRunning(s.id) {
			text = " " + t.Spinner(m.frame) + text
		}
		tabs = append(tabs, tab{s.id, text})
	}
	if m.current == "" {
		tabs = append(tabs, tab{"", " new "})
	}
	cur := 0
	for i, tb := range tabs {
		if tb.id == m.current {
			cur = i
		}
	}
	room := w - len(p) - 4 - 4
	width := func(a, b int) int {
		n := 0
		for _, tb := range tabs[a:b] {
			n += ansi.StringWidth(tb.text) + 1
		}
		return n
	}
	lo, hi := 0, len(tabs)
	for width(lo, hi) > room && hi-lo > 1 {
		if cur-lo > hi-1-cur {
			lo++
		} else {
			hi--
		}
	}
	row := p
	if lo > 0 {
		row += t.Fg(t.Accent).Render("« ")
	}
	var hits []tabHit
	for _, tb := range tabs[lo:hi] {
		x := ansi.StringWidth(row)
		if tb.id == m.current {
			row += m.chip(tb.text, t.Surface, t.Accent, true)
		} else {
			row += m.chip(tb.text, t.Muted, t.Raised, false)
		}
		hits = append(hits, tabHit{x, ansi.StringWidth(row), tb.id})
		row += " "
	}
	if hi < len(tabs) {
		row += t.Fg(t.Accent).Render("» ")
	}
	x := ansi.StringWidth(row)
	row += m.chip(" + ", t.Accent, t.Raised, true)
	if ansi.StringWidth(row) > w {
		row = ansi.Truncate(row, w, "")
	}
	return row, hits, x
}

// tabFiller are openings a tab title drops: they say nothing about the session.
var tabFiller = []string{"please ", "can you ", "could you ", "would you ", "i want you to ", "i want to ", "i need you to ", "help me ", "let's ", "lets ", "go ahead and ", "now "}

// tabTitle is a session's tab label: its title without polite openings, cut at a word near 24
// cells, or with a middle ellipsis when one word is longer.
func tabTitle(title string) string {
	s := strings.Join(strings.Fields(kit.Clean(title)), " ")
	for changed := true; changed; {
		changed = false
		for _, f := range tabFiller {
			if len(s) > len(f) && strings.EqualFold(s[:len(f)], f) {
				s, changed = s[len(f):], true
			}
		}
	}
	s = strings.TrimRight(s, " ?.!,:;")
	if s == "" {
		return ""
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	const most = 24
	if ansi.StringWidth(s) <= most {
		return s
	}
	out := ""
	for _, wd := range strings.Fields(s) {
		if ansi.StringWidth(out)+1+ansi.StringWidth(wd) > most-1 {
			break
		}
		out = strings.TrimSpace(out + " " + wd)
	}
	if ansi.StringWidth(out) >= 10 {
		return strings.TrimRight(out, " ,.:;") + "…"
	}
	r := []rune(s)
	return string(r[:most/2-1]) + "…" + string(r[len(r)-(most/2-1):])
}

// sidebar is the right column beside the transcript, h rows: the session's title, its context
// and spend, and the PRs and issues linked to it.
func (m chatView) sideRows() (rows, urls []string) {
	t := m.th
	s, _ := m.session()
	dim, txt := t.Fg(t.Dim), t.Fg(t.Text)
	iw := m.sideW() - 4
	head := func(n string) string { return t.Fg(t.Accent).Bold(true).Render(n) }
	two := func(l, r string) string { return spread(l, r, iw) }
	turns := m.pulse()
	rows = []string{head("pulse")}
	if len(turns) > 0 {
		if vals := turns[max(len(turns)-iw, 0):]; len(vals) > 1 {
			rows = append(rows, t.Fg(t.Accent).Render(kit.Sparkline(vals, len(vals), t.G.Ticks)))
		}
		n := 0
		for _, v := range turns {
			n += v
		}
		rows = append(rows, dim.Render(fmt.Sprintf("%d turn%s %s %d step%s", len(turns), plural(len(turns)), t.G.Dot, n, plural(n))))
	} else {
		rows = append(rows, dim.Render("no turns yet"))
	}
	if m.branch != "" {
		rows = append(rows, dim.Render(kit.Short("on "+m.branch, iw)))
	}
	rows = append(rows, "", head("heat"))
	used, win := m.ctx[m.current], ctxWindow(m.curModel())
	switch {
	case used > 0 && win > 0:
		pct := int((used*100 + win/2) / win)
		rows = append(rows, m.meter(iw, float64(used)/float64(win)))
		rows = append(rows, two(txt.Render(kTokUp(used))+dim.Render(" of "+kTokUp(win)), t.Fg(t.Strong).Render(fmt.Sprintf("%d%%", pct))))
	case used > 0:
		rows = append(rows, txt.Render(kTokUp(used)+" tokens"))
	default:
		rows = append(rows, dim.Render("no context used yet"))
	}
	if c, ok := m.cost[m.current]; ok {
		r := t.Fg(t.Strong).Render(fmt.Sprintf("$%.2f", c))
		if all := m.totalCost(); all > c+0.005 {
			r += dim.Render(fmt.Sprintf(" of $%.2f", all))
		}
		rows = append(rows, two(dim.Render("spent"), r))
	}
	if work, files := m.workDone(iw); work != "" {
		rows = append(rows, "", head("work"), txt.Render(work))
		for _, f := range files {
			rows = append(rows, t.Fg(t.Info).Render(t.G.On+" ")+dim.Render(f))
		}
	}
	if q := m.queues[m.current]; len(q) > 0 {
		rows = append(rows, "", head("queued"))
		for i, x := range q[:min(len(q), 3)] {
			rows = append(rows, t.Fg(t.Accent).Render(fmt.Sprintf("%d ", i+1))+txt.Render(kit.Short(kit.Clean(x), iw-2)))
		}
	}
	lr, lu := m.sideLinks(s, iw)
	if len(lu) > 0 && !(len(lr) == 1 && lu[0] == "" && strings.Contains(ansi.Strip(lr[0]), "no PRs")) {
		rows = append(rows, "", head("links"))
		urls = append(make([]string, len(rows)), lu...)
		rows = append(rows, lr...)
	}
	return rows, urls
}

// workDone is what the open session did: its tool calls counted by kind, and the last files it
// edited, each cut to w.
func (m chatView) workDone(w int) (string, []string) {
	var tools []chatTool
	var files []string
	for _, c := range m.msgs {
		if c.tool == nil {
			continue
		}
		tools = append(tools, *c.tool)
		switch c.tool.Name {
		case "Edit", "Write", "MultiEdit":
			if f := c.tool.Arg; f != "" && !slices.Contains(files, f) {
				files = append(files, f)
			}
		}
	}
	if len(tools) == 0 {
		return "", nil
	}
	if len(files) > 4 {
		files = files[len(files)-4:]
	}
	for i, f := range files {
		files[i] = kit.Short(f, w-2)
	}
	return strings.TrimPrefix(activityLabel(tools, 0), "+ "), files
}

func (m chatView) sidebar(h int) []string {
	t := m.th
	iw := m.sideW() - 4
	rows, _ := m.sideRows()
	note := m.whereNote()
	out := make([]string, h)
	side := on(t.Raised)
	for i := range out {
		r := ""
		if i > 0 && i-1 < len(rows) {
			r = rows[i-1]
		}
		if k := len(note) - (h - i); k >= 0 && k < len(note) && i > len(rows) {
			r = t.Fg(t.Dim).Render(note[k])
		}
		out[i] = fill(side, m.sideW(), "  "+ansi.Truncate(r, iw, "…"))
	}
	return out
}

// meter is a flat bar w cells wide, frac of it in the accent on the overlay tier; without colour
// it is a run of # and . cells.
func (m chatView) meter(w int, frac float64) string {
	t := m.th
	n := max(0, min(w, int(frac*float64(w)+0.5)))
	if t.Plain || t.Accent == nil {
		return strings.Repeat("#", n) + strings.Repeat(".", w-n)
	}
	return on(t.Accent).Render(strings.Repeat(" ", n)) + on(t.Overlay).Render(strings.Repeat(" ", w-n))
}

// besideSide pads each transcript row to tw and puts the sidebar to the right of the first n
// rows, so it ends on the input band's last row.
func (m chatView) besideSide(body []string, n int) []string {
	if !m.sideOn() {
		return body
	}
	n = min(n, len(body))
	x, side := m.sideX(), m.sidebar(n)
	p := strings.Repeat(" ", m.pad())
	for i, r := range body[:n] {
		r = ansi.Truncate(r, x, "")
		body[i] = r + strings.Repeat(" ", max(x-ansi.StringWidth(r), 0)) + side[i] + p
	}
	return body
}

// qaRows is an answered question kept in the transcript: the question dim, the answer below.
func (m chatView) qaRows(c *chatMsg, w int) []string {
	t := m.th
	ans := m.chip(" "+kit.Short(c.ans, 30)+" ", t.Surface, t.Accent, true)
	q := t.Fg(t.Text).Render(kit.Short(c.text, m.textWidth(w)-ansi.StringWidth(ans)-4))
	return []string{m.spreadLane(w, t.Fg(t.Warn).Render("?"), m.railMark(false), t.Border, q, ans, nil), m.gap(w)}
}

// answered reports whether session id's turn has agent text since your last message and no
// footer yet, so a footer goes only under an answer, once.
func (m chatView) answered(id string) bool {
	msgs := m.convs[id]
	if id == m.current {
		msgs = m.msgs
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		switch c := msgs[i]; {
		case c.who == "you", c.kind == "foot":
			return false
		case c.who == "agent":
			return true
		}
	}
	return false
}

// keepQA leaves the answered question id in the transcript as a compact Q → A block, once:
// whichever comes first, our post's reply or the daemon's answered event.
func (m *chatView) keepQA(id, answer string) {
	for _, d := range m.pending {
		if d.ID == id && answer != "" && d.Session != "" {
			m.into(d.Session, chatMsg{who: "daemon", kind: "qa", text: d.Question, ans: answer})
		}
	}
}

// sideLinks is the sidebar's Links block: the live PRs, issues and URLs when the daemon sent
// them, else the PR URLs the session recorded. urls says, per row, which link a click opens;
// a link's first row ends in a × that removes it.
func (m chatView) sideLinks(s chatSession, iw int) (rows, urls []string) {
	t := m.th
	dim, link := t.Fg(t.Dim), t.Fg(t.Info)
	x := dim.Render("×")
	head := func(u, label string, c color.Color, state string) {
		r := t.Fg(c).Render(t.G.On+" ") + link.Render(label+" ") + t.Fg(t.Text).Render(state)
		r = ansi.Truncate(r, iw-2, "…")
		rows = append(rows, r+strings.Repeat(" ", max(iw-2-ansi.StringWidth(r), 0))+" "+x)
		urls = append(urls, u)
	}
	sub := func(u, text string) {
		rows, urls = append(rows, dim.Render(kit.Short(text, iw))), append(urls, u)
	}
	if l := s.links; l != nil {
		for _, p := range l.PRs {
			col, badge := t.Muted, ""
			switch p.Checks {
			case "green":
				col, badge = t.Ok, dim.Render("ci ")+t.Fg(t.Ok).Render(t.G.Pass)
			case "red":
				col, badge = t.Err, dim.Render("ci ")+t.Fg(t.Err).Render(t.G.Fail)
			case "":
			default:
				col, badge = t.Warn, dim.Render("ci ")+t.Fg(t.Warn).Render(t.Spinner(m.frame))
			}
			head(p.URL, fmt.Sprintf("PR #%d", p.Number), col, kit.Short(p.Title, iw-12))
			state := dim.Render(strings.ToLower(p.State))
			if badge != "" {
				state = badge + dim.Render(" "+t.G.Dot+" ") + state
			}
			rows, urls = append(rows, "  "+state), append(urls, p.URL)
		}
		for _, is := range l.Issues {
			head(is.URL, is.ID, t.Muted, is.Status)
			sub(is.URL, is.Title)
		}
		for _, u := range l.URLs {
			head(u.URL, u.Host, t.Muted, "")
			sub(u.URL, u.Title)
		}
		if l.Error != "" {
			for _, e := range strings.Split(ansi.Wordwrap(l.Error, iw, ""), "\n") {
				rows, urls = append(rows, t.Fg(t.Muted).Render(e)), append(urls, "")
			}
		}
	} else {
		for _, u := range s.prs {
			rows, urls = append(rows, link.Render(kit.Short(strings.TrimPrefix(u, "https://github.com/"), iw))), append(urls, u)
		}
	}
	if len(rows) == 0 {
		rows, urls = append(rows, dim.Render("no PRs or issues yet")), append(urls, "")
	}
	return rows, urls
}

// pulse is the open session's work per turn: tool calls and answers between your prompts, the
// session's rhythm for the sidebar's sparkline.
func (m chatView) pulse() []int {
	var out []int
	for _, c := range m.msgs {
		switch {
		case c.who == "you":
			out = append(out, 0)
		case len(out) > 0 && (c.tool != nil || c.who == "agent" || c.sh != nil):
			out[len(out)-1]++
		}
	}
	return out
}
