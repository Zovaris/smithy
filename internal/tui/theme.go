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
	name                                    string // one of Themes
	dark, uni                               bool
	prof                                    colorprofile.Profile
}

// Themes are the ui.theme choices; steel is the default. Each theme is a whole palette: surfaces,
// text tiers, borders, the accent, the signals and the dither ramp.
var Themes = []string{"steel", "frost", "plum", "moss", "dune", "dusk", "rose"}

// pal is one theme: each token as light and dark hex (256-color terminals get the nearest match), the
// dark dither ramp, and the accent for 16 and 256 colors; 16-color terminals keep steel's other tokens.
type pal struct {
	accent, ok, warn, err, info, dim, text, strong, muted, surface, raised, focus, overlay, border [2]string
	ramp                                                                                           []string
	a16, a256                                                                                      string
}

var palettes = map[string]pal{
	"frost": {accent: [2]string{"#0E7C86", "#3DD6D0"}, ok: [2]string{"#1A8A58", "#5BD69A"}, warn: [2]string{"#956A00", "#E8C063"},
		err: [2]string{"#C23A3A", "#F27A7A"}, info: [2]string{"#1F68C4", "#7AB8FF"}, dim: [2]string{"#7F9CA5", "#6F8C95"},
		text: [2]string{"#24404A", "#B0C8CF"}, strong: [2]string{"#0A171B", "#E4F3F6"}, muted: [2]string{"#557680", "#7F9EA8"},
		surface: [2]string{"#F2F7F8", "#0B1418"}, raised: [2]string{"#E8F1F3", "#101C21"}, focus: [2]string{"#DCE9EC", "#162630"},
		overlay: [2]string{"#D0E1E5", "#1E313B"}, border: [2]string{"#B8CED4", "#28404B"},
		ramp: []string{"#0B1418", "#101C21", "#162630", "#1E313B", "#28404B", "#36566A"}, a16: "14", a256: "44"},
	"plum": {accent: [2]string{"#9333EA", "#C084FC"}, ok: [2]string{"#1F8A5B", "#4CC38A"}, warn: [2]string{"#9A6B00", "#E3B341"},
		err: [2]string{"#C2364F", "#F47089"}, info: [2]string{"#3B5FCF", "#8AB4FF"}, dim: [2]string{"#8E7C9C", "#8A7799"},
		text: [2]string{"#3A2A47", "#C9B9D4"}, strong: [2]string{"#160E1C", "#F2E9F7"}, muted: [2]string{"#6A5679", "#9A86A9"},
		surface: [2]string{"#F8F4FA", "#130E17"}, raised: [2]string{"#F1EAF5", "#1A1320"}, focus: [2]string{"#E7DDEE", "#221A2A"},
		overlay: [2]string{"#DCCFE6", "#2C2236"}, border: [2]string{"#C8B6D6", "#3A2D47"},
		ramp: []string{"#130E17", "#1A1320", "#221A2A", "#2C2236", "#3A2D47", "#4E3D60"}, a16: "13", a256: "170"},
	"moss": {accent: [2]string{"#4D7C0F", "#9BD35A"}, ok: [2]string{"#147D64", "#5CC7A8"}, warn: [2]string{"#946300", "#E3B341"},
		err: [2]string{"#BB3A2A", "#EF6F5E"}, info: [2]string{"#2366B8", "#6CB6FF"}, dim: [2]string{"#7F937F", "#74897A"},
		text: [2]string{"#2C3B2C", "#B5C7B4"}, strong: [2]string{"#0D150E", "#E8F2E5"}, muted: [2]string{"#5A6E5C", "#84998A"},
		surface: [2]string{"#F4F7F2", "#0D130F"}, raised: [2]string{"#EBF0E8", "#121A14"}, focus: [2]string{"#E0E8DC", "#18231B"},
		overlay: [2]string{"#D4DFCE", "#202E23"}, border: [2]string{"#BCCCB4", "#2A3B2D"},
		ramp: []string{"#0D130F", "#121A14", "#18231B", "#202E23", "#2A3B2D", "#3A5040"}, a16: "10", a256: "149"},
	"dune": {accent: [2]string{"#A86A00", "#F0B429"}, ok: [2]string{"#3F7D20", "#8CC56A"}, warn: [2]string{"#B9561A", "#F08A4B"},
		err: [2]string{"#BF3328", "#F0645A"}, info: [2]string{"#2F6AA8", "#7FB2E5"}, dim: [2]string{"#958770", "#8C7F68"},
		text: [2]string{"#43382A", "#CFC2A8"}, strong: [2]string{"#1A140B", "#F5EDDC"}, muted: [2]string{"#74664F", "#A0927A"},
		surface: [2]string{"#FAF6EE", "#16130E"}, raised: [2]string{"#F3EDE1", "#1D1912"}, focus: [2]string{"#EAE2D2", "#262118"},
		overlay: [2]string{"#DFD4C0", "#312A1F"}, border: [2]string{"#CCBFA6", "#3F3628"},
		ramp: []string{"#16130E", "#1D1912", "#262118", "#312A1F", "#3F3628", "#56493A"}, a16: "3", a256: "214"},
	"dusk": {accent: [2]string{"#4153D6", "#7C8CFF"}, ok: [2]string{"#1F8A5B", "#4CC38A"}, warn: [2]string{"#9A6B00", "#E3B341"},
		err: [2]string{"#C2362F", "#F47067"}, info: [2]string{"#137A99", "#5BC8E8"}, dim: [2]string{"#8188B0", "#757CA3"},
		text: [2]string{"#2D3358", "#B6BCE0"}, strong: [2]string{"#0D1024", "#E8EAFB"}, muted: [2]string{"#5C6390", "#868DB5"},
		surface: [2]string{"#F4F5FB", "#0E1020"}, raised: [2]string{"#EBEDF7", "#13162A"}, focus: [2]string{"#E0E3F2", "#1A1E36"},
		overlay: [2]string{"#D3D8EC", "#232844"}, border: [2]string{"#BCC2DE", "#2E3456"},
		ramp: []string{"#0E1020", "#13162A", "#1A1E36", "#232844", "#2E3456", "#404872"}, a16: "12", a256: "105"},
	"rose": {accent: [2]string{"#C2255C", "#FF6B9A"}, ok: [2]string{"#1F8A5B", "#4CC38A"}, warn: [2]string{"#9A6B00", "#E3B341"},
		err: [2]string{"#B8321F", "#E8553E"}, info: [2]string{"#2C66C2", "#7CB4FF"}, dim: [2]string{"#9A7B86", "#927580"},
		text: [2]string{"#46303A", "#D3B9C3"}, strong: [2]string{"#1C0D13", "#F8E8EE"}, muted: [2]string{"#7A5A65", "#A6858F"},
		surface: [2]string{"#FBF4F6", "#160F12"}, raised: [2]string{"#F5EAEE", "#1D1418"}, focus: [2]string{"#ECDDE3", "#271B20"},
		overlay: [2]string{"#E1CFD6", "#32232A"}, border: [2]string{"#CFB5BF", "#412E37"},
		ramp: []string{"#160F12", "#1D1418", "#271B20", "#32232A", "#412E37", "#5A4150"}, a16: "5", a256: "204"},
}

// namedTheme is newTheme in the palette of theme n; an unknown or empty name is steel.
func namedTheme(n string, dark bool, p colorprofile.Profile, unicode bool) theme {
	t := newTheme(dark, p, unicode)
	pl, ok := palettes[n]
	if !ok {
		return t
	}
	t.name = n
	if t.plain {
		return t
	}
	c := lipgloss.Color
	full := lipgloss.Complete(p)
	ld := lipgloss.LightDark(dark)
	t.accent = ld(full(c(pl.a16), c(pl.a256), c(pl.accent[0])), full(c(pl.a16), c(pl.a256), c(pl.accent[1])))
	if p < colorprofile.ANSI256 {
		return t
	}
	hex := func(v [2]string) color.Color { return ld(c(v[0]), c(v[1])) }
	t.ok, t.warn, t.err, t.info = hex(pl.ok), hex(pl.warn), hex(pl.err), hex(pl.info)
	t.dim, t.text, t.strong, t.muted = hex(pl.dim), hex(pl.text), hex(pl.strong), hex(pl.muted)
	t.surface, t.raised, t.focus, t.overlay, t.border = hex(pl.surface), hex(pl.raised), hex(pl.focus), hex(pl.overlay), hex(pl.border)
	t.ramp = make([]color.Color, len(pl.ramp))
	for i, h := range pl.ramp {
		t.ramp[i] = c(h)
	}
	return t
}

// renamed is t in theme n, same background, profile and glyphs.
func (t theme) renamed(n string) theme { return namedTheme(n, t.dark, t.prof, t.uni) }

type glyphs struct {
	sel, open, on, half, off, pass, fail, wait, dot, tee, elbow, pipe, bar, prompt string
	shades                                                                         []string // light → full
	ticks                                                                          []rune   // sparkline levels
	unicode                                                                        bool
}

var uniGlyphs = glyphs{sel: "▸", open: "▾", on: "●", half: "◐", off: "○", pass: "✓", fail: "✗", wait: "?", dot: "·", tee: "├─", elbow: "└─",
	pipe: "│", bar: "━", prompt: "›", shades: []string{"░", "▒", "▓", "█"}, ticks: []rune("▁▂▃▄▅▆▇█"), unicode: true}

var asciiGlyphs = glyphs{sel: ">", open: "v", on: "*", half: "~", off: "o", pass: "+", fail: "x", wait: "?", dot: "-", tee: "|-", elbow: "`-",
	pipe: "|", bar: "=", prompt: ">", shades: []string{".", "-", "=", "#"}, ticks: []rune("_.-=+*#@")}

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
		name:    "steel", dark: dark, uni: unicode, prof: p,
	}
	if unicode {
		t.g = uniGlyphs
	}
	t.ramp = []color.Color{c("#0E1116"), c("#131820"), c("#1A212B"), c("#222B37"), c("#2C3746"), c("#3B4A5E")}
	if p == colorprofile.ANSI256 {
		t.ramp = []color.Color{c("233"), c("234"), c("235"), c("236"), c("237"), c("239")}
	}
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
