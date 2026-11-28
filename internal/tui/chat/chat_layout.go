//go:build harness

package chat

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

// sideW is the right sidebar's width; sideMin the narrowest screen that shows it.
const (
	sideW   = 38
	sideMin = 120
)

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
func (m chatView) sideOn() bool { return !m.sideOff && m.width >= sideMin }

// tw is the transcript's width: the screen less the sidebar when it shows.
func (m chatView) tw() int {
	w := max(m.width, 20)
	if m.sideOn() {
		w -= sideW
	}
	return w
}

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
	p := strings.Repeat(" ", max(m.pad()-1, 0))
	row := p
	var hits []tabHit
	add := func(id, title string) {
		tab := " " + kit.Short(title, 24) + " "
		if id != "" && m.sessionRunning(id) {
			tab = " " + t.Spinner(m.frame) + tab
		}
		x := ansi.StringWidth(row)
		if id == m.current {
			row += m.chip(tab, t.Surface, t.Accent, true)
		} else {
			row += m.chip(tab, t.Muted, t.Raised, false)
		}
		hits = append(hits, tabHit{x, ansi.StringWidth(row), id})
		row += " "
	}
	for _, s := range m.listed() {
		title := s.title
		if title == "" {
			title = kit.Short(s.id, 8)
		}
		add(s.id, title)
	}
	if m.current == "" {
		add("", "new")
	}
	x := ansi.StringWidth(row)
	row += m.chip(" + ", t.Accent, t.Raised, true)
	if over := ansi.StringWidth(row) - w; over > 0 {
		row = ansi.TruncateLeft(row, over+1, "…")
		for i := range hits {
			hits[i].x0, hits[i].x1 = hits[i].x0-over, hits[i].x1-over
		}
		x -= over
	}
	return row, hits, x
}

// sidebar is the right column beside the transcript, h rows: the session's title, its context
// and spend, and the PRs and issues linked to it.
func (m chatView) sideRows() (rows, urls []string) {
	t := m.th
	s, _ := m.session()
	head, dim, txt := t.Fg(t.Accent).Bold(true), t.Fg(t.Dim), t.Fg(t.Text)
	title := s.title
	if title == "" {
		title = "new session"
	}
	iw := sideW - 5
	two := func(l, r string) string { return spread(l, r, iw) }
	rows = []string{t.Fg(t.Strong).Bold(true).Render(kit.Short(title, iw))}
	if am := m.agentModel(); am != "" {
		rows = append(rows, dim.Render(kit.Short(am, iw)))
	}
	rows = append(rows, "")
	used, win := m.ctx[m.current], ctxWindow(m.curModel())
	switch {
	case used > 0 && win > 0:
		pct := int((used*100 + win/2) / win)
		rows = append(rows, two(head.Render("Context"), t.Fg(t.Strong).Render(fmt.Sprintf("%d%%", pct))))
		rows = append(rows, m.meter(iw, float64(used)/float64(win)))
		rows = append(rows, txt.Render(kTokUp(used))+dim.Render(" of "+kTokUp(win)+" tokens"))
	case used > 0:
		rows = append(rows, head.Render("Context"), txt.Render(kTokUp(used)+" tokens"))
	default:
		rows = append(rows, head.Render("Context"), dim.Render("nothing used yet"))
	}
	if c, ok := m.cost[m.current]; ok {
		rows = append(rows, "", head.Render("Spent"))
		rows = append(rows, two(t.Fg(t.Muted).Render("session"), t.Fg(t.Strong).Render(fmt.Sprintf("$%.2f", c))))
		if all := m.totalCost(); all > c+0.005 {
			rows = append(rows, two(t.Fg(t.Muted).Render("all sessions"), t.Fg(t.Strong).Render(fmt.Sprintf("$%.2f", all))))
		}
	}
	rows = append(rows, "", head.Render("Links"))
	lr, lu := m.sideLinks(s, iw)
	urls = append(make([]string, len(rows)), lu...)
	return append(rows, lr...), urls
}

func (m chatView) sidebar(h int) []string {
	t := m.th
	iw := sideW - 5
	rows, _ := m.sideRows()
	out := make([]string, h)
	side := on(t.Raised)
	for i := range out {
		r := ""
		if i > 0 && i-1 < len(rows) {
			r = rows[i-1]
		}
		out[i] = " " + fill(side, sideW-1, "  "+ansi.Truncate(r, iw, "…"))
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

// besideSide pads each transcript row to tw and puts the sidebar to its right.
func (m chatView) besideSide(body []string) []string {
	if !m.sideOn() {
		return body
	}
	tw, side := m.tw(), m.sidebar(len(body))
	for i, r := range body {
		r = ansi.Truncate(r, tw, "")
		body[i] = r + strings.Repeat(" ", max(tw-ansi.StringWidth(r), 0)) + side[i]
	}
	return body
}

// qaRows is an answered question kept in the transcript: the question dim, the answer below.
func (m chatView) qaRows(c *chatMsg, w int) []string {
	t := m.th
	g := strings.Repeat(" ", m.pad())
	bw := max(w-2*m.pad()-6, 10)
	out := []string{g + "  " + t.Fg(t.Warn).Render("→ ") + t.Fg(t.Muted).Render("Asked 1 question")}
	for _, l := range strings.Split(ansi.Wrap(c.text, bw, ""), "\n") {
		out = append(out, g+"    "+t.Fg(t.Text).Render(l))
	}
	out = append(out, g+"    "+t.Fg(t.Accent).Render(t.G.Pass+" ")+t.Fg(t.Strong).Bold(true).Render(c.ans))
	return append(out, "")
}

// footRows is the dim turn footer under an agent's answer.
func (m chatView) footRows(c *chatMsg) []string {
	return []string{strings.Repeat(" ", m.pad()) + "  " + m.th.Fg(m.th.Dim).Render(c.text), ""}
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
			head(p.URL, fmt.Sprintf("#%d", p.Number), col, kit.Short(p.Title, iw-12))
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
			rows, urls = append(rows, t.Fg(t.Err).Render(kit.Short(l.Error, iw))), append(urls, "")
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
