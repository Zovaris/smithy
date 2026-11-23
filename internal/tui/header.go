package tui

import (
	"image/color"
	"math"
	"math/rand/v2"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/harmonica"
)

// fps is the frame rate while sparks, the glow, the shimmer or the cap meter move; at rest no frames run.
const fps = 24

// anvilBig is the 30×14-pixel anvil (7 text rows of half blocks): three rows of headroom for
// sparks, a face with the horn to the left and the heel to the right, a narrow waist and a
// flared base. anvilSmall is the 16×8 one for narrow terminals.
var anvilBig = []string{
	"                              ",
	"                              ",
	"                              ",
	"  ##########################  ",
	"     #######################  ",
	"         ###################  ",
	"            ##############    ",
	"              ##########      ",
	"               ########       ",
	"               ########       ",
	"             ############     ",
	"           ################   ",
	"          ##################  ",
	"          ##################  ",
}

var anvilSmall = []string{
	"                ",
	"  ############  ",
	"     #########  ",
	"       ######   ",
	"        ####    ",
	"        ####    ",
	"      ########  ",
	"      ########  ",
}

// wordB is the Calvin S name.
var wordB = []string{
	"╔═╗╔╦╗╦╔╦╗╦ ╦╦ ╦",
	"╚═╗║║║║ ║ ╠═╣╚╦╝",
	"╚═╝╩ ╩╩ ╩ ╩ ╩ ╩ ",
}

const (
	bandHalf  = 3
	bandMix   = 0.25
	sweepSecs = 2.5
	idleTicks = 5 // refresh ticks (2 s) between idle strikes
	gravity   = 55.0
)

var bayer4 = [4][4]float64{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}

type sprite struct {
	mask  []string
	faceY int // first face row
	hitX  float64
}

var (
	bigAnvil   = sprite{anvilBig, 3, 18}
	smallAnvil = sprite{anvilSmall, 1, 9}
)

func (s sprite) w() int { return len(s.mask[0]) }
func (s sprite) h() int { return len(s.mask) }
func (s sprite) on(x, y int) bool {
	return y >= 0 && y < s.h() && x >= 0 && x < s.w() && s.mask[y][x] == '#'
}

// shade is the steel lightness of an anvil pixel: lit from the top left, with a bright face, a
// highlight on every top edge and darker flanks.
func (s sprite) shade(x, y int) float64 {
	v := 0.62 - 0.05*float64(y-s.faceY) - 0.22*float64(x)/float64(s.w())
	switch {
	case y == s.faceY:
		v = 0.95 - 0.15*float64(x)/float64(s.w())
	case !s.on(x, y-1):
		v += 0.28
	case !s.on(x-1, y):
		v += 0.12
	case !s.on(x+1, y) || !s.on(x, y+1):
		v -= 0.15
	}
	return clamp01(v)
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

type spark struct{ x, y, vx, vy, age, life float64 }

// fx is the header's motion: sparks off the face, the face's heat glow and the name's shimmer.
type fx struct {
	rng     *rand.Rand
	sparks  []spark
	glow    float64
	glowV   float64
	spring  harmonica.Spring
	shimmer bool
	band    float64
	bandW   int
	hitX    float64
	faceY   float64
	maxW    float64
	maxH    float64
}

func newFX(seed uint64) *fx {
	return &fx{rng: rand.New(rand.NewPCG(seed, seed^0x5eed)), spring: harmonica.NewSpring(harmonica.FPS(fps), 3.0, 0.9)}
}

// strike heats the face, throws n sparks and starts a shimmer pass over the name.
func (f *fx) strike(s sprite, n, nameW int) {
	f.glow, f.glowV = 1, 0
	f.hitX, f.faceY, f.maxW, f.maxH = s.hitX, float64(s.faceY), float64(s.w()), float64(s.h())
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
	f.glow, f.glowV = f.spring.Update(f.glow, f.glowV, 0)
	if f.glow < 0.01 && math.Abs(f.glowV) < 0.01 {
		f.glow, f.glowV = 0, 0
	}
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

func (f *fx) busy() bool { return f.glow > 0 || len(f.sparks) > 0 || f.shimmer }

// anvil draws a sprite: Bayer-dithered steel in half blocks, the face glowing by the strike,
// sparks white→gold→orange→red by age. Without 256 colors it falls back to shade glyphs.
func (m Model) anvil(s sprite) []string {
	t := m.th
	type px struct {
		on bool
		c  color.Color
		v  float64
		k  byte
	}
	w, h := s.w(), s.h()
	grid := make([][]px, h)
	glow := 0.0
	if m.motion {
		glow = m.fx.glow
	}
	rich := !t.plain && t.g.unicode && m.profile >= colorprofile.ANSI256
	pick := func(r []color.Color, v float64, x, y int) color.Color {
		lv := clamp01(v) * float64(len(r)-1)
		i := int(lv)
		if lv-float64(i) > (bayer4[y%4][x%4]+0.5)/16 && i < len(r)-1 {
			i++
		}
		return r[i]
	}
	for y := 0; y < h; y++ {
		grid[y] = make([]px, w)
		for x := 0; x < w; x++ {
			if !s.on(x, y) {
				continue
			}
			v := s.shade(x, y)
			p := px{on: true, v: v, k: 'a'}
			if rich {
				p.c = pick(t.steel, v, x, y)
				heat := glow * math.Max(0, 1-math.Abs(float64(x)-s.hitX)/9) * math.Max(0, 1-float64(y-s.faceY)/2.5)
				if heat > 0.04 {
					p.c = pick(t.heat, heat, x, y)
				}
			}
			grid[y][x] = p
		}
	}
	if m.motion {
		for _, sp := range m.fx.sparks {
			x, y := int(sp.x), int(sp.y)
			if y >= 0 && y < h && x >= 0 && x < w {
				p := px{on: true, v: 1, k: 's'}
				if rich {
					p.c = pick(t.spark, sp.age/sp.life, x, y)
				}
				grid[y][x] = p
			}
		}
	}
	out := make([]string, h/2)
	for row := range out {
		var b strings.Builder
		for x := 0; x < w; x++ {
			top, bot := grid[row*2][x], grid[row*2+1][x]
			switch {
			case !top.on && !bot.on:
				b.WriteByte(' ')
			case !rich:
				c, v := top, (top.v+bot.v)/2
				if !top.on || bot.k == 's' {
					c = bot
				}
				if !top.on || !bot.on {
					v *= 0.6
				}
				g := t.g.shades[min(int(v*float64(len(t.g.shades))), len(t.g.shades)-1)]
				col := t.muted
				if c.k == 's' {
					col, g = t.warn, "*"
				}
				b.WriteString(t.fg(col).Render(g))
			case top.on && bot.on:
				b.WriteString(lipgloss.NewStyle().Foreground(top.c).Background(bot.c).Render("▀"))
			case top.on:
				b.WriteString(lipgloss.NewStyle().Foreground(top.c).Render("▀"))
			default:
				b.WriteString(lipgloss.NewStyle().Foreground(bot.c).Render("▄"))
			}
		}
		out[row] = b.String()
	}
	return out
}

// wordmark colors the name per column ember→gold, with the shimmer band when one sweeps.
func (m Model) wordmark() []string {
	t := m.th
	w := len([]rune(wordB[0]))
	rich := !t.plain && m.profile >= colorprofile.ANSI256
	var grad []color.Color
	if rich {
		grad = lipgloss.Blend1D(w, t.forge...)
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
				b.WriteString(t.fg(t.accent).Render(string(r)))
			default:
				c := grad[x]
				if shimmer {
					if d := math.Abs(float64(x) - m.fx.band); d < bandHalf {
						c = lipgloss.Blend1D(11, c, t.warmWhite)[int(bandMix*(1-d/bandHalf)*10+0.5)]
					}
				}
				b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(r)))
			}
		}
		out[y] = b.String()
	}
	return out
}
