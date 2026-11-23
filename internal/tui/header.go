package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// fps is the frame rate while something moves (the shimmer or the cap meter); at rest no frames run.
const fps = 14

// wordA is the ANSI Shadow wordmark (6 rows): █ are faces, the box glyphs its shadow.
var wordA = []string{
	"███████╗███╗   ███╗██╗████████╗██╗  ██╗██╗   ██╗",
	"██╔════╝████╗ ████║██║╚══██╔══╝██║  ██║╚██╗ ██╔╝",
	"███████╗██╔████╔██║██║   ██║   ███████║ ╚████╔╝ ",
	"╚════██║██║╚██╔╝██║██║   ██║   ██╔══██║  ╚██╔╝  ",
	"███████║██║ ╚═╝ ██║██║   ██║   ██║  ██║   ██║   ",
	"╚══════╝╚═╝     ╚═╝╚═╝   ╚═╝   ╚═╝  ╚═╝   ╚═╝   ",
}

// wordB is the Calvin S wordmark (3 rows) for terminals under 100 columns.
var wordB = []string{
	"╔═╗╔╦╗╦╔╦╗╦ ╦╦ ╦",
	"╚═╗║║║║ ║ ╠═╣╚╦╝",
	"╚═╝╩ ╩╩ ╩ ╩ ╩ ╩ ",
}

const (
	bandHalf   = 3    // shimmer band half-width in columns
	bandMix    = 0.25 // strongest pull toward warm white
	sweepSecs  = 3.0
	restTicks  = 4 // refresh ticks (2 s each) between idle sweeps
	fieldWidth = 14
)

// fx is the header's motion: one shimmer band sweeping across the wordmark, then rest.
type fx struct {
	active bool
	pos    float64 // band center column
	width  int     // columns the band crosses
}

func newFX() *fx { return &fx{} }

// sweep starts a shimmer pass unless one is running.
func (f *fx) sweep(width int) {
	if !f.active {
		f.active, f.width, f.pos = true, width, -bandHalf
	}
}

func (f *fx) step() {
	if !f.active {
		return
	}
	f.pos += float64(f.width+2*bandHalf) / (sweepSecs * fps)
	if f.pos > float64(f.width+bandHalf) {
		f.active = false
	}
}

func (f *fx) busy() bool { return f.active }

func isFace(r rune) bool { return r == '█' }

// wordmark colors a wordmark: faces on a 2D ember→orange→gold blend, shadows in dim ember-brown,
// plus the shimmer band when one is sweeping.
func (m Model) wordmark(rows []string) []string {
	t := m.th
	w := 0
	for _, r := range rows {
		w = max(w, len([]rune(r)))
	}
	rich := !t.plain && m.profile >= colorprofile.ANSI256
	var grad []color.Color
	if rich {
		grad = lipgloss.Blend2D(w, len(rows), 100, t.forge...)
	}
	band, shimmer := 0.0, m.fx != nil && m.motion && m.fx.active
	if shimmer {
		band = m.fx.pos
	}
	out := make([]string, len(rows))
	for y, row := range rows {
		var b strings.Builder
		for x, r := range []rune(row) {
			if r == ' ' {
				b.WriteRune(' ')
				continue
			}
			face := isFace(r) || len(rows) == 3
			switch {
			case t.plain:
				b.WriteRune(r)
			case !rich:
				c := t.accent
				if !face {
					c = t.muted
				}
				b.WriteString(t.fg(c).Render(string(r)))
			default:
				c := grad[y*w+x]
				if !face {
					c = t.shadow
				}
				if shimmer {
					if d := abs(float64(x) - band); d < bandHalf {
						mix := bandMix * (1 - d/bandHalf)
						steps := lipgloss.Blend1D(11, c, t.warmWhite)
						c = steps[int(mix*10+0.5)]
					}
				}
				b.WriteString(lipgloss.NewStyle().Foreground(c).Render(string(r)))
			}
		}
		out[y] = b.String()
	}
	return out
}

// field is the fading ╱ hatch beside the wordmark, one string per row (n wide, narrowing).
func (m Model) field(rows int) []string {
	t := m.th
	g := "╱"
	if !t.g.unicode {
		g = "/"
	}
	fade := []color.Color{t.accent, t.accent, t.muted}
	if !t.plain && m.profile >= colorprofile.ANSI256 {
		fade = lipgloss.Blend1D(fieldWidth, t.forge[1], t.shadow, t.border)
	}
	out := make([]string, rows)
	for y := 0; y < rows; y++ {
		n := fieldWidth * (rows + 1 - y) / (rows + 1)
		var b strings.Builder
		for x := 0; x < max(n, 0); x++ {
			b.WriteString(t.fg(fade[min(x*len(fade)/fieldWidth, len(fade)-1)]).Render(g))
		}
		out[y] = b.String()
	}
	return out
}
