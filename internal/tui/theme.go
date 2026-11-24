package tui

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// theme is the Steel palette (TK-28) and glyph set for one background, color profile and charset.
// Depth comes from the background tiers, not boxes; the one ember marks what runs or has focus.
type theme struct {
	accent, ok, warn, err, info             color.Color   // ember and the signals: ok, waits on you, failed, link
	dim, text, strong, muted                color.Color   // text tiers
	surface, raised, focus, overlay, border color.Color   // background tiers: screen, blocks, open block, panels
	ramp                                    []color.Color // steel ramp for dither, dark → light
	plain                                   bool          // no color: lean on bold and reverse
	g                                       glyphs
}

type glyphs struct {
	sel, on, half, off, pass, fail, wait, dot, tee, elbow, pipe, bar string
	shades                                                           []string // light → full
	ticks                                                            []rune   // sparkline levels
	unicode                                                          bool
}

var uniGlyphs = glyphs{sel: "▸", on: "●", half: "◐", off: "○", pass: "✓", fail: "✗", wait: "?", dot: "·", tee: "├─", elbow: "└─",
	pipe: "│", bar: "━", shades: []string{"░", "▒", "▓", "█"}, ticks: []rune("▁▂▃▄▅▆▇█"), unicode: true}

var asciiGlyphs = glyphs{sel: ">", on: "*", half: "~", off: "o", pass: "+", fail: "x", wait: "?", dot: "-", tee: "|-", elbow: "`-",
	pipe: "|", bar: "=", shades: []string{".", "-", "=", "#"}, ticks: []rune("_.-=+*#@")}

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
		accent:  tok("11", "208", "#C2541A", "#F2762E"),
		ok:      tok("10", "78", "#1F8A5B", "#4CC38A"),
		warn:    tok("3", "178", "#9A6B00", "#E3B341"),
		err:     tok("9", "203", "#C2362F", "#F47067"),
		info:    tok("12", "75", "#1F6FCF", "#6CB6FF"),
		dim:     tok("8", "103", "#8592A3", "#7A879A"),
		text:    tok("7", "251", "#2C3746", "#B4C0CE"),
		strong:  tok("15", "255", "#0E1116", "#E6EDF5"),
		muted:   tok("8", "103", "#5E6B7D", "#8593A6"),
		surface: tok("0", "233", "#F4F6F9", "#0E1116"),
		raised:  tok("0", "234", "#EBEFF4", "#131820"),
		focus:   tok("0", "235", "#E1E7EE", "#1A212B"),
		overlay: tok("0", "236", "#D8E0E9", "#222B37"),
		border:  tok("8", "237", "#C3CDD9", "#2C3746"),
		g:       asciiGlyphs,
	}
	if unicode {
		t.g = uniGlyphs
	}
	t.ramp = []color.Color{c("#0E1116"), c("#131820"), c("#1A212B"), c("#222B37"), c("#2C3746"), c("#3B4A5E")}
	if p <= colorprofile.Ascii {
		t.plain = true
		t.accent, t.ok, t.warn, t.err, t.info = nil, nil, nil, nil, nil
		t.dim, t.text, t.strong, t.muted = nil, nil, nil, nil
		t.surface, t.raised, t.focus, t.overlay, t.border = nil, nil, nil, nil, nil
	}
	return t
}

func (t theme) fg(c color.Color) lipgloss.Style {
	if c == nil {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(c)
}
