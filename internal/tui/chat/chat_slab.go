//go:build harness

package chat

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

// chip is s on bg in fg: the flat block tabs, keys, options and badges are drawn as. Without
// colour it is s in reverse for bold ones, else plain.
func (m chatView) chip(s string, fg, bg color.Color, bold bool) string {
	if m.th.Plain || bg == nil {
		st := lipgloss.NewStyle().Bold(bold)
		if bold {
			st = st.Reverse(true)
		}
		return st.Render(s)
	}
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Bold(bold).Render(s)
}

// on is a style on background bg, none without colour.
func on(bg color.Color) lipgloss.Style {
	if bg == nil {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Background(bg)
}

// sessionRunning reports whether session id runs a turn.
func (m chatView) sessionRunning(id string) bool {
	for _, s := range m.sessions {
		if s.id == id {
			return s.state == "running"
		}
	}
	return false
}

// railGlyph is the one-cell accent rail on the left of your messages, the input and asks.
func railGlyph(uni bool) string {
	if uni {
		return "▌"
	}
	return "|"
}

// spring is the one motion curve: quick, a touch under critical damping, so a panel settles
// without a visible bounce.
var spring = harmonica.NewSpring(harmonica.FPS(kit.Fps), 9, 0.9)

// motion is an overlay's entrance: pos runs from 0 (just opened) to 1 (settled).
type motion struct {
	shown    chatOverlay
	pos, vel float64
}

// follow restarts the entrance when a different overlay opens; reduced motion settles at once.
func (mo motion) follow(o chatOverlay, still bool) motion {
	if o == mo.shown {
		return mo
	}
	mo.shown, mo.pos, mo.vel = o, 0, 0
	if still || o == overlayNone {
		mo.pos = 1
	}
	return mo
}

func (mo motion) step() motion {
	if mo.moving() {
		mo.pos, mo.vel = spring.Update(mo.pos, mo.vel, 1)
		if mo.pos > 0.995 && mo.vel < 0.01 {
			mo.pos, mo.vel = 1, 0
		}
	}
	return mo
}

func (mo motion) moving() bool { return mo.shown != overlayNone && mo.pos < 1 }

// lift is how many rows below its resting place an opening panel draws.
func (mo motion) lift() int { return int((1-mo.pos)*3 + 0.5) }

// valueBump is a value that just changed: it slides two cells into place on the spring, so the
// eye catches which row moved.
type valueBump struct {
	key      string
	pos, vel float64
	still    bool
}

func (v valueBump) start() valueBump {
	v.pos, v.vel = 0, 0
	if v.still {
		v.pos = 1
	}
	return v
}

func (v valueBump) step() valueBump {
	if v.moving() {
		v.pos, v.vel = spring.Update(v.pos, v.vel, 1)
		if v.pos > 0.995 && v.vel < 0.01 {
			v.pos, v.vel = 1, 0
		}
	}
	return v
}

func (v valueBump) moving() bool { return v.key != "" && v.pos < 1 }

// shift is the extra right padding of key's value while it settles.
func (v valueBump) shift(key string) int {
	if key != v.key || !v.moving() {
		return 0
	}
	return int((1-v.pos)*2 + 0.5)
}

// agentGutter is the thin line left of an agent's answer, in the border colour.
func agentGutter(uni bool) string {
	if uni {
		return "▏"
	}
	return "|"
}
