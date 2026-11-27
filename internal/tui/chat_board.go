//go:build harness

package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/tasks"
)

// boardAPI is the part of the daemon client /board uses: the task files the web Board shows.
type boardAPI interface {
	Tasks(ctx context.Context, project string) (daemon.TaskBoard, error)
	MoveTask(ctx context.Context, file, status string) (daemon.TaskCard, error)
}

// boardView is the open /board: the project's task lists, the picked one, and the selected task.
type boardView struct {
	b    *daemon.TaskBoard // nil while it loads
	list int
	file string // the selected task, kept across moves and reloads
	err  string
}

type boardMsg struct {
	b    daemon.TaskBoard
	want string // the task dir /board named
	err  error
}

type boardMovedMsg struct {
	file, from string
	err        error
}

func (m chatView) project() string {
	if m.proj.dir != "" {
		return m.proj.dir
	}
	return m.dir
}

// openBoard is /board [task dir]: this folder's task files in the shared modal, a group per status.
func (m chatView) openBoard(want string) (tea.Model, tea.Cmd) {
	if m.board == nil {
		m.system("/board needs the daemon")
		return m, nil
	}
	m.overlay, m.boardSt = overlayBoard, boardView{}
	api, dir := m.board, m.project()
	return m, func() tea.Msg {
		b, err := api.Tasks(context.Background(), dir)
		return boardMsg{b: b, want: want, err: err}
	}
}

func (m chatView) boardLoaded(msg boardMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.boardSt.err = msg.err.Error()
		return m, nil
	}
	m.boardSt.b = &msg.b
	for i, l := range msg.b.Lists {
		if msg.want != "" && (l.Name == msg.want || strings.Contains(l.Dir, msg.want)) {
			m.boardSt.list = i
		}
	}
	if c := m.boardCards(); len(c) > 0 {
		m.boardSt.file = c[0].File
	}
	return m, nil
}

func (m chatView) boardList() *daemon.TaskList {
	b := m.boardSt.b
	if b == nil || m.boardSt.list >= len(b.Lists) {
		return nil
	}
	return &b.Lists[m.boardSt.list]
}

// boardCards is the picked list in board order: by status column, then NN.
func (m chatView) boardCards() []daemon.TaskCard {
	l := m.boardList()
	if l == nil {
		return nil
	}
	var out []daemon.TaskCard
	for _, s := range tasks.Statuses {
		for _, t := range l.Tasks {
			if t.Status == s {
				out = append(out, t)
			}
		}
	}
	return out
}

func (m chatView) boardSel() int {
	c := m.boardCards()
	for i, t := range c {
		if t.File == m.boardSt.file {
			return i
		}
	}
	return 0
}

// cardBlocked reports a todo task still waiting on a blocker that is not done or dropped.
func cardBlocked(l *daemon.TaskList, t daemon.TaskCard) bool {
	if t.Status != "todo" {
		return false
	}
	closed := map[string]bool{}
	for _, x := range l.Tasks {
		if x.Status == "done" || x.Status == "dropped" {
			closed[strings.TrimLeft(x.NN, "0")] = true
		}
	}
	for _, b := range t.Blockers {
		if !closed[strings.TrimLeft(b, "0")] {
			return true
		}
	}
	return false
}

func prNum(u string) string { return "#" + u[strings.LastIndex(u, "/")+1:] }

func (m chatView) boardBox(screen []string, w, h int) []string {
	t := m.th
	bw, bh := m.boxSize(w, h)
	st := m.boardSt
	l := m.boardList()
	title := "Board"
	var items []boxItem
	switch {
	case st.err != "":
		items = append(items, boxItem{glyph: t.g.off, color: t.err, label: st.err, static: true})
	case st.b == nil:
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "reading the task files…", static: true})
	case l == nil:
		items = append(items, boxItem{glyph: t.g.dot, color: t.muted, label: "no task files in " + filepath.Base(st.b.Project), hint: `smithy tasks add "<title>" [dir]`, static: true})
	default:
		names := make([]string, len(st.b.Lists))
		for i, x := range st.b.Lists {
			names[i] = x.Name
			if i == st.list {
				names[i] = "[" + x.Name + "]"
			}
		}
		title = "Board " + t.g.dot + " " + filepath.Base(st.b.Project) + " " + t.g.dot + " " + strings.Join(names, " ")
	}
	sel := len(items)
	if l != nil {
		cards := m.boardCards()
		sel = m.boardSel() // floatBox counts selectable rows only
		for _, s := range tasks.Statuses {
			n := 0
			for _, c := range cards {
				if c.Status == s {
					n++
				}
			}
			items = append(items, boxItem{group: fmt.Sprintf("%s  %d", strings.ToUpper(s), n)})
			for _, c := range cards {
				if c.Status != s {
					continue
				}
				g, col := t.g.off, t.muted
				key := ""
				switch {
				case s == "doing":
					g, col = t.g.half, t.info
				case s == "dropped":
					g = t.g.fail
				case s == "done":
					g, col = t.g.pass, t.ok
				case cardBlocked(l, c):
					g, col, key = t.g.wait, t.warn, "blocked"
				}
				var hint []string
				if len(c.Blockers) > 0 {
					hint = append(hint, "after "+strings.Join(c.Blockers, ", "))
				}
				if c.PR != "" {
					hint = append(hint, "PR "+prNum(c.PR))
				}
				items = append(items, boxItem{glyph: g, color: col, label: c.NN + " · " + c.Title, hint: strings.Join(hint, " "+t.g.dot+" "), key: key})
			}
		}
	}
	foot := "  " + keyHints(t, "↑↓", "move", "←→", "status", "enter", "open file", "o", "open PR", "tab", "task dir", "esc", "close")
	return m.overBox(screen, m.floatBox(bw, bh, "", title, items, sel, foot), w)
}

func (m chatView) boardKey(k string) (tea.Model, tea.Cmd) {
	cards := m.boardCards()
	cur := m.boardSel()
	switch k {
	case "esc", "q":
		m.overlay = overlayNone
	case "up", "k":
		if cur > 0 {
			m.boardSt.file = cards[cur-1].File
		}
	case "down", "j":
		if cur+1 < len(cards) {
			m.boardSt.file = cards[cur+1].File
		}
	case "tab", "shift+tab":
		if b := m.boardSt.b; b != nil && len(b.Lists) > 0 {
			step := 1
			if k == "shift+tab" {
				step = len(b.Lists) - 1
			}
			m.boardSt.list = (m.boardSt.list + step) % len(b.Lists)
			m.boardSt.file = ""
			if c := m.boardCards(); len(c) > 0 {
				m.boardSt.file = c[0].File
			}
		}
	case "left", "right", "h", "l":
		if len(cards) == 0 {
			return m, nil
		}
		c := cards[cur]
		i := slices.Index(tasks.Statuses, c.Status)
		if k == "left" || k == "h" {
			i--
		} else {
			i++
		}
		if i < 0 || i >= len(tasks.Statuses) {
			return m, nil
		}
		to := tasks.Statuses[i]
		m.setCardStatus(c.File, to)
		api := m.board
		return m, func() tea.Msg {
			_, err := api.MoveTask(context.Background(), c.File, to)
			return boardMovedMsg{file: c.File, from: c.Status, err: err}
		}
	case "o":
		if len(cards) == 0 || cards[cur].PR == "" {
			m.status("this task has no PR yet")
			return m, nil
		}
		u, open := cards[cur].PR, m.opener
		if open == nil {
			open = openURL
		}
		m.status("opening " + u)
		return m, func() tea.Msg {
			if err := open(u); err != nil {
				return sessionErrMsg{what: "open the PR", err: err}
			}
			return nil
		}
	case "enter":
		if len(cards) == 0 {
			return m, nil
		}
		return m.openTaskFile(cards[cur])
	}
	return m, nil
}

// setCardStatus shows a move at once, before the daemon has written the file.
func (m *chatView) setCardStatus(file, status string) {
	l := m.boardList()
	if l == nil {
		return
	}
	ts := slices.Clone(l.Tasks)
	for i := range ts {
		if ts[i].File == file {
			ts[i].Status = status
		}
	}
	l.Tasks = ts
}

func (m chatView) boardMoved(msg boardMovedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setCardStatus(msg.file, msg.from)
		m.status(filepath.Base(msg.file) + " not moved: " + msg.err.Error())
	}
	return m, nil
}

// openTaskFile shows the task file read-only; esc goes back to the board. A daemon on another
// machine keeps its files there, so the panel then names the path.
func (m chatView) openTaskFile(c daemon.TaskCard) (tea.Model, tea.Cmd) {
	b, err := os.ReadFile(c.File)
	if err != nil {
		lines := []string{"the file is on the daemon's machine\t" + c.File}
		m.info = infoPanel{title: c.NN + " · " + c.Title, lines: lines}
		m.overlay, m.infoBack = overlayInfo, overlayBoard
		return m, nil
	}
	lines := []string{"-> " + c.File}
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		switch {
		case strings.TrimSpace(l) == "":
		case strings.HasPrefix(l, "#"):
			lines = append(lines, "#"+strings.ToUpper(strings.TrimSpace(strings.TrimLeft(l, "#"))))
		default:
			lines = append(lines, strings.ReplaceAll(l, "\t", "  "))
		}
	}
	m.info = infoPanel{title: c.NN + " · " + c.Title, lines: lines, scroll: true}
	m.overlay, m.infoBack, m.pal = overlayInfo, overlayBoard, palette{}
	return m, nil
}
