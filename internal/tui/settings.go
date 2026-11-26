package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/harness"
)

// Setting is one row of the Settings list.
type Setting struct {
	Key, Value, Src, Meaning, Default string
	Secret                            bool
	APIKey                            bool // a key.<service> row: an API key in ~/.smithy/auth.json
}

// keyServices are the API keys smithy's own features read; stored keys of other services show too.
var keyServices = []string{"github", "linear", "telegram", "slack", "slack_app", "discord"}

// keyRows is one key.<service> row per known or stored service, from auth.List lines ("svc ****abcd …").
func keyRows(stored []string) []Setting {
	have := map[string]string{}
	for _, l := range stored {
		f := strings.Fields(l)
		if len(f) >= 2 {
			have[f[0]] = f[1]
		}
	}
	svcs := append([]string{}, keyServices...)
	for s := range have {
		if !slices.Contains(svcs, s) {
			svcs = append(svcs, s)
		}
	}
	sort.Strings(svcs[len(keyServices):])
	var out []Setting
	for _, s := range svcs {
		r := Setting{Key: "key." + s, Src: "unset", APIKey: true, Meaning: s + " API key in ~/.smithy/auth.json (smithy auth set " + s + "); an env var wins"}
		if v, ok := have[s]; ok {
			r.Value, r.Src = v, "auth"
		}
		out = append(out, r)
	}
	return out
}

var groupOrder = []string{"auto", "linear", "warden", "notify", "key", "decide", "agent", "ask", "tasks", "git", "watch", "tend", "route", "stack", "tui", "models"}

// settingRows lists every known key plus the current harness's models, grouped, with value and source from eff.
func settingRows(h string, eff func(string) (string, string, error)) []Setting {
	var out []Setting
	add := func(key, def, meaning string) {
		v, src, err := eff(key)
		if err != nil {
			v, src = "", "unset"
		}
		s := Setting{Key: key, Value: clean(v), Src: src, Meaning: meaning, Default: def, Secret: config.Secret(key)}
		if s.Secret && s.Value != "" {
			s.Value = "****"
		}
		out = append(out, s)
	}
	for _, k := range config.Known {
		add(k.Key, k.Default, k.Meaning)
	}
	for _, t := range []string{"cheap", "mid", "strong", "judge"} {
		add("models."+h+"."+t, "", t+" model in "+h+" (smithy config set-model)")
	}
	return grouped(out)
}

// grouped sorts rows by groupOrder, keeping the order within a group.
func grouped(out []Setting) []Setting {
	rank := map[string]int{}
	for i, g := range groupOrder {
		rank[g] = i + 1
	}
	group := func(k string) int {
		if r := rank[strings.SplitN(k, ".", 2)[0]]; r > 0 {
			return r
		}
		return len(groupOrder) + 1
	}
	sort.SliceStable(out, func(i, j int) bool { return group(out[i].Key) < group(out[j].Key) })
	return out
}

func currentHarness() string {
	h, _ := harness.Detect()
	if h == "" {
		h = "claude"
	}
	return h
}

// edit is the open settings editor: first the typed input, then the confirm step.
type edit struct {
	set     Setting
	kind    string
	opts    []string
	opt     int
	in      textinput.Model
	val     string
	repo    bool
	unset   bool
	confirm bool
	err     string
}

type savedMsg struct {
	what string
	err  error
}

// setterFunc writes or unsets a key in the user's or repo's file; tests and demo mode swap it.
type setterFunc func(repo, unset bool, key, val string) (string, error)

func writeSetting(repo, unset bool, key, val string) (string, error) {
	if svc, ok := strings.CutPrefix(key, "key."); ok {
		if unset {
			found, err := auth.Remove(svc)
			if err == nil && !found {
				return "no " + svc + " key was stored", nil
			}
			return "removed the " + svc + " key", err
		}
		if err := auth.Set(svc, val); err != nil {
			return "", err
		}
		return "saved the " + svc + " key " + auth.Mask(strings.TrimSpace(val)), nil
	}
	scope := scopeName(repo)
	if unset {
		_, found, err := config.Remove(repo, key)
		if err == nil && !found {
			return fmt.Sprintf("%s was not set in the %s file", key, scope), nil
		}
		return fmt.Sprintf("removed %s from the %s file: back to its default", key, scope), err
	}
	_, saved, note, err := config.Apply(repo, key, val)
	msg := fmt.Sprintf("saved %s = %s (%s)", key, saved, scope)
	if note != "" {
		msg = note
	}
	return msg, err
}

func scopeName(repo bool) string {
	if repo {
		return "repo"
	}
	return "user"
}

func shellCmd(e *edit) string {
	if svc, ok := strings.CutPrefix(e.set.Key, "key."); ok {
		if e.unset {
			return "smithy auth remove " + svc
		}
		return "smithy auth set " + svc + "  (key on stdin)"
	}
	scope := ""
	if e.repo {
		scope = "--repo "
	}
	if e.unset {
		return "smithy config unset " + scope + e.set.Key
	}
	return "smithy config set " + scope + e.set.Key + " " + sh(e.val)
}

// selectedSetting is the Settings row under the cursor.
func (m Model) selectedSetting() (Setting, bool) {
	if m.sys == nil || m.sel[vSystem] >= len(m.sys.Settings) {
		return Setting{}, false
	}
	return m.sys.Settings[m.sel[vSystem]], true
}

// openEdit starts editing the selected setting, or its unset when unset is true.
func (m Model) openEdit(unset bool) (tea.Model, tea.Cmd) {
	s, ok := m.selectedSetting()
	if !ok {
		return m, nil
	}
	if s.Secret {
		m.status = "secrets are set with smithy auth, never here"
		return m, nil
	}
	e := &edit{set: s, repo: s.Src == "repo"}
	if unset {
		if s.APIKey && s.Src != "auth" {
			m.status = "no " + strings.TrimPrefix(s.Key, "key.") + " key is stored"
			return m, nil
		}
		if s.APIKey {
			e.unset, e.confirm = true, true
			m.edit = e
			return m, nil
		}
		if s.Src != "user" && s.Src != "repo" {
			m.status = s.Key + " is already at its default"
			return m, nil
		}
		e.unset, e.confirm, e.val = true, true, s.Default
		m.edit = e
		return m, nil
	}
	if s.APIKey {
		e.kind = "api key"
		e.in = textinput.New()
		e.in.Prompt = "> "
		e.in.Placeholder = "paste the key"
		e.in.EchoMode = textinput.EchoPassword
		m.edit = e
		return m, e.in.Focus()
	}
	e.kind, e.opts = config.KindOf(s.Key)
	if e.opts != nil {
		cur := s.Value
		if cur == "" {
			cur = s.Default
		}
		for i, o := range e.opts {
			if o == cur {
				e.opt = i
			}
		}
		m.edit = e
		return m, nil
	}
	e.in = textinput.New()
	e.in.Prompt = "> "
	e.in.Placeholder = map[string]string{"number": "a whole number", "list": "comma list", "text": "value"}[e.kind]
	e.in.SetValue(s.Value)
	e.in.CursorEnd()
	m.edit = e
	return m, e.in.Focus()
}

// editKey handles a key while the settings editor is open.
func (m Model) editKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	e := *m.edit
	m.edit = &e
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if e.confirm && !e.unset {
			e.confirm = false
			if e.opts == nil {
				return m, e.in.Focus()
			}
			return m, nil
		}
		m.edit = nil
		return m, nil
	}
	if e.confirm {
		switch msg.String() {
		case "tab", "shift+tab":
			e.repo = !e.repo && !e.set.APIKey
		case "enter":
			return m.save()
		}
		return m, nil
	}
	if msg.String() == "enter" {
		raw := e.in.Value()
		if e.opts != nil {
			raw = e.opts[e.opt]
		}
		v, err := strings.TrimSpace(raw), error(nil)
		if e.set.APIKey {
			if v == "" {
				err = fmt.Errorf("paste the key, or esc")
			}
		} else {
			v, _, err = config.Normalize(e.set.Key, raw)
		}
		if err != nil {
			e.err = err.Error()
			return m, nil
		}
		e.err, e.val, e.confirm = "", v, true
		e.in.Blur()
		return m, nil
	}
	if e.opts != nil {
		switch msg.String() {
		case "left", "up", "h", "k":
			e.opt = (e.opt + len(e.opts) - 1) % len(e.opts)
		case "right", "down", "l", "j", "space", "tab":
			e.opt = (e.opt + 1) % len(e.opts)
		}
		return m, nil
	}
	var cmd tea.Cmd
	e.in, cmd = e.in.Update(msg)
	e.err = ""
	return m, cmd
}

// save runs the write through config.Apply or config.Remove; demo mode only says what it would run.
func (m Model) save() (tea.Model, tea.Cmd) {
	e := m.edit
	m.edit = nil
	if m.setter == nil {
		m.say("demo: would run " + shellCmd(e))
		return m, nil
	}
	set := m.setter
	return m, func() tea.Msg {
		what, err := set(e.repo, e.unset, e.set.Key, e.val)
		return savedMsg{what, err}
	}
}

// settingLines renders the Settings list around the cursor in h lines.
func (m Model) settingLines(w, h int) []string {
	t := m.th
	mut, val, head := t.fg(t.muted), t.fg(t.text), t.fg(t.accent).Bold(!t.plain)
	if m.sys == nil {
		return nil
	}
	var lines []string
	at := 0
	group := ""
	for i, s := range m.sys.Settings {
		g := strings.SplitN(s.Key, ".", 2)[0]
		if g != group {
			if group != "" {
				lines = append(lines, "")
			}
			group = g
			lines = append(lines, head.Render(g+".*"))
		}
		v := s.Value
		if v == "" {
			v = "-"
		}
		mark, ks := "  ", val
		if i == m.sel[vSystem] {
			at = len(lines)
			mark, ks = t.fg(t.accent).Render(t.g.sel+" "), t.fg(t.accent).Bold(true)
		}
		src := mut
		if s.Src == "user" || s.Src == "repo" {
			src = t.fg(t.ok)
		}
		row := mark + ks.Render(fmt.Sprintf("%-22s", short(s.Key, 22))) + " " + val.Render(fmt.Sprintf("%-14s", short(v, 14))) + " " + src.Render(fmt.Sprintf("%-7s", s.Src))
		if rest := w - lipgloss.Width(row) - 1; rest > 10 {
			row += " " + mut.Render(short(s.Meaning, rest))
		}
		lines = append(lines, row)
	}
	foot := []string{"", mut.Italic(true).Render("enter edits " + t.g.dot + " u unsets " + t.g.dot + " key.* rows are API keys, typed masked")}
	h -= len(foot)
	if len(lines) > h && h > 0 {
		top := min(max(at-h/2, 0), len(lines)-h)
		lines = lines[top : top+h]
	}
	return append(lines, foot...)
}

// editBox renders the editor or its confirm step.
func (m Model) editBox(w int) string {
	t := m.th
	e := m.edit
	bw := min(w-6, 80)
	inner := bw - 4
	mut, val, acc := t.fg(t.muted), t.fg(t.text), t.fg(t.accent)
	kv := func(k, v string) string { return ansi.Truncate(mut.Render(fmt.Sprintf("%-8s", k))+v, inner, "…") }
	old := e.set.Value
	if old == "" {
		old = "-"
	}
	var lines []string
	if e.confirm {
		nv := e.val
		if e.unset {
			nv = "default"
			if e.val != "" && e.val != "ask" {
				nv += " (" + e.val + ")"
			}
		}
		if nv == "" {
			nv = "(empty)"
		}
		if e.set.APIKey && !e.unset {
			nv = auth.Mask(e.val)
		}
		file := "~/.smithy/config.json"
		if e.set.APIKey {
			file = "~/.smithy/auth.json"
		}
		if e.repo {
			file = ".smithy/config.json in this repo"
		}
		scope := acc.Bold(true).Render(scopeName(e.repo)) + mut.Render("  "+file)
		title := "Save " + e.set.Key + "?"
		if e.unset {
			title = "Unset " + e.set.Key + "?"
		}
		lines = append(lines, acc.Bold(true).Render(title), "",
			kv("change", val.Render(old)+mut.Render(" "+arrow(t)+" ")+t.fg(t.ok).Bold(true).Render(nv)),
			kv("scope", scope), kv("runs", mut.Render(shellCmd(e))))
		if !e.repo && e.set.Src == "repo" {
			lines = append(lines, "", t.fg(t.warn).Render(short("this repo sets "+e.set.Key+": the repo value wins here", inner)))
		}
		lines = append(lines, "",
			t.fg(t.border).Render(strings.Repeat("─", inner)))
		hint := acc.Render("enter") + " " + mut.Render("save") + "   " + acc.Render("tab") + " " + mut.Render("user/repo") + "   " + acc.Render("esc") + " " + mut.Render("back")
		if e.unset {
			hint = acc.Render("enter") + " " + mut.Render("unset") + "   " + acc.Render("tab") + " " + mut.Render("user/repo") + "   " + acc.Render("esc") + " " + mut.Render("cancel")
		}
		lines = append(lines, hint)
	} else {
		lines = append(lines, acc.Bold(true).Render("Edit "+e.set.Key)+mut.Render("  "+e.kind), "")
		for _, l := range wrap(e.set.Meaning, inner) {
			lines = append(lines, t.fg(t.text).Italic(!t.plain).Render(l))
		}
		lines = append(lines, "", kv("now", val.Render(old)+mut.Render("  ("+e.set.Src+")")))
		if e.opts != nil {
			var chips []string
			for i, o := range e.opts {
				if i == e.opt {
					chips = append(chips, t.fg(t.surface).Background(t.accent).Bold(true).Render(" "+o+" "))
				} else {
					chips = append(chips, mut.Render(" "+o+" "))
				}
			}
			lines = append(lines, kv("new", strings.Join(chips, " ")))
		} else {
			lines = append(lines, kv("new", e.in.View()))
		}
		if e.err != "" {
			lines = append(lines, "", t.fg(t.err).Render(short(e.err, inner)))
		}
		hint := acc.Render("enter") + " " + mut.Render("next") + "   " + acc.Render("esc") + " " + mut.Render("cancel")
		if e.opts != nil {
			hint = acc.Render("←→") + " " + mut.Render("choose") + "   " + hint
		}
		lines = append(lines, "", t.fg(t.border).Render(strings.Repeat("─", inner)), hint)
	}
	st := m.panelStyle(t.accent).Width(bw)
	if !t.plain {
		st = st.Background(t.overlay)
	}
	return st.Render(strings.Join(lines, "\n"))
}

// wrap breaks s into lines of at most w cells on spaces.
func wrap(s string, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && lipgloss.Width(line)+1+lipgloss.Width(word) > w {
			out = append(out, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

func arrow(t theme) string {
	if t.g.unicode {
		return "→"
	}
	return "->"
}
