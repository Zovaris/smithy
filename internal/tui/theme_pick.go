//go:build harness

package tui

import (
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"
)

// themeMsg is ui.theme read from or saved through the daemon; err leaves the theme as it was.
type themeMsg struct {
	name, note string
	err        error
}

// loadTheme reads ui.theme from the daemon so the chat starts in the theme the web shows too.
func (m chatView) loadTheme() tea.Cmd {
	api := m.cfg
	if api == nil {
		return nil
	}
	return func() tea.Msg {
		s, err := api.Settings(context.Background())
		if err != nil {
			return nil
		}
		for _, r := range s.Settings {
			if r.Key == "ui.theme" {
				return themeMsg{name: r.Value}
			}
		}
		return nil
	}
}

func (m chatView) saveTheme(n string) tea.Cmd {
	api := m.cfg
	if api == nil {
		return func() tea.Msg { return themeMsg{name: n, note: "theme " + n + " for this chat; no daemon to save it"} }
	}
	return func() tea.Msg {
		err := api.SetSetting(context.Background(), "ui.theme", n)
		return themeMsg{name: n, note: "theme " + n + ": saved as ui.theme, the web uses it too", err: err}
	}
}

func (m chatView) themeLoaded(msg themeMsg) chatView {
	if msg.err != nil {
		m.system("error: " + msg.err.Error())
		return m
	}
	if !slices.Contains(Themes, msg.name) {
		return m
	}
	m.th = m.th.renamed(msg.name)
	m.input.SetPromptFunc(2, composerPrompt(m.th))
	if msg.note != "" {
		m.status(msg.note)
	}
	return m
}

func (m chatView) themeCommand(arg string) (tea.Model, tea.Cmd) {
	if arg != "" {
		if !slices.Contains(Themes, arg) {
			m.system("unknown theme " + arg + "; pick one of steel, frost, plum")
			return m, nil
		}
		return m, m.saveTheme(arg)
	}
	m.overlay, m.setupStep, m.setupNote = overlayPicker, stepTheme, ""
	m.themeSel = max(slices.Index(Themes, m.th.name), 0)
	return m, nil
}

func (m chatView) themeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay, m.setupStep = overlayNone, stepAgent
	case "up", "k":
		m.themeSel = max(m.themeSel-1, 0)
	case "down", "j":
		m.themeSel = min(m.themeSel+1, len(Themes)-1)
	case "enter":
		m.overlay, m.setupStep = overlayNone, stepAgent
		return m, m.saveTheme(Themes[m.themeSel])
	}
	return m, nil
}

var themeHints = map[string]string{"steel": "graphite with the ember accent (default)", "frost": "graphite with a cool teal accent", "plum": "graphite with a violet accent"}

func (m chatView) themeBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	items := []boxItem{{group: "Theme for this TUI and the web · ui.theme"}}
	for _, n := range Themes {
		it := boxItem{glyph: t.g.on, color: t.renamed(n).accent, label: n, hint: themeHints[n]}
		if t.plain {
			it.glyph, it.color = t.g.dot, t.muted
		}
		if n == t.name {
			it.key = t.g.on + " now"
		}
		items = append(items, it)
	}
	foot := "  " + keyHints(t, "↑↓", "move", "enter", "save", "esc", "cancel")
	return m.overBox(screen, m.floatBox(bw, bh, "", "Theme", items, m.themeSel, foot), w)
}
