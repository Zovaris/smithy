package tui

import (
	"image/color"
	"math"
	"math/rand/v2"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// fps is the frame rate while sparks, the shimmer, the cap meter or a running item move; at rest no frames run.
const fps = 15

// wordB is the Calvin S name.
var wordB = []string{
	"╔═╗╔╦╗╦╔╦╗╦ ╦╦ ╦",
	"╚═╗║║║║ ║ ╠═╣╚╦╝",
	"╚═╝╩ ╩╩ ╩ ╩ ╩ ╩ ",
}

const (
	bandHalf  = 3
	sweepSecs = 2.5
	idleTicks = 5 // refresh ticks (2 s) between idle strikes
	gravity   = 55.0
)

// anvilBig is the 30×14-pixel anvil (7 text rows of half blocks): three rows of headroom for
// sparks, a flat face that ends in a pointed horn on the left and a square heel on the right, a
// narrow waist and a wide base on two feet. anvilSmall is the 16×8 one.
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

var anvilSmall = []string{
	"                ",
	"  ###########   ",
	"#############   ",
	"  ###########   ",
	"     #####      ",
	"      ###       ",
	"    #######     ",
	"   ###   ###    ",
}

// sprite is the header anvil in pixels (two per text row) and where sparks fly from.
type sprite struct {
	mask  []string
	faceY int // first face row
	hitX  float64
}

var (
	bigPlate   = sprite{anvilBig, 3, 16}
	smallPlate = sprite{anvilSmall, 1, 8}
)

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

func (f *fx) busy() bool { return len(f.sparks) > 0 || f.shimmer }

// plateRows draws the header plate for s, with sparks only while motion is on.
func (m Model) plateRows(s sprite) []string {
	var sp []spark
	if m.motion {
		sp = m.fx.sparks
	}
	t := m.th
	if m.profile < colorprofile.ANSI256 {
		t.ramp = nil
	}
	return t.plate(s, sp)
}

// wordmark draws the name in steel, dim → base across, with the shimmer band when one sweeps.
func (m Model) wordmark() []string {
	t := m.th
	w := len([]rune(wordB[0]))
	rich := !t.plain && m.profile >= colorprofile.ANSI256
	var grad []color.Color
	if rich {
		grad = lipgloss.Blend1D(w, t.dim, t.text)
	}
	shimmer := m.motion && m.fx.shimmer
	out := make([]string, len(wordB))
	for y, row := range wordB {
		var b strings.Builder
		for x, r := range []rune(row) {
			switch {
			case r == ' ' || t.plain:
				b.WriteRune(r)
			case !rich:
				b.WriteString(t.fg(t.strong).Bold(true).Render(string(r)))
			default:
				c := grad[x]
				if shimmer && math.Abs(float64(x)-m.fx.band) < bandHalf && 1-math.Abs(float64(x)-m.fx.band)/bandHalf > bayerAt(x, y) {
					c = t.strong
				}
				b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(r)))
			}
		}
		out[y] = b.String()
	}
	return out
}
