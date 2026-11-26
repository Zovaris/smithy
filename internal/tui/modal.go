//go:build harness

package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Every list that opens over the chat is one modal: floatBox centered by overBox, the query row
// on top, ↑↓ enter esc in the key row. Pickers only build their boxItems.

// noteItems turns the open overlay's note into rows that are read, never selected.
func (m chatView) noteItems() []boxItem {
	if m.setupNote == "" {
		return nil
	}
	items := []boxItem{{group: "NOTE"}}
	for _, l := range strings.Split(m.setupNote, "\n") {
		items = append(items, boxItem{glyph: m.th.g.dot, color: m.th.info, label: l, static: true})
	}
	return items
}

// pickerBox is the first-run screen and the ctrl+l / /agent / /model picker.
func (m chatView) pickerBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	if m.setupSt != nil && m.setupStep == stepModel {
		return m.modelBox(screen, w, h)
	}
	title := "Pick an agent"
	esc := "close"
	var items []boxItem
	if m.overlay == overlaySetup {
		title, esc = "Welcome to smithy", "later"
		items = append(items, boxItem{group: "Pick the agent new sessions start with; ctrl+l changes it later."})
	}
	if m.setupSt == nil {
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "reading the agents…", static: true})
	} else {
		kind := ""
		for _, a := range m.setupSt.Agents {
			if a.Kind != kind {
				kind = a.Kind
				label := "AGENT CLIS"
				if kind == "api" {
					label = "API KEYS"
				}
				items = append(items, boxItem{group: label})
			}
			it := boxItem{glyph: t.g.pass, color: t.ok, label: a.Name, hint: short(a.Detail, 40), key: "ready"}
			if !a.Ready {
				it.glyph, it.color, it.key = t.g.fail, t.err, "missing"
				if a.Fix != "" {
					it.hint = a.Fix
				}
			}
			if d := m.setupSt.Default.Agent; m.setupSt.Chosen && (d == a.Name || strings.HasPrefix(d, a.Name+"/")) {
				it.key = t.g.on + " default"
			}
			items = append(items, it)
		}
	}
	items = append(items, m.noteItems()...)
	foot := "  " + keyHints(t, "↑↓", "move", "enter", "pick", "m", "model", "esc", esc)
	return m.overBox(screen, m.floatBox(bw, bh, "", title, items, m.setupSel, foot), w)
}

// modelBox is the model list for m.modelFor: the filter is the query row.
func (m chatView) modelBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	items := []boxItem{{group: "Model for " + m.modelFor}}
	list := m.modelRows()
	for _, r := range list {
		it := boxItem{glyph: t.g.dot, color: t.muted, label: r.m.ID}
		switch {
		case r.other:
			it.label, it.hint = "Other: "+r.m.ID, "use this name as typed"
		case r.m.ID == "":
			it.label, it.hint = "agent default", "whatever "+m.modelFor+" starts with"
		default:
			var parts []string
			if r.m.Name != "" && r.m.Name != r.m.ID {
				parts = append(parts, r.m.Name)
			}
			if r.m.InPerM != nil && r.m.OutPerM != nil {
				parts = append(parts, fmt.Sprintf("$%.2f/$%.2f per M", *r.m.InPerM, *r.m.OutPerM))
			}
			it.hint = strings.Join(parts, "  ")
			if r.m.Default {
				it.key, it.color = t.g.on+" default", t.accent
			}
		}
		items = append(items, it)
	}
	count := ""
	if m.modelList == nil {
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "asking " + m.modelFor + " for its models…", static: true})
	} else {
		count = fmt.Sprintf("%d models from %s", len(m.modelList.Models), m.modelList.Source)
		if e := m.modelList.Error; e != "" {
			items = append(items, boxItem{glyph: t.g.wait, color: t.warn, label: e, static: true})
		}
	}
	items = append(items, m.noteItems()...)
	if count != "" {
		items[0].group += " · " + count
	}
	foot := "  " + keyHints(t, "↑↓", "move", "enter", "save", "ctrl+r", "ask again", "esc", "cancel")
	return m.overBox(screen, m.floatBox(bw, bh, m.modelIn, "Model for "+m.modelFor+" · type to filter", items, m.modelSel, foot), w)
}

// skillsBox is /skills: what goes into every session, then what each harness has installed.
func (m chatView) skillsBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	var items []boxItem
	if s := m.setupSt; s == nil {
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "reading…", static: true})
	} else {
		check := func(on bool) (string, color.Color) {
			if on {
				return "[x]", t.ok
			}
			return "[ ]", t.dim
		}
		items = append(items, boxItem{group: "INJECTED INTO EVERY SESSION"})
		g, c := check(s.Kit.MCP)
		items = append(items, boxItem{glyph: g, color: c, label: "smithy MCP server", hint: "board, cards and decisions as tools"})
		for _, sk := range s.Skills {
			g, c := check(slices.Contains(s.Kit.Skills, sk))
			hint := "skill"
			if p := s.Paths[sk]; p != "" {
				hint += " · " + skillDir(p)
			}
			items = append(items, boxItem{glyph: g, color: c, label: sk, hint: hint})
		}
		if len(s.Skills) == 0 {
			items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "no smithy skills installed: press i", static: true})
		}
		items = append(items, boxItem{group: "INSTALLED PER HARNESS"})
		for _, hs := range s.Harnesses {
			if !hs.Present {
				items = append(items, boxItem{glyph: t.g.dot, color: t.dim, label: hs.Harness, hint: "not installed", static: true})
				continue
			}
			g, c := t.g.pass, t.ok
			if len(hs.Missing) > 0 {
				g, c = t.g.wait, t.warn
			}
			mcp := "no MCP servers"
			if len(hs.MCP) > 0 {
				mcp = "MCP " + strings.Join(hs.MCP, ", ")
			}
			if hs.MCPError != "" {
				mcp = hs.MCPError
			}
			hint := fmt.Sprintf("smithy %d/%d", len(hs.Smithy), len(hs.Smithy)+len(hs.Missing))
			if len(hs.Other) > 0 {
				hint += fmt.Sprintf(" %s %d other skills", t.g.dot, len(hs.Other))
			}
			items = append(items, boxItem{glyph: g, color: c, label: hs.Harness, hint: hint + " " + t.g.dot + " " + mcp, static: true})
		}
	}
	items = append(items, m.noteItems()...)
	foot := "  " + keyHints(t, "↑↓", "move", "space", "toggle", "i", "install/update smithy skills", "esc", "close")
	return m.overBox(screen, m.floatBox(bw, bh, "", "Skills and MCP", items, m.setupSel, foot), w)
}

// agentsBox is tab: every session with what it does now, then what waits on you.
func (m chatView) agentsBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	running := 0
	for _, s := range m.sessions {
		if s.state == "running" {
			running++
		}
	}
	sum := fmt.Sprintf("Agents · %d running · %d sessions", running, len(m.sessions))
	if c := m.totalCost(); c > 0 {
		sum += fmt.Sprintf(" · $%.2f", c)
	}
	items := []boxItem{{group: "THIS PROJECT"}}
	list, shownOther := m.listed(), false
	for _, s := range list {
		if !m.here(s) && !shownOther {
			shownOther = true
			items = append(items, boxItem{group: "OTHER PROJECTS"})
		}
		g, c := m.sessionGlyph(s.state)
		title := s.title
		if title == "" {
			title = s.id
		}
		when := s.state
		if !s.since.IsZero() && s.state == "running" {
			if when = shortDur(time.Since(s.since).Truncate(time.Minute)); when == "0s" {
				when = "now"
			}
		}
		var parts []string
		where := ""
		if !m.here(s) {
			where = filepath.Base(s.dir)
		}
		for _, x := range []string{where, s.agent, s.model, s.id} {
			if x != "" {
				parts = append(parts, x)
			}
		}
		now := s.now
		if now == "" {
			now = s.kind
		}
		if now != "" {
			parts = append(parts, now)
		}
		if s.id == m.current && m.current != "" {
			title = t.g.prompt + " " + title
		}
		items = append(items, boxItem{glyph: g, color: c, label: title, hint: strings.Join(parts, " "+t.g.dot+" "), key: when})
	}
	if len(list) == 0 {
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "no sessions in this folder yet: type a message to start one", static: true})
	}
	if other := len(m.sessions) - len(list); other > 0 {
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: fmt.Sprintf("%d in other projects", other), hint: "a shows them", static: true})
	}
	if len(m.pending) > 0 {
		items = append(items, boxItem{group: fmt.Sprintf("WAITING ON YOU  %d", len(m.pending))})
		for _, d := range m.pending {
			items = append(items, boxItem{glyph: t.g.wait, color: t.warn, label: d.Question, hint: strings.Join(d.Options, " "+t.g.dot+" "), static: true})
		}
	}
	foot := "  " + keyHints(t, "↑↓", "move", "enter", "open", "s", "stop", "n", "new session", "a", projHint(m.allProj), "tab", "close")
	return m.overBox(screen, m.floatBox(bw, bh, "", sum, items, m.agentSel, foot), w)
}
