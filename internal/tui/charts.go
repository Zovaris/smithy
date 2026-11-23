package tui

import "strings"

// sparkline draws the last w values as level glyphs scaled to the largest value.
func sparkline(vals []int, w int, ticks []rune) string {
	if len(vals) > w {
		vals = vals[len(vals)-w:]
	}
	hi := 0
	for _, v := range vals {
		hi = max(hi, v)
	}
	var b strings.Builder
	for _, v := range vals {
		i := 0
		if hi > 0 {
			i = v * (len(ticks) - 1) / hi
		}
		b.WriteRune(ticks[i])
	}
	return b.String()
}

// braille draws vals as a filled area chart in w×h cells (2×4 dots each), scaled to top,
// with a dotted line at limit when it fits.
func braille(vals []int, top, limit, w, h int) []string {
	dw, dh := w*2, h*4
	if top < 1 {
		top = 1
	}
	dots := make([][]bool, dh)
	for i := range dots {
		dots[i] = make([]bool, dw)
	}
	if len(vals) > dw {
		vals = vals[len(vals)-dw:]
	}
	off := dw - len(vals)
	for i, v := range vals {
		y := min(v*dh/top, dh)
		if v > 0 && y == 0 {
			y = 1
		}
		for k := 0; k < y; k++ {
			dots[dh-1-k][off+i] = true
		}
	}
	if limit > 0 && limit <= top {
		ly := dh - limit*dh/top
		if ly >= 0 && ly < dh {
			for x := 0; x < dw; x += 3 {
				dots[ly][x] = true
			}
		}
	}
	bits := [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}
	out := make([]string, h)
	for r := 0; r < h; r++ {
		var b strings.Builder
		for c := 0; c < w; c++ {
			var ch rune = 0x2800
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					if dots[r*4+dy][c*2+dx] {
						ch |= bits[dy][dx]
					}
				}
			}
			b.WriteRune(ch)
		}
		out[r] = b.String()
	}
	return out
}
