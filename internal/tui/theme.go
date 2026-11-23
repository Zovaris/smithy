package tui

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// theme is the semantic palette and glyph set for one background, color profile and charset.
type theme struct {
	accent, ok, warn, err, muted, text, surface, border color.Color
	ember                                               []color.Color // header and progress gradient, dark → bright
	spark                                               []color.Color // spark life ramp, hot → cold
	steel                                               []color.Color // anvil shading, dark → light
	plain                                               bool          // no color: lean on bold and reverse
	g                                                   glyphs
}

type glyphs struct {
	sel, on, half, off, pass, fail, dot, tee, elbow, pipe, bar string
	shades                                                     []string // light → full
	ticks                                                      []rune   // sparkline levels
	unicode                                                    bool
}

var uniGlyphs = glyphs{sel: "▸", on: "●", half: "◐", off: "○", pass: "✓", fail: "✗", dot: "·", tee: "├─", elbow: "└─",
	pipe: "│", bar: "━", shades: []string{"░", "▒", "▓", "█"}, ticks: []rune("▁▂▃▄▅▆▇█"), unicode: true}

var asciiGlyphs = glyphs{sel: ">", on: "*", half: "~", off: "o", pass: "+", fail: "x", dot: "-", tee: "|-", elbow: "`-",
	pipe: "|", bar: "=", shades: []string{".", ":", "#", "@"}, ticks: []rune("_.-=+*#@")}

// utf8Locale reports whether the locale can draw box and block glyphs.
func utf8Locale() bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(k); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return true
}

func newTheme(dark bool, p colorprofile.Profile, unicode bool) theme {
	c := lipgloss.Color
	full := lipgloss.Complete(p)
	ld := lipgloss.LightDark(dark)
	tok := func(a16, a256, light, darkHex string) color.Color {
		return ld(full(c(a16), c(a256), c(light)), full(c(a16), c(a256), c(darkHex)))
	}
	t := theme{
		accent:  tok("11", "214", "#C2410C", "#FFB547"),
		ok:      tok("10", "78", "#15803D", "#5FD38D"),
		warn:    tok("3", "179", "#A16207", "#F2C14E"),
		err:     tok("9", "203", "#B91C1C", "#FF6B6B"),
		muted:   tok("8", "244", "#78716C", "#7A7470"),
		text:    tok("7", "253", "#1C1917", "#ECE7E2"),
		surface: tok("0", "236", "#F5F0EB", "#1E1B19"),
		border:  tok("8", "239", "#D6CFC8", "#3A3532"),
		g:       asciiGlyphs,
	}
	if unicode {
		t.g = uniGlyphs
	}
	t.ember = lipgloss.Blend1D(8, c("#4A0D05"), c("#B3260B"), c("#F25C05"), c("#FFB547"), c("#FFF1C1"))
	t.spark = lipgloss.Blend1D(8, c("#FFFFFF"), c("#FFD36E"), c("#F25C05"), c("#7A1204"))
	t.steel = lipgloss.Blend1D(6, c("#1A1A1F"), c("#3B3D46"), c("#6B6F7D"), c("#B9BDC9"))
	if !dark {
		t.steel = lipgloss.Blend1D(6, c("#2B2D33"), c("#4A4D57"), c("#7C8090"), c("#A9AEBD"))
	}
	if p <= colorprofile.Ascii {
		t.plain = true
		t.accent, t.ok, t.warn, t.err, t.muted, t.text, t.surface, t.border = nil, nil, nil, nil, nil, nil, nil, nil
	}
	return t
}

func (t theme) fg(c color.Color) lipgloss.Style {
	if c == nil {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(c)
}
