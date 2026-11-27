//go:build harness

package tui

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/Zovaris/smithy/internal/agents"
	"github.com/charmbracelet/x/ansi"
)

// slashCommand is one / command: what the popup, the help and the palette show.
type slashCommand struct{ name, args, desc string }

var slashCommands = []slashCommand{
	{"new", "", "start a new session; your next message opens it"},
	{"sessions", "", "agents and sessions: open one, stop it, see what waits on you"},
	{"agent", "[name]", "the agent new sessions start with"},
	{"model", "[name]", "the model the open session runs, else new sessions; tab lists other agents"},
	{"effort", "[off|low|medium]", "reasoning effort: the open session's, else the next session's (agent.effort)"},
	{"skills", "", "skills and MCP injected into every session"},
	{"theme", "[steel|frost|plum|moss|dune|dusk|rose]", "the color theme this TUI and the web share (ui.theme)"},
	{"connect", "", "which daemon this chat talks to, and how to reach another"},
	{"web", "", "open the web UI in your browser"},
	{"remote", "", "reach this daemon from your phone or another machine"},
	{"stop", "", "stop the open session's turn"},
	{"goal", "[text|clear]", "pin a goal on the open session: the daemon continues it until met (session.goal_max_*)"},
	{"mode", "[minimal|expandable|verbose]", "how much of each turn the transcript shows"},
	{"log", "", "daemon events kept out of the transcript"},
	{"welcome", "", "walk the first-run steps again: agents, model, API keys, notifications"},
	{"setup", "", "first run: every agent ready or missing, with its fix"},
	{"ask", "[question]", "pin a sample question on the strip, to see and try how decisions work"},
	{"answer", "<text>", "answer the decision pinned at the top in your words"},
	{"help", "", "keys and commands"},
	{"serve", "[on|off]", "serve mode: off launches nothing new (auto.paused)"},
	{"set", "<key> <value>", "save a setting in your user file (smithy config set)"},
	{"unset", "<key>", "put a setting back to its default"},
	{"keys", "", "stored API keys, masked, and how to add or remove one"},
	{"key", "rm <service>", "remove a stored API key"},
	{"prompt", "", "the system prompt file (~/.smithy/prompt.md) and whether it replaces the default"},
	{"doctor", "", "run smithy doctor on the daemon's machine"},
	{"board", "[task dir]", "the task files by status: move, filter by task dir, open a file or its PR"},
	{"tend", "", "the batch PRs the daemon keeps green, and what it is doing on each"},
	{"detach", "", "quit and leave the daemon running"},
	{"quit", "", "leave; a background daemon keeps running"},
}

func isCommand(name string) bool {
	for _, c := range slashCommands {
		if c.name == name {
			return true
		}
	}
	return false
}

// popupItem is one row of the / or @ popup: what tab puts in the input, and its description.
// skill marks a skill row, drawn apart from the commands.
type popupItem struct {
	fill, label, desc string
	skill             bool
}

// slashPopup is the / menu for q: commands, then skills, each whose name holds q (or, from three
// letters on, whose description does), name-prefix matches first.
func (m chatView) slashPopup(q string) []popupItem {
	var first, rest []popupItem
	add := func(it popupItem, name string) {
		switch {
		case strings.HasPrefix(name, q):
			first = append(first, it)
		case strings.Contains(name, q) || len(q) >= 3 && strings.Contains(strings.ToLower(it.desc), q):
			rest = append(rest, it)
		}
	}
	for _, c := range slashCommands {
		add(popupItem{fill: "/" + c.name + " ", label: "/" + c.name, desc: c.desc}, c.name)
	}
	cmds := slices.Concat(first, rest)
	first, rest = nil, nil
	if m.setupSt != nil {
		for _, s := range m.setupSt.Skills {
			if p := m.setupSt.Paths[s]; p != "" && !isCommand(s) {
				add(popupItem{fill: "/" + s + " ", label: "/" + s, desc: skillDir(p), skill: true}, s)
			}
		}
	}
	return append(cmds, append(first, rest...)...)
}

// effortPopup completes /effort with the levels the current agent takes, default first.
func (m chatView) effortPopup(q string) []popupItem {
	if strings.Contains(q, " ") {
		return nil
	}
	agent := ""
	if s := m.openSession(); s != nil {
		agent = s.agent
	} else if m.setupSt != nil {
		agent = m.setupSt.Default.Agent
	}
	if agent != "" && !agents.EffortFlags[agent] {
		return nil
	}
	var out []popupItem
	for _, e := range append([]string{"default"}, agents.Effort().Levels...) {
		if !strings.HasPrefix(e, strings.ToLower(q)) {
			continue
		}
		desc := ""
		switch e {
		case "default":
			desc = "off: back to the agent.effort setting"
		case agents.MaxEffort:
			desc = "the top level smithy allows"
		}
		out = append(out, popupItem{fill: "/effort " + e, label: "/effort " + e, desc: desc})
	}
	if len(out) == 1 && out[0].fill == "/effort "+q {
		return nil
	}
	return out
}

const popupMax = 8

// popup is the open / or @ popup's rows for the input as typed; nil when none is open.
func (m chatView) popup() []popupItem {
	v := m.input.Value()
	if m.overlay != overlayNone || strings.Contains(v, "\n") {
		return nil
	}
	if strings.HasPrefix(v, "/effort ") {
		return m.effortPopup(strings.TrimPrefix(v, "/effort "))
	}
	if strings.HasPrefix(v, "/") && !strings.Contains(v, " ") {
		out := m.slashPopup(strings.ToLower(v[1:]))
		if len(out) == 1 && out[0].label == v {
			return nil
		}
		for i, it := range out {
			if it.label == v {
				copy(out[1:i+1], out[:i])
				out[0] = it
			}
		}
		return out
	}
	at := strings.LastIndexByte(v, '@')
	if at < 0 || (at > 0 && v[at-1] != ' ') || strings.ContainsAny(v[at:], " \t") {
		return nil
	}
	q := strings.ToLower(v[at+1:])
	var hits []string
	for _, f := range m.fileList() {
		if strings.Contains(strings.ToLower(f), q) {
			hits = append(hits, f)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		bi, bj := strings.HasPrefix(strings.ToLower(filepath.Base(hits[i])), q), strings.HasPrefix(strings.ToLower(filepath.Base(hits[j])), q)
		if bi != bj {
			return bi
		}
		return len(hits[i]) < len(hits[j])
	})
	out := make([]popupItem, 0, popupMax)
	for _, f := range hits {
		if len(out) == popupMax {
			break
		}
		out = append(out, popupItem{fill: v[:at] + "@" + f + " ", label: f})
	}
	return out
}

// fileList is the folder's files for the @ popup, walked once: no dot folders, no
// node_modules or vendor, at most 5000.
func (m chatView) fileList() []string {
	if m.files != nil && m.files.dir == m.dir {
		return m.files.list
	}
	dir := m.dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	var list []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != dir && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if len(list) >= 5000 {
			return filepath.SkipAll
		}
		if rel, err := filepath.Rel(dir, p); err == nil {
			list = append(list, filepath.ToSlash(rel))
		}
		return nil
	})
	if m.files != nil {
		m.files.dir, m.files.list = m.dir, list
	}
	return list
}

type fileCache struct {
	dir  string
	list []string
}

// popupKey handles the keys an open popup takes: up/down move, tab completes, enter on a
// command completes and runs it.
func (m chatView) popupKey(msg tea.KeyPressMsg, items []popupItem) (chatView, tea.Cmd, bool) {
	switch msg.String() {
	case "up":
		m.popSel = max(m.popSel-1, 0)
	case "down":
		m.popSel = min(m.popSel+1, len(items)-1)
	case "tab":
		m.input.SetValue(items[min(m.popSel, len(items)-1)].fill)
		m.input.CursorEnd()
		m.popSel = 0
	case "enter":
		it := items[min(m.popSel, len(items)-1)]
		m.popSel = 0
		if !strings.HasPrefix(it.label, "/") || needsArgs(it.label[1:]) {
			m.input.SetValue(it.fill)
			m.input.CursorEnd()
			return m, nil, true
		}
		m.input.SetValue("")
		n, cmd := m.command(it.label)
		return n.(chatView), cmd, true
	case "esc":
		m.input.SetValue("")
		m.popSel = 0
	default:
		return m, nil, false
	}
	return m, nil, true
}

// skillGlyph marks a skill row in the / menu.
func skillGlyph(t theme) string {
	if t.uni {
		return "◆"
	}
	return "*"
}

// popupRows draws the popup as a raised list w wide, the selected row marked by the accent bar
// floatBox uses.
func (m chatView) popupRows(items []popupItem, w int) []string {
	t := m.th
	bg := lipgloss.NewStyle()
	if t.overlay != nil {
		bg = bg.Background(t.overlay)
	}
	bar := frameGlyphs(t.uni).sel
	lw := 0
	for _, it := range items {
		lw = max(lw, ansi.StringWidth(it.label))
	}
	room := max(min(len(items), m.rows()), 1)
	start := max(0, min(m.popSel-room+1, len(items)-room))
	var out []string
	for i := start; i < len(items) && i < start+room; i++ {
		it := items[i]
		label, mark := t.fg(t.text), "  "
		if i == m.popSel {
			label, mark = t.fg(t.strong).Bold(true), t.fg(t.accent).Render(bar)+" "
		}
		glyph := "  "
		if it.skill {
			glyph = t.fg(t.info).Render(skillGlyph(t)) + " "
			if i != m.popSel {
				label = t.fg(t.info)
			}
		}
		r := mark + glyph + label.Render(it.label) + strings.Repeat(" ", lw-ansi.StringWidth(it.label))
		if it.desc != "" {
			desc := t.fg(t.dim)
			if it.skill {
				desc = t.fg(t.muted).Italic(true)
			}
			r += "   " + desc.Render(it.desc)
		}
		out = append(out, fill(bg, w, ansi.Truncate(r, w-1, "…")))
	}
	return out
}

// needsArgs reports whether a command takes required arguments, so enter fills it in instead of running it bare.
func needsArgs(name string) bool {
	for _, c := range slashCommands {
		if c.name == name {
			return strings.HasPrefix(c.args, "<") || strings.HasPrefix(c.args, "rm ")
		}
	}
	return false
}
