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
