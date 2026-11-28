//go:build harness

package chat

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/tui/kit"
)

// configGroups names the groups /config shows, in order, by a setting key's first part.
var configGroups = []struct {
	name     string
	prefixes []string
}{
	{"Look", []string{"ui", "chat"}},
	{"Agent", []string{"agent", "models", "kit"}},
	{"Tend", []string{"tend"}},
	{"Serve", []string{"auto", "serve", "daemon"}},
	{"Notify", []string{"notify", "connect"}},
}

// configGroup is the group key k is listed under; "Other" for the rest.
func configGroup(k string) string {
	head, _, _ := strings.Cut(k, ".")
	for _, g := range configGroups {
		if slices.Contains(g.prefixes, head) {
			return g.name
		}
	}
	return "Other"
}

// configRows is the settings /config lists for query q: matched on key or meaning, ordered by
// group, then key.
func (m chatView) configRows() []api.SettingRow {
	q := strings.ToLower(m.pal.query)
	var rows []api.SettingRow
	for _, r := range append(m.localRows(), m.palSet...) {
		if q == "" || strings.Contains(strings.ToLower(r.Key+" "+r.Meaning), q) {
			rows = append(rows, r)
		}
	}
	rank := func(k string) int {
		g := configGroup(k)
		for i, c := range configGroups {
			if c.name == g {
				return i
			}
		}
		return len(configGroups)
	}
	slices.SortStableFunc(rows, func(a, b api.SettingRow) int {
		return cmp.Or(cmp.Compare(rank(a.Key), rank(b.Key)), cmp.Compare(a.Key, b.Key))
	})
	return rows
}

// localRows are the settings of this chat /config lists beside the daemon's: the transcript mode,
// the sidebar and serve.
func (m chatView) localRows() []api.SettingRow {
	side := "shown"
	if m.sideOff {
		side = "hidden"
	}
	serve := "on"
	for _, r := range m.palSet {
		if r.Key == "auto.paused" && cmp.Or(r.Value, r.Default) == "true" {
			serve = "off"
		}
	}
	return []api.SettingRow{
		{Key: "chat.transcript", Value: m.mode.String(), Default: "minimal", Src: "this chat", Kind: "enum", Options: []string{"minimal", "expandable", "verbose"},
			Meaning: "how much of each turn the transcript shows: minimal folds tools into one line, verbose shows every output (ctrl+o)"},
		{Key: "chat.sidebar", Value: side, Default: "shown", Src: "this chat", Kind: "enum", Options: []string{"shown", "hidden"},
			Meaning: "the right column with context, spend and links; it hides by itself under 120 columns (/sidebar)"},
		{Key: "serve", Value: serve, Default: "on", Src: "auto.paused", Kind: "enum", Options: []string{"on", "off"},
			Meaning: "serve mode: on launches sessions for labeled issues, off launches nothing new (/serve)"},
	}
}

// openConfig is /config: the settings modal, filtered by arg when given.
func (m chatView) openConfig(arg string) (tea.Model, tea.Cmd) {
	m.overlay, m.pal = overlayConfig, palette{query: arg}
	return m, m.openPaletteLoad()
}

// openPaletteLoad fetches the daemon's settings for the palette and /config.
func (m chatView) openPaletteLoad() tea.Cmd {
	a := m.cfg
	if a == nil {
		return nil
	}
	return func() tea.Msg {
		s, err := a.Settings(context.Background())
		if err != nil {
			return sessionErrMsg{what: "reading settings", err: err}
		}
		return paletteSettingsMsg{s.Settings}
	}
}

// edited reports whether r differs from its default.
func edited(r api.SettingRow) bool { return r.Value != "" && r.Value != r.Default }

func (m chatView) configKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := m.configRows()
	k := msg.String()
	switch k {
	case "esc":
		if m.pal.query != "" {
			m.pal = palette{}
			return m, nil
		}
		m.overlay = overlayNone
		return m, nil
	case "up":
		m.pal.sel = max(m.pal.sel-1, 0)
		return m, nil
	case "down", "tab":
		m.pal.sel = min(m.pal.sel+1, max(len(rows)-1, 0))
		return m, nil
	case "ctrl+u":
		m.pal = palette{}
		return m, nil
	case "backspace":
		if r := []rune(m.pal.query); len(r) > 0 {
			m.pal = palette{query: string(r[:len(r)-1])}
		}
		return m, nil
	case "left", "right":
		if m.pal.sel >= len(rows) {
			return m, nil
		}
		dir := 1
		if k == "left" {
			dir = -1
		}
		return m.cycleSetting(rows[m.pal.sel], dir)
	case "enter":
		if m.pal.sel < len(rows) {
			r := rows[m.pal.sel]
			if len(r.Options) > 0 {
				return m.cycleSetting(r, 1)
			}
			m.overlay = overlayNone
			m.input.SetValue("/set " + r.Key + " ")
			m.input.CursorEnd()
		}
		return m, nil
	}
	if t := msg.Text; t != "" && k != "space" || k == "space" && m.pal.query != "" {
		if k == "space" {
			t = " "
		}
		m.pal = palette{query: m.pal.query + t}
	}
	return m, nil
}

// cycleSetting steps r's value by dir through its options and saves it; ui.theme also re-themes
// this chat at once.
func (m chatView) cycleSetting(r api.SettingRow, dir int) (tea.Model, tea.Cmd) {
	opts := r.Options
	if len(opts) == 0 {
		return m, nil
	}
	val := cmp.Or(r.Value, r.Default)
	i := slices.Index(opts, val)
	v := opts[((i+dir)%len(opts)+len(opts))%len(opts)]
	if i < 0 && dir < 0 {
		v = opts[len(opts)-1]
	}
	for j := range m.palSet {
		if m.palSet[j].Key == r.Key {
			m.palSet[j].Value = v
		}
	}
	m.vb = valueBump{key: r.Key, still: m.still}.start()
	switch r.Key {
	case "chat.transcript":
		for c := modeMinimal; c <= modeVerbose; c++ {
			if c.String() == v {
				m.setMode(c)
			}
		}
		return m, nil
	case "chat.sidebar":
		m.sideOff = v == "hidden"
		m.ver++
		*m.at = anchor{}
		return m, nil
	case "serve":
		for j := range m.palSet {
			if m.palSet[j].Key == "auto.paused" {
				m.palSet[j].Value = map[bool]string{true: "true", false: "false"}[v == "off"]
			}
		}
	}
	if r.Key == "ui.theme" {
		return m, m.saveTheme(v)
	}
	return m, m.saveSetting(panelSetting{key: r.Key, opts: []string{v}})
}

// configBox draws /config: a search field, the settings grouped under accent headings with the
// value right-aligned, the focused row on the focus tier with every option as a chip and
// ‹ value ›, and the focused setting's meaning, default and source at the bottom.
func (m chatView) configBox(screen []string, w, h int) []string {
	t := m.th
	bw := max(min(110, w-6), min(w, 40))
	bh := max(min(h-4, 44), 14)
	bg, focus := on(t.Raised), on(t.Focus)
	row := func(s string) string { return fill(bg, bw, s) }
	rows := m.configRows()
	sel := min(m.pal.sel, max(len(rows)-1, 0))
	all := append(m.localRows(), m.palSet...)
	n := 0
	for _, r := range all {
		if edited(r) {
			n++
		}
	}
	title := t.Fg(t.Surface).Bold(true).Render(" Settings") + t.Fg(t.Surface).Render(fmt.Sprintf("   %d settings · %d changed", len(all), n))
	out := []string{fill(on(t.Accent), bw, spread(title, t.Fg(t.Surface).Render("esc  "), bw))}
	q := t.Fg(t.Muted).Render("type to filter")
	if m.pal.query != "" {
		q = t.Fg(t.Strong).Bold(true).Render(m.pal.query)
	}
	out = append(out, fill(focus, bw, spread("  "+t.Fg(t.Accent).Bold(true).Render("/")+" "+q+t.Fg(t.Accent).Render("▏"), t.Fg(t.Dim).Render(fmt.Sprintf("%d shown  ", len(rows))), bw)), row(""))
	var list []string
	selAt, group := 0, ""
	nameW := 24
	for i, r := range rows {
		if g := configGroup(r.Key); g != group {
			if group != "" {
				list = append(list, row(""))
			}
			group = g
			rule := t.Fg(t.Border).Render(" " + strings.Repeat("─", max(bw-8-len(g), 1)))
			list = append(list, row("  "+t.Fg(t.Accent).Bold(true).Render(g)+rule))
		}
		val := cmp.Or(r.Value, r.Default)
		if r.Secret && r.Value != "" {
			val = "****"
		}
		mark := "  "
		if edited(r) {
			mark = t.Fg(t.Accent).Render("• ")
		}
		name := fmt.Sprintf("%-*s", nameW, kit.Short(r.Key, nameW))
		if i != sel {
			vc := t.Muted
			if edited(r) {
				vc = t.Text
			}
			list = append(list, row(spread("  "+mark+t.Fg(t.Text).Render(name), t.Fg(vc).Render(kit.Short(val, 30))+"    ", bw)))
			continue
		}
		selAt = len(list)
		left := t.Fg(t.Accent).Render(railGlyph(t.G.Unicode)) + " " + mark + t.Fg(t.Strong).Bold(true).Render(name)
		right := t.Fg(t.Accent).Bold(true).Render("‹ ") + t.Fg(t.Strong).Bold(true).Render(kit.Short(val, 24)) + t.Fg(t.Accent).Bold(true).Render(" ›") + strings.Repeat(" ", 2+m.vb.shift(r.Key))
		if len(r.Options) == 0 {
			right = t.Fg(t.Strong).Render(kit.Short(val, 30)) + t.Fg(t.Dim).Render("  enter edits") + "  "
		}
		room := bw - ansi.StringWidth(left) - ansi.StringWidth(right) - 2
		chips := m.optionChips(r, val, room)
		list = append(list, fill(focus, bw, spread(left+chips, right, bw)))
	}
	if len(rows) == 0 {
		list = append(list, row("  "+t.Fg(t.Muted).Render("no setting matches "+m.pal.query)))
	}
	body := bh - 8
	start := 0
	if selAt >= body {
		start = selAt - body + 1
	}
	for i := start; i < len(list) && i < start+body; i++ {
		out = append(out, list[i])
	}
	for len(out) < bh-4 {
		out = append(out, row(""))
	}
	desc, meta := "", ""
	if sel < len(rows) {
		r := rows[sel]
		desc = t.Fg(t.Accent).Bold(true).Render(r.Key) + "  " + t.Fg(t.Text).Render(r.Meaning)
		meta = t.Fg(t.Dim).Render("default " + cmp.Or(r.Default, "none") + " " + t.G.Dot + " from " + cmp.Or(r.Src, "default"))
	}
	ov := on(t.Overlay)
	out = append(out, fill(ov, bw, "  "+ansi.Truncate(desc, bw-4, "…")), fill(ov, bw, "  "+meta), row(""))
	out = append(out, row("  "+keyHints(t, "↑↓", "move", "←→", "change", "type", "filter", "enter", "edit", "esc", "close")+"   "+t.Fg(t.Accent).Render("•")+t.Fg(t.Dim).Render(" changed")))
	return m.overBox(screen, out, w)
}

// optionChips is every option of r as a flat chip, the current one in the accent, within room
// cells; ui.theme shows each theme in its own accent.
func (m chatView) optionChips(r api.SettingRow, val string, room int) string {
	t := m.th
	if len(r.Options) == 0 || room < 12 {
		return ""
	}
	var b strings.Builder
	b.WriteString(on(t.Focus).Render("  "))
	used := 2
	for _, o := range r.Options {
		bg := t.Overlay
		fg := t.Muted
		if r.Key == "ui.theme" && !t.Plain {
			bg, fg = t.Renamed(o).Accent, t.Surface
		}
		c := m.chip(" "+o+" ", fg, bg, false)
		if o == val {
			if r.Key != "ui.theme" {
				bg = t.Accent
			}
			c = m.chip(" "+o+" ", t.Surface, bg, true)
			if r.Key == "ui.theme" {
				c = m.chip("▸"+o+" ", t.Surface, bg, true)
			}
		}
		wd := ansi.StringWidth(o) + 3
		if used+wd > room {
			b.WriteString(t.Fg(t.Dim).Render("…"))
			break
		}
		b.WriteString(c + on(t.Focus).Render(" "))
		used += wd
	}
	return b.String()
}
