//go:build harness

package chat

import (
	"context"
	"errors"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
)

// projectAPI is the part of the daemon client the project modal uses; a sessions API without it
// starts every session in the chat's folder.
type projectAPI interface {
	Projects(ctx context.Context) (api.ProjectList, error)
	AddProject(ctx context.Context, dir string, def bool) (api.ProjectList, error)
	ListDirs(ctx context.Context, dir string) (api.DirList, error)
}

// projectPick is the modal a new session's first message opens: which saved project of the
// daemon it starts in. dir is the pick; it is fixed once the session runs.
type projectPick struct {
	dir     string
	pending string           // the message that starts the session once a project is picked
	list    *api.ProjectList // nil while it loads
	sel     int
	browse  *api.DirList // the folder picker, nil when it is closed
	err     string
}

type projectsMsg struct {
	l   api.ProjectList
	err error
}

type dirsMsg struct {
	l   api.DirList
	err error
}

var errNoProjects = errors.New("projects need a running daemon")

// projRow is one row of the modal: a project, this folder, the add row, or a folder to open.
type projRow struct {
	label, hint, dir string
	add, up          bool
}

func (m chatView) askProject(pa projectAPI, text string) (chatView, tea.Cmd) {
	m.proj = projectPick{pending: text}
	m.overlay = overlayProject
	return m, func() tea.Msg {
		ctx, cancel := daemonCtx()
		defer cancel()
		l, err := pa.Projects(ctx)
		return projectsMsg{l: l, err: err}
	}
}

func (m chatView) projectsLoaded(msg projectsMsg) (tea.Model, tea.Cmd) {
	if m.overlay != overlayProject {
		return m, nil
	}
	if msg.err != nil || len(msg.l.Projects) == 0 {
		return m.pickProject(m.dir)
	}
	m.proj.list = &msg.l
	m.proj.sel = 0
	pick := msg.l.Default
	for _, p := range msg.l.Projects {
		if p.Dir == m.root {
			pick = p.Dir
		}
	}
	for i, r := range m.projRows() {
		if pick != "" && r.dir == pick {
			m.proj.sel = i
		}
	}
	return m, nil
}

func (m chatView) dirsLoaded(msg dirsMsg) (tea.Model, tea.Cmd) {
	if m.overlay != overlayProject {
		return m, nil
	}
	if msg.err != nil {
		m.proj.err = msg.err.Error()
		return m, nil
	}
	m.proj.browse, m.proj.sel, m.proj.err = &msg.l, 0, ""
	return m, nil
}

// projRows are the modal's rows: the folder picker's when it is open, else this folder (when it
// is no saved project), the saved projects and the add row.
func (m chatView) projRows() []projRow {
	if b := m.proj.browse; b != nil {
		var rows []projRow
		if b.Parent != "" {
			rows = append(rows, projRow{label: "..", dir: b.Parent, up: true})
		}
		for _, d := range b.Dirs {
			rows = append(rows, projRow{label: d, dir: filepath.Join(b.Dir, d)})
		}
		return rows
	}
	var rows []projRow
	l := m.proj.list
	if l == nil {
		return nil
	}
	saved := false
	for _, p := range l.Projects {
		saved = saved || p.Dir == m.root
	}
	if !saved {
		rows = append(rows, projRow{label: filepath.Base(m.dir), hint: "this folder · " + m.dir, dir: m.dir})
	}
	for _, p := range l.Projects {
		h := p.Dir
		if p.Dir == m.root {
			h = "this folder · " + h
		}
		rows = append(rows, projRow{label: p.Name, hint: h, dir: p.Dir})
	}
	return append(rows, projRow{label: "add a project…", hint: "pick a folder on the daemon's machine", add: true})
}

func (m chatView) listDirs(dir string) tea.Cmd {
	pa, ok := m.sess.(projectAPI)
	return func() tea.Msg {
		if !ok {
			return dirsMsg{err: errNoProjects}
		}
		ctx, cancel := daemonCtx()
		defer cancel()
		l, err := pa.ListDirs(ctx, dir)
		return dirsMsg{l: l, err: err}
	}
}

// pickProject fixes the new session's folder and starts it with the pending message.
func (m chatView) pickProject(dir string) (tea.Model, tea.Cmd) {
	text := m.proj.pending
	m.overlay = overlayNone
	m.proj = projectPick{dir: dir}
	return m.send(text)
}

func (m chatView) projectKey(k string) (tea.Model, tea.Cmd) {
	rows := m.projRows()
	switch k {
	case "up", "k":
		m.proj.sel = max(m.proj.sel-1, 0)
		return m, nil
	case "down", "j":
		m.proj.sel = min(m.proj.sel+1, max(len(rows)-1, 0))
		return m, nil
	}
	if b := m.proj.browse; b != nil {
		switch k {
		case "esc":
			m.proj.browse, m.proj.sel, m.proj.err = nil, len(m.projRows())-1, ""
		case "enter", "right":
			if m.proj.sel < len(rows) {
				return m, m.listDirs(rows[m.proj.sel].dir)
			}
		case "backspace", "left":
			if b.Parent != "" {
				return m, m.listDirs(b.Parent)
			}
		case "a":
			pa, ok := m.sess.(projectAPI)
			if !ok {
				m.proj.err = errNoProjects.Error()
				return m, nil
			}
			dir := b.Dir
			ctx, cancel := daemonCtx()
			defer cancel()
			l, err := pa.AddProject(ctx, dir, false)
			if err != nil {
				m.proj.err = err.Error()
				return m, nil
			}
			m.proj.list, m.proj.browse, m.proj.err = &l, nil, ""
			for i, r := range m.projRows() {
				if r.dir == dir {
					m.proj.sel = i
				}
			}
		}
		return m, nil
	}
	switch k {
	case "esc":
		m.input.SetValue(m.proj.pending)
		m.overlay, m.proj = overlayNone, projectPick{}
	case "enter":
		if m.proj.sel >= len(rows) {
			return m, nil
		}
		r := rows[m.proj.sel]
		if r.add {
			return m, m.listDirs("")
		}
		return m.pickProject(r.dir)
	}
	return m, nil
}

// projectBox is the project modal, or its folder picker.
func (m chatView) projectBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	var items []boxItem
	title := "Which project does this session work in?"
	foot := "  " + keyHints(t, "↑↓", "move", "enter", "start here", "esc", "cancel")
	switch {
	case m.proj.browse != nil:
		title = "Add a project · " + m.proj.browse.Dir
		items = append(items, boxItem{group: "FOLDERS ON THE DAEMON'S MACHINE"})
		for _, r := range m.projRows() {
			items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: r.label})
		}
		if len(m.proj.browse.Dirs) == 0 {
			items = append(items, boxItem{glyph: t.G.Dot, color: t.Dim, label: "no folders inside", static: true})
		}
		foot = "  " + keyHints(t, "↑↓", "move", "enter", "open", "←", "up", "a", "add this folder", "esc", "back")
	case m.proj.list == nil:
		items = append(items, boxItem{glyph: t.G.Dot, color: t.Muted, label: "reading the projects…", static: true})
	default:
		items = append(items, boxItem{group: "PROJECTS · fixed once the session starts"})
		for _, r := range m.projRows() {
			it := boxItem{glyph: t.G.Dot, color: t.Muted, label: r.label, hint: r.hint}
			if r.add {
				it.glyph, it.color = "+", t.Accent
			}
			if r.dir != "" && r.dir == m.proj.list.Default {
				it.key = t.G.On + " default"
			}
			items = append(items, it)
		}
	}
	if m.proj.err != "" {
		items = append(items, boxItem{glyph: t.G.Fail, color: t.Err, label: m.proj.err, static: true})
	}
	return m.overBox(screen, m.floatBox(bw, bh, "", title, items, m.proj.sel, foot), w)
}
