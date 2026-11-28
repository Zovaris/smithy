//go:build harness

package chat

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

// The forge timeline: every transcript row hangs off one vertical rail. Left of the rail sits a
// narrow gutter for marks (the time a prompt was sent, ✓ or ✗ for tools, $ for shell, ? for a
// question); right of it, the text column every block starts on. Bands (shell output, the input)
// run from one cell before the text column to the right edge. Under 80 columns the gutter goes and
// the mark takes the rail's place.

// gutterW is the width of the mark gutter left of the rail.
const gutterW = 1

// grid is this width's columns: the rail, the text column and the right edge (exclusive).
func (m chatView) grid(w int) (rail, text, right int) {
	p := m.pad()
	if w < 80 {
		return p, p + 2, w - p
	}
	return p + gutterW + 1, p + gutterW + 3, w - p
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
	rx, tx, right := m.grid(w)
	p := m.pad()
	g := strings.Repeat(" ", p)
	if rx > p {
		mw := rx - p - 1
		mk := ansi.Truncate(mark, mw, "")
		g += strings.Repeat(" ", mw-ansi.StringWidth(mk)) + mk + " "
	} else if mark != "" {
		rail, railC = "", nil
		g += ansi.Truncate(mark, 1, "")
	}
	if rail != "" {
		g += t.Fg(railC).Render(rail)
	}
	g += strings.Repeat(" ", max(tx-1-ansi.StringWidth(g), 0))
	if bg == nil {
		return g + " " + ansi.Truncate(s, right-tx, "…")
	}
	return g + fill(on(bg), right-tx+1, " "+s)
}

// spreadLane is a lane whose text has left and right parts, the right part ending on the edge.
func (m chatView) spreadLane(w int, mark, rail string, railC color.Color, left, rightS string, bg color.Color) string {
	_, tx, right := m.grid(w)
	room := right - tx
	if bg != nil {
		room--
	}
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

// anvil is the forge mark of the empty screen, drawn in the accent.
var anvil = []string{
	"  ▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄",
	"▀▀██████▄▄██████▀▀",
	"      ▀██████▀",
	"      ▄██████▄",
	"     ▀▀▀▀▀▀▀▀▀",
}

// sparkFrames is how long the sparks fly off the anvil when the empty screen opens.
const sparkFrames = 24

// home is the empty screen of a session with no turns yet, h rows from the top of the transcript:
// the anvil with a few sparks that fly once, what this chat runs with, recent sessions and the
// keys to start. It sits on the timeline's text column, not centered.
func (m chatView) home(w, h int) []string {
	t := m.th
	var out []string
	add := func(mark, s string) { out = append(out, m.lane(w, mark, "", nil, s, nil)) }
	add("", "")
	art := anvil
	if !t.G.Unicode {
		art = []string{"  _______________", " (_______________)", "      |     |", "     _|_____|_", ""}
	}
	sparks := []string{"", "", "", "", ""}
	if t.G.Unicode {
		ph := min(m.frame, sparkFrames)
		c := t.Warn
		if ph >= sparkFrames || m.still {
			c, ph = t.Dim, sparkFrames
		}
		pos := []int{2, 6, 11, 4, 9}
		for i := range 3 {
			x := pos[i] + ph/4
			sparks[i] = strings.Repeat(" ", x) + t.Fg(c).Render([]string{"·", "*", "·"}[i])
		}
	}
	name := t.Fg(t.Strong).Bold(true).Render("smithy") + t.Fg(t.Dim).Render(" "+kitVersion())
	where := t.Fg(t.Muted).Render("a new session in ") + t.Fg(t.Text).Render(projectName(m.dir))
	runs := t.Fg(t.Muted).Render("runs with ") + t.Fg(t.Info).Render(m.metaText())
	side := []string{"", name, where, runs, ""}
	for i, a := range art {
		add("", padTo(t.Fg(t.Accent).Render(a), 20)+padTo(sparks[i], 10)+side[i])
	}
	add("", "")
	head := func(s string) string { return t.Fg(t.Accent).Bold(true).Render(s) }
	var recent []string
	for _, s := range m.listed() {
		if s.id == m.current || len(recent) == 5 {
			continue
		}
		g, c := m.sessionGlyph(s.state)
		recent = append(recent, t.Fg(c).Render(g)+" "+t.Fg(t.Text).Render(kit.Short(tabTitle(s.title), 24)))
	}
	if len(recent) == 0 {
		recent = []string{t.Fg(t.Dim).Render("nothing yet in this folder")}
	}
	keys := [][2]string{{"enter", "start with your first message"}, {"tab", "sessions and agents"}, {"/agent", "pick the agent"}, {"/config", "every setting"}, {"/usage", "tokens and spend"}, {"?", "all keys"}}
	left := append([]string{head("recent")}, recent...)
	right := []string{head("start")}
	for _, k := range keys {
		right = append(right, t.Fg(t.Accent).Render(fmt.Sprintf("%-8s", k[0]))+t.Fg(t.Muted).Render(k[1]))
	}
	for i := range max(len(left), len(right)) {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		add("", padTo(l, 30)+r)
	}
	add("", "")
	add("", t.Fg(t.Dim).Render("tip  ")+t.Fg(t.Muted).Render(homeTips[(len(m.sessions))%len(homeTips)]))
	if len(out) > h {
		out = out[:h]
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out
}

// homeTips are the one-line tips the empty screen rotates through.
var homeTips = []string{
	"!cmd runs a shell command in this folder; its output stays in the session",
	"@ adds a file from this folder to your message",
	"esc interrupts a turn; enter while it runs queues the next message",
	"ctrl+o folds or opens tool calls: minimal, expandable, verbose",
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

// padTo pads s with spaces to n cells.
func padTo(s string, n int) string { return s + strings.Repeat(" ", max(n-ansi.StringWidth(s), 0)) }

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
		return m.rows() - n
	}
	if k := len(m.intro(m.tw())); m.offset == 0 && k+n < m.rows() {
		return k
	}
	return max(m.rows()-n, 0)
}
