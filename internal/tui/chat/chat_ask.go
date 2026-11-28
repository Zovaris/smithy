//go:build harness

package chat

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

// askCard is the state of the question card: which decision it shows, the selected row (the
// options, then "Other"), the options picked on a multi-select question and the free text.
type askCard struct {
	id     string
	sel    int
	picked map[int]bool
	other  string
}

// offerAsk opens the card for the first waiting decision when nothing else is open and the
// composer is empty, so a question never takes the keys of a message being typed. A question
// the user closed with esc stays closed until they open it again.
func (m *chatView) offerAsk() {
	if len(m.pending) == 0 || m.overlay != overlayNone || m.input.Value() != "" || len(m.images) > 0 || m.welcome != welcomeOff {
		return
	}
	if m.card.id == m.pending[0].ID {
		return
	}
	m.openAsk()
}

// openAsk shows the card for the first waiting decision.
func (m *chatView) openAsk() {
	if len(m.pending) == 0 {
		m.system("nothing waits on you")
		return
	}
	m.card = askCard{id: m.pending[0].ID, picked: map[int]bool{}}
	m.overlay = overlayAsk
}

func (m chatView) askKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if len(m.pending) == 0 || m.pending[0].ID != m.card.id {
		m.overlay = overlayNone
		return m, nil
	}
	d := m.pending[0]
	n := len(d.Options)
	onOther := m.card.sel == n
	k := msg.String()
	switch k {
	case "esc":
		m.overlay = overlayNone
		return m, nil
	case "up":
		m.card.sel = max(m.card.sel-1, 0)
		return m, nil
	case "down", "tab":
		m.card.sel = min(m.card.sel+1, n)
		return m, nil
	case "backspace":
		if r := []rune(m.card.other); len(r) > 0 {
			m.card.other = string(r[:len(r)-1])
		}
		return m, nil
	case "enter":
		if onOther || m.card.other != "" && !d.Multi {
			if a := strings.TrimSpace(m.card.other); a != "" {
				return m, m.answer(a)
			}
			return m, nil
		}
		if !d.Multi {
			return m, m.answer(d.Options[m.card.sel])
		}
		var as []string
		for i, o := range d.Options {
			if m.card.picked[i] {
				as = append(as, o)
			}
		}
		if len(as) == 0 {
			as = []string{d.Options[m.card.sel]}
		}
		return m, m.answer(strings.Join(as, ", "))
	case "space":
		if d.Multi && !onOther {
			m.card.picked[m.card.sel] = !m.card.picked[m.card.sel]
			return m, nil
		}
	}
	t := msg.Text
	if k == "space" {
		t = " "
	}
	if t == "" {
		return m, nil
	}
	if len(t) == 1 && t[0] >= '1' && t[0] <= '9' && m.card.other == "" && int(t[0]-'1') < n {
		i := int(t[0] - '1')
		if !d.Multi {
			return m, m.answer(d.Options[i])
		}
		m.card.sel, m.card.picked[i] = i, !m.card.picked[i]
		return m, nil
	}
	m.card.sel, m.card.other = n, m.card.other+t
	return m, nil
}

// askBox is the Questions panel docked above the input: the question, its options numbered with
// a muted description line under each (text after " — "), then "Type your own answer". The
// transcript above stays readable; only the rows the panel needs are covered.
func (m chatView) askBox(screen []string, w, h int) []string {
	t := m.th
	if len(m.pending) == 0 {
		return screen
	}
	d := m.pending[0]
	p := m.pad()
	gut := strings.Repeat(" ", p)
	tw := m.tw()
	bw := tw - 2*p
	bg, sel := on(t.Raised), on(t.Focus)
	rail := t.Fg(t.Warn).Render(m.railMark(true))
	row := func(st lipgloss.Style, s string) string { return gut + fill(st, bw, rail+s) + gut }
	title := t.Fg(t.Warn).Bold(true).Render("Questions")
	count := t.Fg(t.Dim).Render(fmt.Sprintf("1 of %d  ", len(m.pending)))
	rows := []string{row(bg, ""), row(bg, spread(" "+title, count, bw-1)), row(bg, "")}
	for _, l := range strings.Split(ansi.Wrap(d.Question, max(bw-8, 10), ""), "\n") {
		rows = append(rows, row(bg, " "+t.Fg(t.Strong).Bold(true).Render(l)))
	}
	rows = append(rows, row(bg, ""))
	opt := func(i int, label, desc string) {
		st, num, lab := bg, t.Fg(t.Accent).Render(fmt.Sprintf(" %d ", i+1)), t.Fg(t.Text).Render(label)
		if i == m.card.sel {
			st, num, lab = sel, m.chip(fmt.Sprintf(" %d ", i+1), t.Surface, t.Accent, true), t.Fg(t.Strong).Bold(true).Render(label)
		}
		mark := ""
		if d.Multi && i < len(d.Options) {
			mark = "[ ] "
			if m.card.picked[i] {
				mark = t.Fg(t.Accent).Render("[x] ")
			}
		}
		rows = append(rows, row(st, " "+num+"  "+mark+lab))
		if desc != "" {
			rows = append(rows, row(st, "      "+t.Fg(t.Muted).Render(kit.Short(desc, bw-10))))
		}
	}
	for i, o := range d.Options {
		label, desc, _ := strings.Cut(o, " — ")
		opt(i, label, desc)
	}
	other := "Type your own answer"
	if m.card.other != "" {
		other = m.card.other + "▏"
	}
	opt(len(d.Options), other, "")
	hint := t.Fg(t.Dim).Render("↑↓ select " + t.G.Dot + " enter submit " + t.G.Dot + " esc dismiss")
	if d.Multi {
		hint = t.Fg(t.Dim).Render("↑↓ select " + t.G.Dot + " space pick " + t.G.Dot + " enter submit " + t.G.Dot + " esc dismiss")
	}
	rows = append(rows, row(bg, ""), row(bg, " "+hint), row(bg, ""))
	bottom := len(screen) - 1 - len(m.composer(tw))
	top := max(bottom-len(rows), headRows)
	rows = rows[len(rows)-(bottom-top):]
	out := slices.Clone(screen)
	cut := tw
	if m.sideOn() {
		cut = m.sideX()
	}
	for i, r := range rows {
		out[top+i] = ansi.Truncate(r, cut, "") + ansi.TruncateLeft(out[top+i], cut, "")
	}
	return out
}
