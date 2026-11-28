package kit

import (
	"math/rand/v2"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// fps is the frame rate while sparks, the shimmer, the cap meter or a running item move; at rest no frames run.
const Fps = 15

// breathSecs is one slow in-and-out of a running pill's dot.
const BreathSecs = 2.0

// wordB is the Calvin S name.
var WordB = []string{
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
type Sprite struct {
	mask  []string
	FaceY int // first face row
	hitX  float64
}

var BigPlate = Sprite{anvilBig, 3, 16}

func (s Sprite) W() int { return len(s.mask[0]) }
func (s Sprite) H() int { return len(s.mask) }

func (s Sprite) On(x, y int) bool {
	return y >= 0 && y < s.H() && x >= 0 && x < s.W() && s.mask[y][x] == '#'
}

// shade is the steel lightness of an anvil pixel: lit from the top left, a bright face, a
// highlight on every top edge and darker flanks.
func (s Sprite) Shade(x, y int) float64 {
	v := 0.5 - 0.04*float64(y-s.FaceY) - 0.2*float64(x)/float64(s.W())
	switch {
	case y == s.FaceY:
		v = 0.97 - 0.12*float64(x)/float64(s.W())
	case y <= s.FaceY+2:
		v = 0.78 - 0.12*float64(y-s.FaceY) - 0.15*float64(x)/float64(s.W())
	case !s.On(x, y-1):
		v += 0.28
	case !s.On(x-1, y):
		v += 0.12
	case !s.On(x+1, y) || !s.On(x, y+1):
		v -= 0.15
	}
	return min(1, max(0, v))
}

type Spark struct{ X, Y, vx, vy, Age, Life float64 }

// fx is the header's motion: ember sparks off the plate and the name's shimmer.
type fx struct {
	rng     *rand.Rand
	Sparks  []Spark
	shimmer bool
	band    float64
	bandW   int
	maxW    float64
	maxH    float64
}

func NewFX(seed uint64) *fx {
	return &fx{rng: rand.New(rand.NewPCG(seed, seed^0x5eed))}
}

// strike throws n sparks up from the plate's floor and starts a shimmer pass over the name.
func (f *fx) Strike(s Sprite, n, nameW int) {
	f.maxW, f.maxH = float64(s.W()), float64(s.H())
	for i := 0; i < n; i++ {
		f.Sparks = append(f.Sparks, Spark{
			X: s.hitX + f.rng.Float64()*4 - 2, Y: float64(s.FaceY) - 0.5,
			vx: (f.rng.Float64()*2 - 1) * 22, vy: -9 - f.rng.Float64()*13,
			Life: 0.55 + f.rng.Float64()*0.6,
		})
	}
	if !f.shimmer {
		f.shimmer, f.band, f.bandW = true, -bandHalf, nameW
	}
}

func (f *fx) Step() {
	dt := 1.0 / Fps
	live := f.Sparks[:0]
	for _, s := range f.Sparks {
		s.vy += gravity * dt
		s.X += s.vx * dt
		s.Y += s.vy * dt
		s.Age += dt
		if s.Age < s.Life && s.X >= 0 && s.X < f.maxW && s.Y < f.maxH {
			live = append(live, s)
		}
	}
	f.Sparks = live
	if f.shimmer {
		f.band += float64(f.bandW+2*bandHalf) / (sweepSecs * Fps)
		if f.band > float64(f.bandW+bandHalf) {
			f.shimmer = false
		}
	}
}

// headBar is the one-row header the chat and the dashboard share: ◆ smithy, the version and the
// project on the left, a state pill and the clock in the corner. When the width runs short the
// pill goes first, then the left side is cut.
func (t Theme) HeadBar(w, gutter int, project string, pill func(bar lipgloss.Style) string, clock string) string {
	bar := lipgloss.NewStyle().Background(t.Raised)
	if t.Plain {
		bar = lipgloss.NewStyle()
	}
	p := strings.Repeat(" ", gutter)
	diamond := "*"
	if t.G.Unicode {
		diamond = "◆"
	}
	block := bar.Foreground(t.Accent).Render(diamond+" ") + bar.Foreground(t.Strong).Bold(true).Render("smithy")
	if !t.Plain && t.Accent != nil {
		block = lipgloss.NewStyle().Background(t.Accent).Foreground(t.Surface).Bold(true).Render(" " + diamond + " smithy ")
	}
	name := block
	if Version != "" {
		name += bar.Foreground(t.Muted).Render(" " + Version)
	}
	left := bar.Render(p[:max(len(p)-1, 0)]) + name
	if project != "" {
		left += bar.Foreground(t.Dim).Render(" "+t.G.Dot+" ") + bar.Foreground(t.Muted).Render(project)
	}
	right := bar.Foreground(t.Text).Render(clock) + bar.Render(p)
	if !t.Plain && t.Overlay != nil {
		right = lipgloss.NewStyle().Background(t.Overlay).Foreground(t.Strong).Render(" "+clock+" ") + bar.Render(p)
	}
	if s := pill(bar); s != "" {
		if full := s + bar.Render(" ") + right; w-ansi.StringWidth(left)-ansi.StringWidth(full) >= 1 {
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
