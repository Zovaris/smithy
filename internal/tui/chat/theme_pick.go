//go:build harness

package chat

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/Zovaris/smithy/internal/tui/kit"
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
		ctx, cancel := daemonCtx()
		defer cancel()
		s, err := api.Settings(ctx)
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
		ctx, cancel := daemonCtx()
		defer cancel()
		err := api.SetSetting(ctx, "ui.theme", n)
		return themeMsg{name: n, note: "theme " + n + ": saved as ui.theme, the web uses it too", err: err}
	}
}

func (m chatView) themeLoaded(msg themeMsg) chatView {
	if msg.err != nil {
		m.system("error: " + msg.err.Error())
		return m
	}
	if !slices.Contains(kit.Themes, msg.name) {
		return m
	}
	m.th = m.th.Renamed(msg.name)
	m.input.SetPromptFunc(0, composerPrompt(m.th))
	m.input.SetStyles(composerStyles(m.th))
	if msg.note != "" {
		m.status(msg.note)
	}
	return m
}

func (m chatView) themeCommand(arg string) (tea.Model, tea.Cmd) {
	if arg != "" {
		if !slices.Contains(kit.Themes, arg) {
			m.system("unknown theme " + arg + "; pick one of steel, frost, plum")
			return m, nil
		}
		return m, m.saveTheme(arg)
	}
	m.overlay, m.setupStep, m.setupNote = overlayPicker, stepTheme, ""
	m.themeSel = max(slices.Index(kit.Themes, m.th.Name), 0)
	return m, nil
}

func (m chatView) themeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay, m.setupStep = overlayNone, stepAgent
	case "up", "k":
		m.themeSel = wrapStep(m.themeSel, -1, len(kit.Themes))
	case "down", "j":
		m.themeSel = wrapStep(m.themeSel, 1, len(kit.Themes))
	case "left", "h":
		m.themeSel = (m.themeSel + len(kit.Themes) - 1) % len(kit.Themes)
	case "right", "l":
		m.themeSel = (m.themeSel + 1) % len(kit.Themes)
	case "enter":
		m.overlay, m.setupStep = overlayNone, stepAgent
		return m, m.saveTheme(kit.Themes[m.themeSel])
	}
	return m, nil
}

var themeHints = map[string]string{"steel": "graphite with the ember accent (default)", "frost": "blue slate with a teal accent",
	"plum": "aubergine with a violet accent", "moss": "forest green with a lime accent", "dune": "warm sand with a saffron accent",
	"dusk": "indigo night with a periwinkle accent", "rose": "mauve with a pink accent"}

func (m chatView) themeBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	items := []boxItem{{group: "Theme for this TUI and the web · ui.theme"}}
	for _, n := range kit.Themes {
		it := boxItem{glyph: t.G.On, color: t.Renamed(n).Accent, label: n, hint: themeHints[n]}
		if t.Plain {
			it.glyph, it.color = t.G.Dot, t.Muted
		}
		if n == t.Name {
			it.key = t.G.On + " now"
		}
		items = append(items, it)
	}
	foot := "  " + keyHints(t, "↑↓←→", "move", "enter", "save", "esc", "cancel")
	return m.overBox(screen, m.floatBox(bw, bh, "", "Theme", items, m.themeSel, foot), w)
}
