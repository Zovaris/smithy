//go:build harness

package chat

import (
	"cmp"
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
	if k == advancedKey || !slices.ContainsFunc(configMain, func(c struct{ key, label string }) bool { return c.key == k }) {
		return "Advanced"
	}
	head, _, _ := strings.Cut(k, ".")
	for _, g := range configGroups {
		if slices.Contains(g.prefixes, head) {
			return g.name
		}
	}
	return "Other"
}

// configMain is the curated /config list: the settings people change, each with its label, in
// the order shown. Every other setting sits under Advanced, which filtering still searches.
var configMain = []struct{ key, label string }{
	{"ui.theme", "Theme"}, {"chat.transcript", "Transcript"}, {"chat.sidebar", "Sidebar"}, {"ui.agent_tabs", "Show agent tabs"}, {"ui.reduce_motion", "Reduce motion"}, {"ui.splash", "Splash"},
	{"agent.effort", "Effort"}, {"agent.code_mode", "Code mode"}, {"agent.timeout", "Turn timeout"}, {"kit.mcp", "smithy MCP server"},
	{"tend.daemon", "Tend"}, {"tend.watch", "Tend watches"}, {"tend.label", "Tend label"},
	{"serve", "Serve"}, {"auto.agent", "Serve agent"}, {"auto.max_per_day", "Runs a day"}, {"auto.max_cost_per_day", "Spend a day"}, {"daemon.idle_exit", "Daemon idles out after"},
	{"notify.kinds", "Notify about"}, {"notify.style", "Message style"}, {"notify.telegram.chat", "Telegram chat"},
}

// advancedKey is the collapsed Advanced row.
const advancedKey = "\x00advanced"

// configLabel is how /config names key k: its curated label, else its last part in words.
func configLabel(k string) string {
	for _, c := range configMain {
		if c.key == k {
			return c.label
		}
	}
	if k == advancedKey {
		return "Advanced"
	}
	if l, ok := advLabels[k]; ok {
		return l
	}
	parts := strings.Split(k, ".")
	l := strings.ReplaceAll(parts[len(parts)-1], "_", " ")
	if len(parts) > 2 {
		l = parts[len(parts)-2] + " " + l
	}
	return strings.ToUpper(l[:1]) + l[1:]
}

// shownValue is a setting's value as /config reads it: booleans as on and off.
func shownValue(v string) string {
	switch v {
	case "true":
		return "on"
	case "false":
		return "off"
	}
	return v
}

// configRows is what /config lists: the curated settings by group, then Advanced, collapsed until
// opened; a query searches every setting by label, key or meaning.
func (m chatView) configRows() []api.SettingRow {
	q := strings.ToLower(m.pal.query)
	all := append(m.localRows(), m.palSet...)
	byKey := map[string]api.SettingRow{}
	for _, r := range all {
		byKey[r.Key] = r
	}
	var main, adv []api.SettingRow
	for _, c := range configMain {
		if r, ok := byKey[c.key]; ok {
			main = append(main, r)
			delete(byKey, c.key)
		}
	}
	for _, r := range all {
		if _, ok := byKey[r.Key]; ok {
			adv = append(adv, r)
		}
	}
	advRank := func(k string) int {
		return slices.IndexFunc(advGroups, func(g struct {
			name     string
			prefixes []string
		}) bool {
			return g.name == advGroup(k)
		})
	}
	slices.SortStableFunc(adv, func(a, b api.SettingRow) int { return cmp.Compare(advRank(a.Key), advRank(b.Key)) })
	if q != "" {
		var rows []api.SettingRow
		for _, r := range append(main, adv...) {
			if strings.Contains(strings.ToLower(configLabel(r.Key)+" "+r.Key+" "+r.Meaning), q) {
				rows = append(rows, r)
			}
		}
		return rows
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
	slices.SortStableFunc(main, func(a, b api.SettingRow) int { return cmp.Compare(rank(a.Key), rank(b.Key)) })
	main = append(main, api.SettingRow{Key: advancedKey, Value: fmt.Sprint(len(adv)), Meaning: "every other setting, by its config key; typing searches them too"})
	if m.cfgAdv {
		main = append(main, adv...)
	}
	return main
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
		ctx, cancel := daemonCtx()
		defer cancel()
		s, err := a.Settings(ctx)
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
	if m.cfgEd != nil {
		for _, r := range rows {
			if r.Key == m.cfgEd.key {
				return m.editKey(msg, r)
			}
		}
		m.cfgEd = nil
	}
	if k == "f" && m.pal.query == "" {
		m.cfgFull = !m.cfgFull
		return m, nil
	}
	switch k {
	case "esc":
		if m.pal.query != "" {
			m.pal = palette{}
			return m, nil
		}
		m.overlay = overlayNone
		return m, nil
	case "up":
		m.pal.sel = wrapStep(m.pal.sel, -1, len(rows))
		return m, nil
	case "down", "tab":
		m.pal.sel = wrapStep(m.pal.sel, 1, len(rows))
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
		if rows[m.pal.sel].Key == advancedKey {
			m.cfgAdv = k == "right"
			return m, nil
		}
		return m.cycleSetting(rows[m.pal.sel], dir)
	case "enter":
		if m.pal.sel < len(rows) {
			r := rows[m.pal.sel]
			if r.Key == advancedKey {
				m.cfgAdv = !m.cfgAdv
				return m, nil
			}
			m.cfgEd = editorFor(r)
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
	e := editorFor(r)
	if e.kind != "pick" {
		m.cfgEd = e
		return m, nil
	}
	opts := e.opts
	val := cmp.Or(r.Value, r.Default)
	i := slices.Index(opts, val)
	v := opts[((i+dir)%len(opts)+len(opts))%len(opts)]
	if i < 0 && dir < 0 {
		v = opts[len(opts)-1]
	}
	return m.applySetting(r, v)
}

// applySetting saves r = v; the chat's own rows (transcript, sidebar, tabs, theme) change at once.
func (m chatView) applySetting(r api.SettingRow, v string) (tea.Model, tea.Cmd) {
	if v == cmp.Or(r.Value, r.Default) && r.Value != "" {
		return m, nil
	}
	for j := range m.palSet {
		if m.palSet[j].Key == r.Key {
			m.palSet[j].Value = v
		}
	}
	m.vb = valueBump{key: r.Key, still: m.still}.start()
	switch r.Key {
	case "ui.agent_tabs":
		m.tabsOn = v == "true"
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
// ‹ value ›, an inline editor under it once enter opens one, and the focused setting's meaning,
// default and source: at the bottom, or in a pane on the right in fullscreen (f).
func (m chatView) configBox(screen []string, w, h int) []string {
	t := m.th
	bw := max(min(110, w-6), min(w, 40))
	bh := max(min(h-4, 44), 14)
	if m.cfgFull {
		bw, bh = w, h-2
	}
	lw := bw
	if m.cfgFull && bw >= 100 {
		lw = bw * 3 / 5
	}
	bg, focus := on(t.Raised), on(t.Focus)
	row := func(s string) string { return fill(bg, lw, s) }
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
	head := []string{fill(on(t.Accent), bw, spread(title, t.Fg(t.Surface).Render("esc  "), bw))}
	q := t.Fg(t.Muted).Render("type to filter")
	if m.pal.query != "" {
		q = t.Fg(t.Strong).Bold(true).Render(m.pal.query)
	}
	head = append(head, fill(focus, bw, spread("  "+t.Fg(t.Accent).Bold(true).Render("/")+" "+q+t.Fg(t.Accent).Render("▏"), t.Fg(t.Dim).Render(fmt.Sprintf("%d shown  ", len(rows))), bw)), fill(bg, bw, ""))
	var list []string
	selAt, group := 0, ""
	nameW := 24
	if m.cfgFull {
		nameW = 34
	}
	heading := func(g string) {
		if group != "" {
			list = append(list, row(""))
		}
		group = g
		list = append(list, row("  "+t.Fg(t.Accent).Bold(true).Render(g)))
	}
	for i, r := range rows {
		if r.Key == advancedKey {
			if group != "" {
				list = append(list, row(""))
			}
			group = "Advanced"
			mark, st := "  ", bg
			if i == sel {
				mark, st, selAt = t.Fg(t.Accent).Render(railGlyph(t.G.Unicode))+" ", focus, len(list)
			}
			open := "›"
			if m.cfgAdv {
				open = "‹"
			}
			list = append(list, fill(st, lw, spread(mark+t.Fg(t.Accent).Bold(true).Render("Advanced")+t.Fg(t.Dim).Render(" ("+r.Value+")"), t.Fg(t.Muted).Render(open)+"    ", lw)))
			continue
		}
		g := configGroup(r.Key)
		if g == "Advanced" {
			g = "Advanced · " + advGroup(r.Key)
		}
		if g != group && (m.pal.query == "" || !strings.HasPrefix(g, "Advanced")) {
			heading(g)
		}
		val := shownValue(cmp.Or(r.Value, r.Default, "not set"))
		if r.Secret && r.Value != "" {
			val = "****"
		}
		mark := "  "
		if edited(r) {
			mark = t.Fg(t.Accent).Render("• ")
		}
		name := fmt.Sprintf("%-*s", nameW, kit.Short(configLabel(r.Key), nameW))
		if i != sel {
			vc := t.Muted
			if edited(r) {
				vc = t.Text
			}
			list = append(list, row(spread("  "+mark+t.Fg(t.Text).Render(name), t.Fg(vc).Render(kit.Short(val, 30))+"    ", lw)))
			continue
		}
		selAt = len(list)
		e := editorFor(r)
		left := t.Fg(t.Accent).Render(railGlyph(t.G.Unicode)) + " " + mark + t.Fg(t.Strong).Bold(true).Render(name)
		right := t.Fg(t.Accent).Bold(true).Render("‹ ") + t.Fg(t.Strong).Bold(true).Render(kit.Short(val, 24)) + t.Fg(t.Accent).Bold(true).Render(" ›") + strings.Repeat(" ", 2+m.vb.shift(r.Key))
		if e.kind != "pick" {
			right = t.Fg(t.Strong).Render(kit.Short(val, 30)) + t.Fg(t.Dim).Render("  enter edits") + "  "
		}
		room := lw - ansi.StringWidth(left) - ansi.StringWidth(right) - 2
		chips := ""
		if m.cfgEd == nil {
			chips = m.optionChips(r, val, room)
		}
		list = append(list, fill(focus, lw, spread(left+chips, right, lw)))
		if m.cfgEd != nil && m.cfgEd.key == r.Key {
			ed := m.editorRows(lw)
			list = append(list, ed...)
			selAt += len(ed)
		}
	}
	if len(rows) == 0 {
		list = append(list, row("  "+t.Fg(t.Muted).Render("no setting matches "+m.pal.query)))
	}
	body := bh - 8
	if m.cfgFull {
		body = bh - 5
	}
	start := 0
	if selAt >= body {
		start = selAt - body + 1
	}
	var mid []string
	for i := start; i < len(list) && i < start+body; i++ {
		mid = append(mid, list[i])
	}
	for len(mid) < body {
		mid = append(mid, row(""))
	}
	var cur *api.SettingRow
	if sel < len(rows) {
		cur = &rows[sel]
	}
	hints := "  " + keyHints(t, "↑↓", "move", "←→", "change", "enter", "pick or edit", "f", "fullscreen", "type", "filter", "esc", "close") + "   " + t.Fg(t.Accent).Render("•") + t.Fg(t.Dim).Render(" changed")
	if m.cfgEd != nil {
		hints = "  " + keyHints(t, "↑↓", "move", "enter", "save", "esc", "cancel")
		if m.cfgEd.kind == "multi" {
			hints = "  " + keyHints(t, "↑↓", "move", "space", "check", "enter", "save", "esc", "cancel")
		}
		if m.cfgEd.kind == "text" {
			hints = "  " + keyHints(t, "type", "edit", "ctrl+u", "clear", "enter", "save", "esc", "cancel")
		}
	}
	out := head
	if lw < bw {
		pane := m.configDetail(cur, bw-lw, len(mid))
		for i := range mid {
			out = append(out, mid[i]+pane[i])
		}
		out = append(out, fill(bg, bw, ""), fill(bg, bw, hints))
		return m.overBox(screen, out, w)
	}
	out = append(out, mid...)
	desc, meta := "", ""
	if cur != nil {
		r := *cur
		desc = t.Fg(t.Accent).Bold(true).Render(configLabel(r.Key)) + "  " + t.Fg(t.Text).Render(r.Meaning)
		if r.Key != advancedKey {
			meta = t.Fg(t.Muted).Render(r.Key) + t.Fg(t.Dim).Render("  "+t.G.Dot+" default "+shownValue(cmp.Or(r.Default, "none"))+" "+t.G.Dot+" from "+cmp.Or(r.Src, "default"))
		}
	}
	ov := on(t.Overlay)
	out = append(out, fill(ov, bw, "  "+ansi.Truncate(desc, bw-4, "…")), fill(ov, bw, "  "+meta), row(""))
	out = append(out, row(hints))
	return m.overBox(screen, out, w)
}

// editorRows draws the open inline editor w wide: the choices with the focused one on the rail,
// checkboxes for a comma list, or a text field.
func (m chatView) editorRows(w int) []string {
	t := m.th
	e := m.cfgEd
	focus, ov := on(t.Focus), on(t.Overlay)
	ind := "      "
	if e.kind == "text" {
		field := t.Fg(t.Strong).Render(e.text) + t.Fg(t.Accent).Render("▏")
		return []string{fill(ov, w, ind+field)}
	}
	var out []string
	for i, o := range e.opts {
		label := shownValue(o)
		if o == "" {
			label = "detect (none set)"
		}
		box := ""
		if e.kind == "multi" {
			box = t.Fg(t.Dim).Render(t.G.Off) + " "
			if e.on[o] {
				box = t.Fg(t.Accent).Render(t.G.On) + " "
			}
		}
		if i == e.sel {
			out = append(out, fill(focus, w, ind[:4]+t.Fg(t.Accent).Render(railGlyph(t.G.Unicode))+" "+box+t.Fg(t.Strong).Bold(true).Render(label)))
			continue
		}
		out = append(out, fill(ov, w, ind+box+t.Fg(t.Text).Render(label)))
	}
	return out
}

// configDetail is the fullscreen detail pane for r, h rows of w: its label, what it does wrapped,
// then its key, default, source and allowed values.
func (m chatView) configDetail(r *api.SettingRow, w, h int) []string {
	t := m.th
	ov := on(t.Overlay)
	iw := max(w-4, 10)
	var lines []string
	if r != nil {
		lines = append(lines, t.Fg(t.Accent).Bold(true).Render(configLabel(r.Key)), "")
		for _, l := range strings.Split(ansi.Wrap(r.Meaning, iw, ""), "\n") {
			lines = append(lines, t.Fg(t.Text).Render(l))
		}
		if r.Key != advancedKey {
			lines = append(lines, "", t.Fg(t.Muted).Render("Key      ")+t.Fg(t.Strong).Render(r.Key),
				t.Fg(t.Muted).Render("Default  ")+t.Fg(t.Strong).Render(shownValue(cmp.Or(r.Default, "none"))),
				t.Fg(t.Muted).Render("From     ")+t.Fg(t.Strong).Render(cmp.Or(r.Src, "default")))
			e := editorFor(*r)
			if e.kind != "text" {
				vals := make([]string, 0, len(e.opts))
				for _, o := range e.opts {
					vals = append(vals, shownValue(cmp.Or(o, "detect")))
				}
				lines = append(lines, "", t.Fg(t.Muted).Render("Values"))
				for _, l := range strings.Split(ansi.Wrap(strings.Join(vals, ", "), iw, ""), "\n") {
					lines = append(lines, t.Fg(t.Text).Render(l))
				}
			}
		}
	}
	out := make([]string, h)
	for i := range out {
		l := ""
		if i > 0 && i-1 < len(lines) {
			l = lines[i-1]
		}
		out[i] = fill(ov, w, "  "+ansi.Truncate(l, iw, "…"))
	}
	return out
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
		c := m.chip(" "+shownValue(o)+" ", fg, bg, false)
		if shownValue(o) == val {
			if r.Key != "ui.theme" {
				bg = t.Accent
			}
			c = m.chip(" "+shownValue(o)+" ", t.Surface, bg, true)
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
