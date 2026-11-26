package tui

import (
	"math/rand/v2"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// fps is the frame rate while sparks, the shimmer, the cap meter or a running item move; at rest no frames run.
const fps = 15

// breathSecs is one slow in-and-out of a running pill's dot.
const breathSecs = 2.0

// wordB is the Calvin S name.
var wordB = []string{
	"╔═╗╔╦╗╦╔╦╗╦ ╦╦ ╦",
	"╚═╗║║║║ ║ ╠═╣╚╦╝",
	"╚═╝╩ ╩╩ ╩ ╩ ╩ ╩ ",
}

const (
	bandHalf  = 3
	sweepSecs = 2.5
	gravity   = 55.0
)

// anvilBig is the 30×14-pixel anvil (7 text rows of half blocks): three rows of headroom for
// sparks, a flat face that ends in a pointed horn on the left and a square heel on the right, a
// narrow waist and a wide base on two feet.
var anvilBig = []string{
	"                              ",
	"                              ",
	"                              ",
	"    ######################    ",
	"##########################    ",
	"    ######################    ",
	"         ###############      ",
	"            #########         ",
	"             #######          ",
	"             #######          ",
	"           ###########        ",
	"        #################     ",
	"       ###################    ",
	"       #######     #######    ",
}

// sprite is the header anvil in pixels (two per text row) and where sparks fly from.
type sprite struct {
	mask  []string
	faceY int // first face row
	hitX  float64
}

var bigPlate = sprite{anvilBig, 3, 16}

func (s sprite) w() int { return len(s.mask[0]) }
func (s sprite) h() int { return len(s.mask) }

func (s sprite) on(x, y int) bool {
	return y >= 0 && y < s.h() && x >= 0 && x < s.w() && s.mask[y][x] == '#'
}

// shade is the steel lightness of an anvil pixel: lit from the top left, a bright face, a
// highlight on every top edge and darker flanks.
func (s sprite) shade(x, y int) float64 {
	v := 0.5 - 0.04*float64(y-s.faceY) - 0.2*float64(x)/float64(s.w())
	switch {
	case y == s.faceY:
		v = 0.97 - 0.12*float64(x)/float64(s.w())
	case y <= s.faceY+2:
		v = 0.78 - 0.12*float64(y-s.faceY) - 0.15*float64(x)/float64(s.w())
	case !s.on(x, y-1):
		v += 0.28
	case !s.on(x-1, y):
		v += 0.12
	case !s.on(x+1, y) || !s.on(x, y+1):
		v -= 0.15
	}
	return min(1, max(0, v))
}

type spark struct{ x, y, vx, vy, age, life float64 }

// fx is the header's motion: ember sparks off the plate and the name's shimmer.
type fx struct {
	rng     *rand.Rand
	sparks  []spark
	shimmer bool
	band    float64
	bandW   int
	maxW    float64
	maxH    float64
}

func newFX(seed uint64) *fx {
	return &fx{rng: rand.New(rand.NewPCG(seed, seed^0x5eed))}
}

// strike throws n sparks up from the plate's floor and starts a shimmer pass over the name.
func (f *fx) strike(s sprite, n, nameW int) {
	f.maxW, f.maxH = float64(s.w()), float64(s.h())
	for i := 0; i < n; i++ {
		f.sparks = append(f.sparks, spark{
			x: s.hitX + f.rng.Float64()*4 - 2, y: float64(s.faceY) - 0.5,
			vx: (f.rng.Float64()*2 - 1) * 22, vy: -9 - f.rng.Float64()*13,
			life: 0.55 + f.rng.Float64()*0.6,
		})
	}
	if !f.shimmer {
		f.shimmer, f.band, f.bandW = true, -bandHalf, nameW
	}
}

func (f *fx) step() {
	dt := 1.0 / fps
	live := f.sparks[:0]
	for _, s := range f.sparks {
		s.vy += gravity * dt
		s.x += s.vx * dt
		s.y += s.vy * dt
		s.age += dt
		if s.age < s.life && s.x >= 0 && s.x < f.maxW && s.y < f.maxH {
			live = append(live, s)
		}
	}
	f.sparks = live
	if f.shimmer {
		f.band += float64(f.bandW+2*bandHalf) / (sweepSecs * fps)
		if f.band > float64(f.bandW+bandHalf) {
			f.shimmer = false
		}
	}
}

// headBar is the one-row header the chat and the dashboard share: ◆ smithy, the version and the
// project on the left, a state pill and the clock in the corner. When the width runs short the
// pill goes first, then the left side is cut.
func (t theme) headBar(w, gutter int, project string, pill func(bar lipgloss.Style) string, clock string) string {
	bar := lipgloss.NewStyle().Background(t.raised)
	if t.plain {
		bar = lipgloss.NewStyle()
	}
	p := strings.Repeat(" ", gutter)
	diamond := "*"
	if t.g.unicode {
		diamond = "◆"
	}
	name := bar.Foreground(t.strong).Bold(true).Render("smithy")
	if Version != "" {
		name += bar.Foreground(t.dim).Render(" " + Version)
	}
	left := bar.Render(p) + bar.Foreground(t.accent).Render(diamond+" ") + name
	if project != "" {
		left += bar.Foreground(t.dim).Render(" "+t.g.dot+" ") + bar.Foreground(t.muted).Render(project)
	}
	right := bar.Foreground(t.text).Render(clock) + bar.Render(p)
	if s := pill(bar); s != "" {
		if full := s + bar.Render("  ") + right; w-ansi.StringWidth(left)-ansi.StringWidth(full) >= 1 {
			right = full
		}
	}
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	if w-lw-rw >= 1 {
		return left + bar.Render(strings.Repeat(" ", w-lw-rw)) + right
	}
	if room := w - rw - 1; room >= 8 {
		left = ansi.Truncate(left, room, "")
		return left + bar.Render(strings.Repeat(" ", w-ansi.StringWidth(left)-rw)) + right
	}
	return ansi.Truncate(left, w, "")
}
