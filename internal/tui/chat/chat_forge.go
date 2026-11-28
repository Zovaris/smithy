//go:build harness

package chat

import (
	"charm.land/lipgloss/v2"
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

// The forge timeline: every transcript row hangs off one vertical rail at the page gutter. A block's
// mark (✓ or ✗ for tools, $ for shell, ? for a question, the spinner) takes the rail's place on its
// first line; text starts two cells right of the rail. Bands (shell output, open tools, the input,
// the ask panel) start on the rail itself and end on the page's right edge, so every left and right
// edge is shared.

// grid is this width's columns: the rail, the text column and the right edge (exclusive).
func (m chatView) grid(w int) (rail, text, right int) {
	p := m.pad()
	return p, p + 2, w - p
}

// railMark is the rail glyph: a thin line, or the heavy one that marks a prompt and the input.
func (m chatView) railMark(heavy bool) string {
	switch {
	case !m.th.G.Unicode:
		return "|"
	case heavy:
		return "┃"
	}
	return "│"
}

// lane is one timeline row w wide: mark right-aligned in the gutter, the rail in railC, then s
// from the text column; with bg set, a band from one cell before the text column to the right edge.
func (m chatView) lane(w int, mark, rail string, railC color.Color, s string, bg color.Color) string {
	t := m.th
	_, tx, right := m.grid(w)
	p := m.pad()
	lead := " "
	switch {
	case mark != "":
		lead = ansi.Truncate(mark, 1, "")
	case rail != "":
		lead = t.Fg(railC).Render(rail)
	}
	if bg == nil {
		return strings.Repeat(" ", p) + lead + " " + ansi.Truncate(s, right-tx, "…")
	}
	return strings.Repeat(" ", p) + fill(on(bg), right-p, lead+" "+s)
}

// spreadLane is a lane whose text has left and right parts, the right part ending on the edge.
func (m chatView) spreadLane(w int, mark, rail string, railC color.Color, left, rightS string, bg color.Color) string {
	_, tx, right := m.grid(w)
	room := right - tx
	return m.lane(w, mark, rail, railC, spread(ansi.Truncate(left, max(room-ansi.StringWidth(rightS)-1, 1), "…"), rightS, room), bg)
}

// gap is the blank row between blocks: the rail runs through it.
func (m chatView) gap(w int) string {
	return m.lane(w, "", m.railMark(false), m.th.Border, "", nil)
}

// textWidth is how wide text may wrap in a lane of width w.
func (m chatView) textWidth(w int) int {
	_, tx, right := m.grid(w)
	return max(right-tx-1, 10)
}

// home is the empty screen of a session with no turns yet, h rows, centred both ways: the
// wordmark rising in on the spring, the repo's pulse as tone chips, the recent sessions as rich
// rows (title, what was said last, agent and age) and one tip. From homeSplitMin columns it
// lays out as two panels, narrower as one stacked panel; each frame picks again, so a resize
// switches it.
// homeSplitMin is the narrowest transcript that fits the two-panel home comfortably.
const homeSplitMin = 130

func (m chatView) home(w, h int) []string {
	var block []string
	if w >= homeSplitMin {
		block = m.homeSplit(min(w-2*m.pad(), 104))
	} else {
		block = m.homeStack(min(w-2*m.pad(), 76))
	}
	bw := 0
	for _, l := range block {
		bw = max(bw, ansi.StringWidth(l))
	}
	x := max((w-bw)/2, m.pad())
	top := max((h-len(block))/2, 1)
	lift := int((1-m.homeIn.pos)*3 + 0.5)
	if m.homeIn.key == "" || !m.homeIn.moving() {
		lift = 0
	}
	out := make([]string, 0, h)
	for range top + lift {
		out = append(out, "")
	}
	for _, l := range block {
		out = append(out, strings.Repeat(" ", x)+l)
	}
	if len(out) > h {
		out = out[:h]
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out
}

// homePulse is the repo at a glance as tone chips: branch, changes, tend and the session count.
func (m chatView) homePulse() []string {
	t := m.th
	var out []string
	chip := func(label, val string, c color.Color) string {
		return m.chip(" "+label+" ", t.Muted, t.Overlay, false) + m.chip(" "+val+" ", t.Surface, c, true)
	}
	if m.branch != "" {
		out = append(out, chip("branch", m.branch, t.Accent))
	}
	switch {
	case m.dirty == 0:
		out = append(out, chip("tree", "clean", t.Ok))
	case m.dirty > 0:
		out = append(out, chip("tree", fmt.Sprintf("%d changed", m.dirty), t.Warn))
	}
	if m.tendMode != "" {
		out = append(out, chip("tend", fmt.Sprintf("%s %s %d PR%s", m.tendMode, t.G.Dot, m.tendPRs, plural(m.tendPRs)), t.Info))
	}
	return out
}

// homeRow is one recent session in two lines: its title (with its age when titles repeat) and the
// last thing said in it, dim; agent and age right-aligned.
func (m chatView) homeRow(i int, s chatSession, w int, dup bool) []string {
	t := m.th
	title := tabTitle(s.title)
	if title == "" {
		title = kit.Short(s.id, 8)
	}
	age := ""
	if !s.since.IsZero() {
		age = ago(time.Since(s.since))
	}
	last := m.lastLine(s.id)
	if dup && last == "" {
		last = "started " + age
	}
	num := m.chip(fmt.Sprintf(" %d ", i+1), t.Surface, t.Accent, true)
	right := t.Fg(t.Dim).Render(strings.Join(nonEmpty(s.agent, age), " "+t.G.Dot+" "))
	head := spread(num+"  "+t.Fg(t.Strong).Bold(true).Render(kit.Short(title, w-30)), right, w)
	rows := []string{head}
	if last != "" {
		rows = append(rows, "     "+t.Fg(t.Muted).Render(kit.Short(last, w-6)))
	}
	return rows
}

// lastLine is the last thing said in session id that this chat holds, one line.
func (m chatView) lastLine(id string) string {
	msgs := m.convs[id]
	for i := len(msgs) - 1; i >= 0; i-- {
		if c := msgs[i]; (c.who == "agent" || c.who == "you") && strings.TrimSpace(c.text) != "" {
			return strings.Join(strings.Fields(c.text), " ")
		}
	}
	return ""
}

// homeTip is one key or habit worth knowing, a different one per session count.
func (m chatView) homeTip() string {
	t := m.th
	tips := [][2]string{{"ctrl+k", "every action, session and setting in one place"}, {"!cmd", "runs a shell command here; it stays in the session"},
		{"esc", "interrupts a turn; enter while it runs queues the next"}, {"/config", "every setting, grouped, ←→ to change"}}
	tp := tips[len(m.sessions)%len(tips)]
	return t.Fg(t.Accent).Render(tp[0]) + t.Fg(t.Dim).Render("  "+tp[1])
}

// homeStack is the stacked variant: one raised panel, the wordmark and context on its top band,
// the pulse chips, the recent sessions on the focus tone, the tip under it.
func (m chatView) homeStack(w int) []string {
	t := m.th
	panel, deep := on(t.Raised), on(t.Focus)
	row := func(st lipgloss.Style, s string) string { return fill(st, w, "  "+s) }
	word := t.Fg(t.Accent).Bold(true).Render("smithy") + t.Fg(t.Dim).Render("  "+kitVersion())
	ctx := t.Fg(t.Muted).Render(strings.Join(nonEmpty(projectName(m.dir), m.agentModel()), " "+t.G.Dot+" "))
	out := []string{row(panel, ""), row(panel, spread(word, ctx+"  ", w-2)), row(panel, "")}
	if p := m.homePulse(); len(p) > 0 {
		out = append(out, row(panel, strings.Join(p, panel.Render("  "))), row(panel, ""))
	}
	rec := m.recent()
	dup := dupTitles(rec)
	if len(rec) == 0 {
		out = append(out, row(deep, ""), row(deep, t.Fg(t.Text).Render("Your first message starts a session here.")), row(deep, ""))
	} else {
		out = append(out, row(deep, ""))
		for i, s := range rec {
			for _, l := range m.homeRow(i, s, w-4, dup[s.title]) {
				out = append(out, row(deep, l))
			}
			out = append(out, row(deep, ""))
		}
	}
	out = append(out, row(panel, ""), row(panel, m.homeTip()), row(panel, ""))
	return out
}

// homeSplit is the two-panel variant: a narrow overlay-toned column with the wordmark and the
// repo's pulse stacked as tiles, beside a raised column of recent sessions; the tip below both.
func (m chatView) homeSplit(w int) []string {
	t := m.th
	lw := 30
	rw := w - lw - 2
	left, right := on(t.Overlay), on(t.Raised)
	var l []string
	addL := func(s string) { l = append(l, fill(left, lw, "  "+s)) }
	addL("")
	addL(t.Fg(t.Accent).Bold(true).Render("smithy") + t.Fg(t.Dim).Render("  "+kitVersion()))
	addL(t.Fg(t.Muted).Render(kit.Short(projectName(m.dir), lw-4)))
	addL("")
	tile := func(label, val string, c color.Color) {
		addL(t.Fg(t.Dim).Render(label))
		addL(t.Fg(c).Bold(true).Render(kit.Short(val, lw-4)))
		addL("")
	}
	if m.branch != "" {
		tile("branch", m.branch, t.Accent)
	}
	switch {
	case m.dirty == 0:
		tile("working tree", "clean", t.Ok)
	case m.dirty > 0:
		tile("working tree", fmt.Sprintf("%d changed", m.dirty), t.Warn)
	}
	if m.tendMode != "" {
		tile("tend", fmt.Sprintf("%s %s %d PR%s", m.tendMode, t.G.Dot, m.tendPRs, plural(m.tendPRs)), t.Info)
	}
	tile("agent", m.agentModel(), t.Text)
	var r []string
	addR := func(s string) { r = append(r, fill(right, rw, "  "+s)) }
	addR("")
	addR(t.Fg(t.Strong).Bold(true).Render("Pick up where you left off"))
	addR("")
	rec := m.recent()
	dup := dupTitles(rec)
	for i, s := range rec {
		for _, x := range m.homeRow(i, s, rw-4, dup[s.title]) {
			addR(x)
		}
		addR("")
	}
	if len(rec) == 0 {
		addR(t.Fg(t.Text).Render("Nothing here yet: your first message starts a session."))
		addR("")
	}
	for len(l) < len(r) {
		addL("")
	}
	for len(r) < len(l) {
		addR("")
	}
	out := make([]string, len(l))
	for i := range l {
		out[i] = l[i] + "  " + r[i]
	}
	return append(out, "", "  "+m.homeTip())
}

// dupTitles marks the titles more than one recent session shares.
func dupTitles(ss []chatSession) map[string]bool {
	n := map[string]int{}
	for _, s := range ss {
		n[s.title]++
	}
	out := map[string]bool{}
	for k, v := range n {
		out[k] = v > 1
	}
	return out
}

// recent is up to five other sessions of this folder, the newest first.
func (m chatView) recent() []chatSession {
	var out []chatSession
	for _, s := range m.listed() {
		if s.id != m.current {
			out = append(out, s)
		}
	}
	slices.SortStableFunc(out, func(a, b chatSession) int { return b.since.Compare(a.since) })
	return out[:min(len(out), 5)]
}

func projectName(dir string) string {
	if dir == "" {
		return "this folder"
	}
	return filepath.Base(dir)
}

func kitVersion() string {
	if kit.Version == "" {
		return "dev"
	}
	return kit.Version
}

// fresh reports whether the open session has nothing but notes yet, so the empty screen shows.
func (m chatView) fresh() bool {
	for _, c := range m.msgs {
		if c.kind != "system" {
			return false
		}
	}
	return true
}

// intro heads a short session: its title, then the agent and model, the branch and when it
// started, so the transcript can start at the top and grow down.
func (m chatView) intro(w int) []string {
	t := m.th
	s, _ := m.session()
	title := tabTitle(s.title)
	if title == "" {
		title = "new session"
	}
	meta := nonEmpty(strings.Split(m.agentModel(), " "+t.G.Dot+" ")...)
	if m.branch != "" {
		meta = append(meta, "on "+m.branch)
	}
	if !s.since.IsZero() {
		meta = append(meta, "started "+s.since.Local().Format("15:04"))
	}
	return []string{
		m.lane(w, "", "", nil, "", nil),
		m.lane(w, "", "", nil, t.Fg(t.Strong).Bold(true).Render(title), nil),
		m.lane(w, "", "", nil, t.Fg(t.Dim).Render(strings.Join(meta, " "+t.G.Dot+" ")), nil),
		m.lane(w, "", "", nil, "", nil),
	}
}

// lead is how many rows sit above n transcript rows: the intro when a short session starts at the
// top, else the blank rows that keep a long one at the bottom.
func (m chatView) lead(n int) int {
	if m.fresh() && !m.working() && m.offset == 0 {
		return m.rows()
	}
	if k := len(m.intro(m.tw())); m.offset == 0 && k+n < m.rows() {
		return k
	}
	return max(m.rows()-n, 0)
}

// ago is how long ago, in one unit: "just now", "4m ago", "2h ago", "3d ago".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// agentsChip is the header's way into the agents panel when tabs are off: the session count.
func (m chatView) agentsChip() string {
	t := m.th
	n := len(m.listed())
	return m.chip(fmt.Sprintf(" agents %d ", n), t.Text, t.Overlay, false)
}

// agentsChipAt reports whether column x of the header row is on the agents chip.
func (m chatView) agentsChipAt(x int) bool {
	if m.tabsOn {
		return false
	}
	row := ansi.Strip(m.header(max(m.width, 20))[0])
	at := strings.Index(row, " agents ")
	if at < 0 {
		return false
	}
	x0 := ansi.StringWidth(row[:at])
	return x >= x0 && x < x0+ansi.StringWidth(ansi.Strip(m.agentsChip()))
}
