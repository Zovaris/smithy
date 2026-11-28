//go:build harness

package chat

import (
	"image/color"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/tui/kit"
)

// welcomeStep is where the first-run walk is: a dithered splash, then agents, model, API keys
// and notifications, each one the picker or setting the chat already has.
type welcomeStep int

const (
	welcomeOff welcomeStep = iota
	welcomeSplash
	welcomeAgents
	welcomeModel
	welcomeKeys
	welcomeNotify
)

type welcomeSettingsMsg struct {
	s   api.Settings
	err error
}

// openWelcome starts the walk at the splash; the steps reuse the agent and model pickers.
func (m *chatView) openWelcome() {
	m.welcome, m.overlay, m.setupNote, m.setupSel = welcomeSplash, overlayWelcome, "", 0
	m.splash, m.splashOn = nil, false
}

func (m *chatView) closeWelcome() {
	m.welcome, m.overlay, m.setupNote = welcomeOff, overlayNone, ""
	if m.welcomeNote != "" {
		m.system(m.welcomeNote)
		m.welcomeNote = ""
	}
}

func (m chatView) loadWelcomeSettings() tea.Cmd {
	api := m.cfg
	if api == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := daemonCtx()
		defer cancel()
		s, err := api.Settings(ctx)
		return welcomeSettingsMsg{s, err}
	}
}

// welcomeTo moves the walk to step s and opens what that step shows.
func (m chatView) welcomeTo(s welcomeStep, agent string) (chatView, tea.Cmd) {
	m.welcome, m.setupNote, m.setupSel = s, "", 0
	switch s {
	case welcomeAgents:
		m.openPicker(overlaySetup)
		return m, m.loadSetup(false)
	case welcomeModel:
		return m, m.askModel(agent)
	case welcomeKeys:
		m.overlay, m.setupStep = overlayWelcome, stepAgent
		if m.welcomeSet != nil {
			return m, nil
		}
		return m, m.loadWelcomeSettings()
	case welcomeNotify:
		m.overlay = overlayWelcome
		return m, nil
	}
	m.closeWelcome()
	m.system("smithy is set up; /welcome walks these steps again, /set and /keys change them one by one")
	return m, nil
}

// welcomeSaved moves on once the agents or model step saved its pick.
func (m chatView) welcomeSaved() (chatView, tea.Cmd) {
	if m.welcome == welcomeAgents && m.setupStep == stepAgent && !strings.HasPrefix(m.welcomeAgent, "api:") {
		return m.welcomeTo(welcomeModel, m.welcomeAgent)
	}
	return m.welcomeTo(welcomeKeys, "")
}

// toChat hands a typed / to the composer: commands are never picker input.
func (m chatView) toChat(text string) (tea.Model, tea.Cmd) {
	m.closeWelcome()
	m.input.InsertString(text)
	return m, nil
}

// welcomeKey handles the splash, keys and notifications steps, and the walk's esc and / in
// the agent and model steps it borrows.
func (m chatView) welcomeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	k := msg.String()
	if msg.Text == "/" && (m.overlay == overlayWelcome || m.setupStep == stepAgent || m.modelIn == "") {
		n, cmd := m.toChat("/")
		return n, cmd, true
	}
	if m.overlay != overlayWelcome {
		if k == "esc" && m.welcome == welcomeModel {
			n, cmd := m.welcomeTo(welcomeKeys, "")
			return n, cmd, true
		}
		if k == "esc" || k == "ctrl+l" {
			m.closeWelcome()
			return m, nil, true
		}
		return m, nil, false
	}
	switch k {
	case "esc":
		m.closeWelcome()
	case "enter", "space", " ", "right", "tab":
		n, cmd := m.welcomeTo(m.welcome+1, "")
		if m.welcome == welcomeSplash && m.setup == nil {
			n, cmd = m.welcomeTo(welcomeKeys, "")
		}
		return n, cmd, true
	case "left", "shift+tab":
		if m.welcome == welcomeNotify {
			n, cmd := m.welcomeTo(welcomeKeys, "")
			return n, cmd, true
		}
	}
	return m, nil, true
}

// welcomeView draws the walk full screen: the dither field, and the step's box over it.
func (m chatView) welcomeView(w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	var items []boxItem
	title, foot := "", ""
	switch m.welcome {
	case welcomeSplash:
		title = "Welcome to smithy " + kit.Version
		items = []boxItem{
			{group: "Four short steps; each one changes later from the chat."},
			{glyph: "1", color: t.Accent, label: "agents", hint: "the agent CLIs and API keys found on this machine", static: true},
			{glyph: "2", color: t.Accent, label: "model", hint: "the model new sessions start with", static: true},
			{glyph: "3", color: t.Accent, label: "API keys", hint: "which are stored, and how to add one", static: true},
			{glyph: "4", color: t.Accent, label: "notifications", hint: "Telegram when work needs you", static: true},
		}
		foot = "  " + keyHints(t, "enter", "begin", "/", "type a command", "esc", "later")
	case welcomeKeys:
		title = "3/4  API keys"
		items = m.welcomeKeyItems()
		foot = "  " + keyHints(t, "enter", "next", "/", "type a command", "esc", "done")
	case welcomeNotify:
		title = "4/4  notifications"
		items = m.welcomeNotifyItems()
		foot = "  " + keyHints(t, "enter", "finish", "←", "back", "/", "type a command", "esc", "done")
	default:
		return m.welcomeField(w, h, 0)
	}
	items = append(items, m.noteItems()...)
	box := m.floatBox(bw, min(bh, len(items)+12), "", title, items, -1, foot)
	return m.overBox(m.welcomeField(w, h, len(box)), box, w)
}

func (m chatView) welcomeKeyItems() []boxItem {
	t := m.th
	var items []boxItem
	if m.setupSt != nil {
		items = append(items, boxItem{group: "API AGENTS"})
		for _, a := range m.setupSt.Agents {
			if a.Kind != "api" {
				continue
			}
			it := boxItem{glyph: t.G.Pass, color: t.Ok, label: a.Name, hint: kit.Short(a.Detail, 40), key: "ready", static: true}
			if !a.Ready {
				it.glyph, it.color, it.key, it.hint = t.G.Fail, t.Err, "missing", a.Fix
			}
			items = append(items, it)
		}
	}
	if s := m.welcomeSet; s != nil {
		items = append(items, boxItem{group: "STORED KEYS"})
		if len(s.Keys) == 0 {
			items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: "none yet", hint: "smithy auth set <service>", static: true})
		}
		for _, k := range s.Keys {
			items = append(items, boxItem{glyph: t.G.Pass, color: t.Ok, label: k.Service, hint: k.Masked, key: "/key rm " + k.Service, static: true})
		}
	}
	return items
}

func (m chatView) welcomeNotifyItems() []boxItem {
	t := m.th
	items := []boxItem{{group: "Telegram tells you when a PR is ready or work is blocked; /set changes a row."}}
	if m.welcomeSet == nil {
		return append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: "reading the settings…", static: true})
	}
	for _, r := range m.welcomeSet.Settings {
		if !strings.HasPrefix(r.Key, "notify.") {
			continue
		}
		v := r.Value
		if v == "" {
			v = "unset"
		}
		items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: r.Key, hint: kit.Short(v, 40), key: "/set " + r.Key, static: true})
	}
	return append(items, boxItem{glyph: t.G.Dot, color: t.Info, label: "bot token", hint: "smithy auth set telegram, then smithy notify test", static: true})
}

// fieldAt is the drifting steel field's lightness at cell x, y and phase ph; the welcome and
// the entry splash both draw it.
func fieldAt(x, y int, ph float64) float64 {
	return 0.5 + 0.3*math.Sin(float64(x)/11+ph) + 0.2*math.Cos(float64(y)/4-ph*0.7)
}

// welcomeStrike is how often, in frames, the welcome's anvil throws sparks while motion is on.
const welcomeStrike = 45

// welcomeField is the dither field with the lit anvil centered in the room below a box of height
// bh drawn by overBox; too short a screen gets the field alone. Reduced motion holds it still.
func (m chatView) welcomeField(w, h, bh int) []string {
	t := m.th
	s := kit.BigPlate
	if t.Plain || len(t.Ramp) == 0 || !t.G.Unicode {
		return m.ditherField(w, h)
	}
	top := max(h*120/800, 1) + bh
	room := h - top
	sc := anvilScene{s: s, ax: (w - s.W()) / 2, fade: 1, rise: 1}
	if room >= s.H()/2+1 && w >= s.W() {
		sc.ay = (top + (room-s.H()/2)/2) * 2
	} else {
		sc.rise, sc.ay = 0, -s.H()
	}
	if !m.still {
		sc.ph = float64(m.frame) / 12
		f := kit.NewFX(7)
		f.Strike(s, 14, 0)
		for range m.frame % welcomeStrike {
			f.Step()
		}
		sc.sparks = f.Sparks
	}
	return anvilField(t, sc, w, h)
}

// ditherField fills w×h with the theme ramp drifting by frame, Bayer-dithered so the bands move
// without jumping; a plain theme gets blank rows.
func (m chatView) ditherField(w, h int) []string {
	t := m.th
	out := make([]string, h)
	if t.Plain || len(t.Ramp) == 0 {
		return out
	}
	ph := float64(m.frame) / 12
	for y := range h {
		var b strings.Builder
		var run color.Color
		n := 0
		flush := func() {
			if n > 0 {
				b.WriteString(lipgloss.NewStyle().Background(run).Render(strings.Repeat(" ", n)))
			}
		}
		for x := range w {
			c := kit.Dpick(t.Ramp, fieldAt(x, y, ph), x, y)
			if c != run {
				flush()
				run, n = c, 0
			}
			n++
		}
		flush()
		out[y] = b.String()
	}
	return out
}
