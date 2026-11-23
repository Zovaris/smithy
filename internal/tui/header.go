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

const fps = 30

// The header art is a pixel grid two pixels per text row (half blocks): an anvil and the wordmark.
var anvilArt = []string{
	"                        ",
	" ###################### ",
	"   ####################  ",
	"      ##############    ",
	"         ########       ",
	"          ######        ",
	"          ######        ",
	"        ##########      ",
	"      ##############    ",
	"      ##############    ",
}

var font = map[rune][]string{
	's': {".###", "#...", ".##.", "...#", "###."},
	'm': {"##.#.", "#.#.#", "#.#.#", "#.#.#", "#.#.#"},
	'i': {"#", ".", "#", "#", "#"},
	't': {"#..", "###", "#..", "#..", ".##"},
	'h': {"#..", "#..", "###", "#.#", "#.#"},
	'y': {"#.#", "#.#", ".##", "..#", "##."},
}

const (
	artH    = 10 // pixels, 5 text rows
	anvilW  = 24
	wordX   = anvilW + 4
	faceY   = 1
	gravity = 70.0
)

// bayer is the 8×8 ordered-dither matrix, normalized to [0,1).
var bayer = func() [8][8]float64 {
	b2 := [2][2]int{{0, 2}, {3, 1}}
	var m [8][8]float64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			v := 0
			for s := 1; s <= 4; s *= 2 {
				v = v*4 + b2[(y/s)%2][(x/s)%2]
			}
			m[y][x] = (float64(v) + 0.5) / 64
		}
	}
	return m
}()

type pixel struct {
	kind byte    // 0 empty, 'a' anvil, 'w' wordmark
	v    float64 // shade 0..1
}

// base is the static pixel grid, built once.
var base, baseW = func() ([][]pixel, int) {
	word := "smithy"
	w := wordX
	for _, r := range word {
		w += 2*len(font[r][0]) + 2
	}
	g := make([][]pixel, artH)
	for y := range g {
		g[y] = make([]pixel, w)
	}
	for y, row := range anvilArt {
		for x, ch := range row {
			if ch == '#' {
				v := 0.95 - 0.07*float64(y) + 0.15*math.Sin(float64(x)/5)
				if y == faceY {
					v = 1
				}
				g[y][x] = pixel{'a', clamp01(v)}
			}
		}
	}
	x0 := wordX
	for _, r := range word {
		f := font[r]
		for fy, row := range f {
			for fx, ch := range row {
				if ch != '#' {
					continue
				}
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2; dx++ {
						y := fy*2 + dy
						g[y][x0+fx*2+dx] = pixel{'w', clamp01(1 - 0.06*float64(y))}
					}
				}
			}
		}
		x0 += 2*len(f[0]) + 2
	}
	return g, w
}()

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

type spark struct{ x, y, vx, vy, life, max float64 }

// fx is the header's motion state: sparks and the strike glow.
type fx struct {
	rng    *rand.Rand
	sparks []spark
	glow   float64
	glowV  float64
	spring harmonica.Spring
}

func newFX(seed uint64) *fx {
	return &fx{rng: rand.New(rand.NewPCG(seed, seed^0x5eed)), spring: harmonica.NewSpring(harmonica.FPS(fps), 3.2, 1.0)}
}

// strike heats the anvil face and throws sparks.
func (f *fx) strike(n int) {
	f.glow = 1
	for i := 0; i < n; i++ {
		f.sparks = append(f.sparks, spark{
			x: 4 + f.rng.Float64()*16, y: faceY,
			vx: (f.rng.Float64()*2 - 1) * 45, vy: -4 - f.rng.Float64()*12,
			life: 0, max: 0.6 + f.rng.Float64()*0.8,
		})
	}
}

func (f *fx) step() {
	dt := 1.0 / fps
	f.glow, f.glowV = f.spring.Update(f.glow, f.glowV, 0)
	if f.glow < 0.005 {
		f.glow, f.glowV = 0, 0
	}
	live := f.sparks[:0]
	for _, s := range f.sparks {
		s.vy += gravity * dt
		s.x += s.vx * dt
		s.y += s.vy * dt
		s.life += dt
		if s.life < s.max && s.x >= 0 && s.x < float64(baseW) && s.y < artH && s.y > -6 {
			live = append(live, s)
		}
	}
	f.sparks = live
}

func (f *fx) busy() bool { return f.glow > 0 || len(f.sparks) > 0 }

// header renders the art: dithered half blocks in 256/truecolor, shade glyphs otherwise.
func (m Model) art() string {
	t := m.th
	type cell struct {
		c    color.Color
		on   bool
		v    float64
		kind byte
	}
	grid := make([][]cell, artH)
	for y := range grid {
		grid[y] = make([]cell, baseW)
		for x, p := range base[y] {
			if p.kind == 0 {
				continue
			}
			grid[y][x] = cell{on: true, v: p.v, kind: p.kind}
		}
	}
	glow := 0.0
	if m.fx != nil {
		glow = m.fx.glow
		for _, s := range m.fx.sparks {
			x, y := int(s.x), int(s.y)
			if y >= 0 && y < artH && x >= 0 && x < baseW {
				grid[y][x] = cell{on: true, v: 1 - s.life/s.max, kind: 's'}
			}
		}
	}
	rich := m.profile >= colorprofile.ANSI256 && !t.plain && t.g.unicode
	ramp := func(c cell, x, y int) color.Color {
		r := t.steel
		v := c.v
		switch c.kind {
		case 'w':
			r = t.ember[2:]
			v = clamp01(0.25 + 0.75*float64(x-wordX)/float64(baseW-wordX)*c.v)
		case 's':
			r = t.spark
			v = 1 - c.v
		case 'a':
			if heat := glow * math.Max(0, 1-float64(y-faceY)/4); heat > 0.02 {
				r = t.ember
				v = clamp01(heat * c.v)
			}
		}
		lv := v * float64(len(r)-1)
		i := int(lv)
		if lv-float64(i) > bayer[y%8][x%8] && i < len(r)-1 {
			i++
		}
		return r[i]
	}
	var b strings.Builder
	for row := 0; row < artH/2; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		for x := 0; x < baseW; x++ {
			top, bot := grid[row*2][x], grid[row*2+1][x]
			if !top.on && !bot.on {
				b.WriteByte(' ')
				continue
			}
			if !rich {
				c := top
				if !c.on || (bot.on && bot.kind == 's') {
					c = bot
				}
				v := (top.v + bot.v) / 2
				if !top.on || !bot.on {
					v /= 2
				}
				sh := t.g.shades[min(int(v*float64(len(t.g.shades))), len(t.g.shades)-1)]
				col := t.muted
				switch c.kind {
				case 'w':
					col = t.accent
				case 's':
					col = t.warn
				}
				b.WriteString(t.fg(col).Render(sh))
				continue
			}
			switch {
			case top.on && bot.on:
				b.WriteString(lipgloss.NewStyle().Foreground(ramp(top, x, row*2)).Background(ramp(bot, x, row*2+1)).Render("▀"))
			case top.on:
				b.WriteString(lipgloss.NewStyle().Foreground(ramp(top, x, row*2)).Render("▀"))
			default:
				b.WriteString(lipgloss.NewStyle().Foreground(ramp(bot, x, row*2+1)).Render("▄"))
			}
		}
	}
	return b.String()
}

// artWidth is the header art's width in cells.
func artWidth() int { return baseW }
