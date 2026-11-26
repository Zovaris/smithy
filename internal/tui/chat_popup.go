//go:build harness

package tui

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// slashCommand is one / command: what the popup, the help and the palette show.
type slashCommand struct{ name, args, desc string }

var slashCommands = []slashCommand{
	{"new", "", "start a new session; your next message opens it"},
	{"sessions", "", "agents and sessions: open one, stop it, see what waits on you"},
	{"agent", "[name]", "the agent new sessions start with"},
	{"model", "[name]", "the model the open session runs, else new sessions; tab lists other agents"},
	{"effort", "[low|medium]", "the open session's reasoning effort, for agents that take one"},
	{"skills", "", "skills and MCP injected into every session"},
	{"connect", "", "which daemon this chat talks to, and how to reach another"},
	{"web", "", "open the web UI in your browser"},
	{"remote", "", "reach this daemon from your phone or another machine"},
	{"stop", "", "stop the open session's turn"},
	{"mode", "[minimal|expandable|verbose]", "how much of each turn the transcript shows"},
	{"log", "", "daemon events kept out of the transcript"},
	{"setup", "", "first run: every agent ready or missing, with its fix"},
	{"ask", "[question]", "pin a sample question on the strip, to see and try how decisions work"},
	{"answer", "<text>", "answer the decision pinned at the top in your words"},
	{"help", "", "keys and commands"},
	{"serve", "[on|off]", "serve mode: off launches nothing new (auto.paused)"},
	{"set", "<key> <value>", "save a setting in your user file (smithy config set)"},
	{"unset", "<key>", "put a setting back to its default"},
	{"keys", "", "stored API keys, masked; set them in smithy tui Settings"},
	{"key", "rm <service>", "remove a stored API key"},
	{"doctor", "", "run smithy doctor on the daemon's machine"},
	{"detach", "", "quit and leave the daemon running"},
	{"quit", "", "leave; a background daemon keeps running"},
}

// popupItem is one row of the / or @ popup: what tab puts in the input, and its description.
type popupItem struct{ fill, label, desc string }

const popupMax = 8

// popup is the open / or @ popup's rows for the input as typed; nil when none is open.
func (m chatView) popup() []popupItem {
	v := m.input.Value()
	if m.overlay != overlayNone || strings.Contains(v, "\n") {
		return nil
	}
	if strings.HasPrefix(v, "/") && !strings.Contains(v, " ") {
		q := strings.ToLower(v[1:])
		var out []popupItem
		for _, c := range slashCommands {
			if strings.HasPrefix(c.name, q) {
				out = append(out, popupItem{fill: "/" + c.name + " ", label: "/" + c.name, desc: c.desc})
			}
		}
		if len(out) == 1 && out[0].label == v {
			return nil
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
		if !strings.HasPrefix(it.label, "/") {
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

// popupRows draws the popup as a raised list w wide, the selected row on the border tier.
func (m chatView) popupRows(items []popupItem, w int) []string {
	t := m.th
	bg, sel := lipgloss.NewStyle(), lipgloss.NewStyle()
	if t.overlay != nil {
		bg, sel = bg.Background(t.overlay), sel.Background(t.border)
	}
	lw := 0
	for _, it := range items {
		lw = max(lw, ansi.StringWidth(it.label))
	}
	room := max(min(len(items), m.rows()), 1)
	start := max(0, min(m.popSel-room+1, len(items)-room))
	var out []string
	for i := start; i < len(items) && i < start+room; i++ {
		it := items[i]
		label, st := t.fg(t.text), bg
		if i == m.popSel {
			label, st = t.fg(t.strong).Bold(true), sel
		}
		r := "  " + label.Render(it.label) + strings.Repeat(" ", lw-ansi.StringWidth(it.label))
		if it.desc != "" {
			r += "   " + t.fg(t.dim).Render(it.desc)
		}
		out = append(out, fill(st, w, ansi.Truncate(r, w-1, "…")))
	}
	return out
}
