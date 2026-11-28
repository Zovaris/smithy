package kit

import (
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

// The dither kit: a 4×4 Bayer threshold turns a level between two colors into a stable pattern,
// so gradients, edges and moving bands read smoothly even at 256 colors.

var bayer4 = [4][4]float64{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}

// bayerAt is the threshold in (0, 1) for cell x, y.
func bayerAt(x, y int) float64 { return (bayer4[y&3][x&3] + 0.5) / 16 }

// dpick picks the ramp color for level v in [0, 1], dithered at x, y.
func Dpick(r []color.Color, v float64, x, y int) color.Color {
	lv := Clamp01(v) * float64(len(r)-1)
	i := int(lv)
	if lv-float64(i) > bayerAt(x, y) && i < len(r)-1 {
		i++
	}
	return r[i]
}

func Clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// shimmerHalf is how far the shimmer band reaches each side of its center, in cells.
const ShimmerHalf = 5

// shimmer draws running text dim with a strong band centered on cell pos; the band's edge is
// dithered so it slides without jumping.
func (t Theme) Shimmer(s string, pos float64) string {
	if t.Plain {
		return s
	}
	var b strings.Builder
	for x, r := range []rune(s) {
		c := t.Dim
		if d := math.Abs(float64(x) - pos); d <= ShimmerHalf && 1-d/(ShimmerHalf+1) > bayerAt(x, 0) {
			c = t.Strong
		}
		b.WriteString(t.Fg(c).Render(string(r)))
	}
	return b.String()
}

// ditherBar is a w-cell progress bar: █ filled in base text, a three-cell ▓▒░ edge with the
// ember at its front, ░ for the rest.
func (t Theme) DitherBar(w int, frac float64) string {
	n := int(Clamp01(frac)*float64(w) + 0.5)
	sh := t.G.Shades
	var b strings.Builder
	for x := 0; x < w; x++ {
		g, c := sh[0], t.Border
		switch {
		case x < n:
			g, c = sh[3], t.Text
		case n > 0 && x < n+3:
			g, c = sh[2-(x-n)], t.Dim
			if x == n {
				c = t.Accent
			}
		}
		b.WriteString(t.Fg(c).Render(g))
	}
	return b.String()
}

// loadingField is a w-cell field of ' ·░▒▓' whose levels drift with phase.
func (t Theme) LoadingField(w, phase int) string {
	lv := []string{" ", "·", "░", "▒", "▓"}
	if !t.G.Unicode {
		lv = []string{" ", ".", ":", "-", "="}
	}
	var b strings.Builder
	for x := 0; x < w; x++ {
		v := 0.5 + 0.5*math.Sin(float64(x+phase)/2.5)
		i := int(v * float64(len(lv)-1))
		if v*float64(len(lv)-1)-float64(i) > bayerAt(x, phase) && i < len(lv)-1 {
			i++
		}
		b.WriteString(t.Fg(t.Dim).Render(lv[i]))
	}
	return b.String()
}

// plate is the header's half-block gradient: w cells by rows text rows (two pixels each) on the
// steel ramp, lit from the top left, with ember sparks that cool into the ramp by age. Without unicode blocks or 256
// colors it falls back to shade glyphs.
func (t Theme) Plate(s Sprite, sparks []Spark) []string {
	w, ph := s.W(), s.H()
	rows := ph / 2
	sp := map[[2]int]float64{}
	for _, k := range sparks {
		x, y := int(k.X), int(k.Y)
		if x >= 0 && x < w && y >= 0 && y < ph && !s.On(x, y) {
			sp[[2]int{x, y}] = k.Age / k.Life
		}
	}
	rich := !t.Plain && t.Ramp != nil && t.G.Unicode
	// px is one pixel's color, or nil where there is neither steel nor a spark.
	px := func(x, y int) color.Color {
		if age, ok := sp[[2]int{x, y}]; ok {
			if !rich {
				return t.Accent
			}
			return Dpick([]color.Color{t.Accent, t.Ramp[len(t.Ramp)-1]}, age, x, y)
		}
		if !s.On(x, y) {
			return nil
		}
		if !rich {
			return t.Border
		}
		return Dpick(t.Ramp, s.Shade(x, y), x, y)
	}
	out := make([]string, rows)
	for row := range out {
		var b strings.Builder
		for x := 0; x < w; x++ {
			top, bot := px(x, row*2), px(x, row*2+1)
			switch {
			case top == nil && bot == nil:
				b.WriteByte(' ')
			case !rich:
				g := t.G.Shades[len(t.G.Shades)-1]
				if _, ok := sp[[2]int{x, row * 2}]; ok {
					g = "*"
				} else if _, ok := sp[[2]int{x, row*2 + 1}]; ok {
					g = "*"
				}
				c := top
				if c == nil {
					c = bot
				}
				b.WriteString(t.Fg(c).Render(g))
			case bot == nil:
				b.WriteString(lipgloss.NewStyle().Foreground(top).Render("▀"))
			case top == nil:
				b.WriteString(lipgloss.NewStyle().Foreground(bot).Render("▄"))
			default:
				b.WriteString(lipgloss.NewStyle().Foreground(top).Background(bot).Render("▀"))
			}
		}
		out[row] = b.String()
	}
	return out
}
