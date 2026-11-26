// Package text is the one cleaner for outside text (issue titles, API errors, card fields)
// before it reaches a terminal or a log line.
package text

import "strings"

// StripEscapes drops ANSI escape sequences (CSI, OSC and two-byte ones) and turns every other
// control character, C1 control and Unicode line separator into a space; tabs stay when keepTab.
func StripEscapes(s string, keepTab bool) string {
	var b strings.Builder
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == 0x1b && i+1 < len(r) && r[i+1] == '[':
			for i += 2; i < len(r) && (r[i] < 0x40 || r[i] > 0x7e); i++ { //revive:disable-line:empty-block skips the CSI body
			}
		case c == 0x1b && i+1 < len(r) && r[i+1] == ']':
			for i += 2; i < len(r) && r[i] != 0x07 && !(r[i] == 0x1b && i+1 < len(r) && r[i+1] == '\\'); i++ { //revive:disable-line:empty-block skips the OSC body
			}
			if i < len(r) && r[i] == 0x1b {
				i++
			}
		case c == 0x1b:
			i++
		case c == '\t' && keepTab:
			b.WriteRune(c)
		case c < 0x20 || c == 0x7f || (c >= 0x80 && c < 0xa0) || c == ' ' || c == ' ':
			b.WriteRune(' ')
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// Clean makes s one safe line: no escape sequences, control characters or line breaks, and
// runs of spaces collapsed.
func Clean(s string) string { return strings.Join(strings.Fields(StripEscapes(s, false)), " ") }
