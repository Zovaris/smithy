//go:build harness

package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

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

// askBox is the question card: the question, its options numbered, then a free-text "Other".
func (m chatView) askBox(screen []string, w, h int) []string {
	t := m.th
	if len(m.pending) == 0 {
		return screen
	}
	d := m.pending[0]
	bw, bh := m.boxSize(w, h)
	bh = min(bh, len(d.Options)+12)
	title := "Waiting on you"
	if more := len(m.pending) - 1; more > 0 {
		title += fmt.Sprintf(" · %d more after this", more)
	}
	items := []boxItem{{group: kit.Short(d.Question, bw-6)}}
	if d.Multi {
		items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: "pick any: space or the number picks, enter sends", static: true})
	}
	for i, o := range d.Options {
		g, c := t.G.Dot, t.Muted
		if d.Multi {
			g = "[ ]"
			if m.card.picked[i] {
				g, c = "[x]", t.Accent
			}
		}
		items = append(items, boxItem{glyph: g, color: c, label: fmt.Sprintf("%d  %s", i+1, o)})
	}
	other := m.card.other
	if other == "" {
		other = t.Fg(t.Dim).Render("type your own answer")
	}
	items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: "Other: " + other})
	sel := m.card.sel
	if d.Multi {
		sel++
	}
	foot := "  " + keyHints(t, "↑↓", "move", "1-9", "pick", "enter", "answer", "esc", "later")
	if d.Multi {
		foot = "  " + keyHints(t, "↑↓", "move", "space", "pick", "enter", "send", "esc", "later")
	}
	return m.overBox(screen, m.floatBox(bw, bh, "", title, items, sel, foot), w)
}
