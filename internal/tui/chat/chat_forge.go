//go:build harness

package chat

import (
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

// home is the empty screen of a session with no turns yet, h rows: a third of the way down, the
// wordmark and version, one line of context (folder, branch, agent, model), then up to five recent
// sessions numbered so 1-5 opens one. Nothing else; it sits on the transcript's text column.
func (m chatView) home(w, h int) []string {
	t := m.th
	var rows []string
	add := func(s string) { rows = append(rows, m.lane(w, "", "", nil, s, nil)) }
	add(t.Fg(t.Accent).Bold(true).Render("smithy") + t.Fg(t.Dim).Render("  "+kitVersion()))
	ctx := []string{projectName(m.dir)}
	if m.branch != "" {
		ctx = append(ctx, m.branch)
	}
	ctx = append(ctx, nonEmpty(strings.Split(m.agentModel(), " "+t.G.Dot+" ")...)...)
	add(t.Fg(t.Muted).Render(strings.Join(ctx, " "+t.G.Dot+" ")))
	add("")
	rec := m.recent()
	for i, s := range rec {
		age := ""
		if !s.since.IsZero() {
			age = ago(time.Since(s.since))
		}
		add(t.Fg(t.Accent).Render(fmt.Sprint(i+1)) + "  " + padTo(t.Fg(t.Text).Render(kit.Short(tabTitle(s.title), 32)), 34) + t.Fg(t.Dim).Render(strings.Join(nonEmpty(s.agent, age), " "+t.G.Dot+" ")))
	}
	if len(rec) == 0 {
		add(t.Fg(t.Dim).Render("your first message starts a session"))
	} else {
		add("")
		keys := fmt.Sprintf("1-%d opens one", len(rec))
		if len(rec) == 1 {
			keys = "1 opens it"
		}
		add(t.Fg(t.Dim).Render(keys + " " + t.G.Dot + " or type to start a new session"))
	}
	out := make([]string, 0, h)
	for range max(h/3-2, 1) {
		out = append(out, "")
	}
	out = append(out, rows...)
	if len(out) > h {
		out = out[:h]
	}
	for len(out) < h {
		out = append(out, "")
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
