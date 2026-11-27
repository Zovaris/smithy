//go:build harness

package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
)

type fakeBoard struct {
	b       daemon.TaskBoard
	project []string
	moves   [][2]string
}

func (f *fakeBoard) Tasks(_ context.Context, project string) (daemon.TaskBoard, error) {
	f.project = append(f.project, project)
	return f.b, nil
}

func (f *fakeBoard) MoveTask(_ context.Context, file, status string) (daemon.TaskCard, error) {
	f.moves = append(f.moves, [2]string{file, status})
	for _, l := range f.b.Lists {
		for i := range l.Tasks {
			if l.Tasks[i].File == file {
				l.Tasks[i].Status = status
				return l.Tasks[i], nil
			}
		}
	}
	return daemon.TaskCard{}, nil
}

func boardFixture(t *testing.T) (*fakeBoard, string) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "01-schema.md")
	if err := os.WriteFile(f1, []byte("# 01 · Schema\nStatus: done\n\n## What\nthe tables\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &fakeBoard{b: daemon.TaskBoard{Project: dir, Statuses: []string{"todo", "doing", "done", "dropped"}, Lists: []daemon.TaskList{
		{Dir: dir + "/v1", Name: "v1", Tasks: []daemon.TaskCard{
			{File: f1, NN: "01", Title: "Schema", Status: "done", Blockers: []string{}},
			{File: dir + "/02-api.md", NN: "02", Title: "API", Status: "doing", PR: "https://github.com/o/r/pull/12", Blockers: []string{"01"}},
			{File: dir + "/03-ui.md", NN: "03", Title: "UI", Status: "todo", Blockers: []string{"02"}},
		}},
		{Dir: dir + "/v2", Name: "v2", Tasks: []daemon.TaskCard{{File: dir + "/01-later.md", NN: "01", Title: "Later", Status: "todo", Blockers: []string{}}}},
	}}}, f1
}

func pressCode(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func TestBoardShowsColumnsDepsAndPRs(t *testing.T) {
	f, _ := boardFixture(t)
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.board = f
	m = cpress(typeIn(m, "/board"), ckey("enter"))
	if m.overlay != overlayBoard {
		t.Fatal("/board must open the board modal")
	}
	out := screen(m)
	for _, s := range []string{"TODO  1", "DOING  1", "DONE  1", "DROPPED  0", "03 · UI", "after 02", "blocked", "PR #12", "v1", "v2"} {
		if !strings.Contains(out, s) {
			t.Errorf("board lacks %q:\n%s", s, out)
		}
	}
	if strings.Contains(out, "Later") {
		t.Error("only the picked task dir shows")
	}
	if strings.Index(out, "03 · UI") > strings.Index(out, "02 · API") {
		t.Error("columns go todo, doing, done, dropped")
	}
}

func TestBoardMovesFiltersAndOpens(t *testing.T) {
	f, f1 := boardFixture(t)
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.board = f
	var opened []string
	m.opener = func(u string) error { opened = append(opened, u); return nil }
	m = cpress(typeIn(m, "/board"), ckey("enter"))

	m = cpress(m, pressCode(tea.KeyRight))
	if len(f.moves) != 1 || f.moves[0][1] != "doing" || !strings.HasSuffix(f.moves[0][0], "03-ui.md") {
		t.Fatalf("right moves the selected task one column on: %v", f.moves)
	}
	if out := screen(m); !strings.Contains(out, "DOING  2") || !strings.Contains(out, "TODO  0") {
		t.Fatalf("the move shows at once:\n%s", out)
	}
	if l := strings.Split(screen(m), "\n")[rowOf(t, m, "03 · UI")]; !strings.Contains(l, m.th.g.sel) {
		t.Fatalf("the cursor stays on the moved task: %q", l)
	}
	m = cpress(m, pressCode(tea.KeyLeft))
	if len(f.moves) != 2 || f.moves[1][1] != "todo" {
		t.Fatalf("left moves it back, the cursor follows the task: %v", f.moves)
	}

	m = cpress(m, ckey("down"))
	m = cpress(m, ckey("o"))
	if len(opened) != 1 || opened[0] != "https://github.com/o/r/pull/12" {
		t.Fatalf("o opens the task's PR: %v", opened)
	}

	m = cpress(m, ckey("down"))
	m = cpress(m, ckey("enter"))
	if m.overlay != overlayInfo || !strings.Contains(screen(m), "the tables") {
		t.Fatalf("enter opens the task file:\n%s", screen(m))
	}
	_ = f1
	m = cpress(m, ckey("esc"))
	if m.overlay != overlayBoard {
		t.Fatal("esc on the file goes back to the board")
	}

	m = cpress(m, ckey("tab"))
	if out := screen(m); !strings.Contains(out, "Later") || strings.Contains(out, "03 · UI") {
		t.Fatalf("tab picks the next task dir:\n%s", out)
	}
	m = cpress(m, ckey("esc"))
	if m.overlay != overlayNone {
		t.Fatal("esc closes the board")
	}
}

func TestBoardArgPicksTheTaskDir(t *testing.T) {
	f, _ := boardFixture(t)
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.board = f
	m = cpress(typeIn(m, "/board v2"), ckey("enter"))
	if out := screen(m); !strings.Contains(out, "Later") || strings.Contains(out, "03 · UI") {
		t.Fatalf("/board v2 opens on that task dir:\n%s", out)
	}
	if len(f.project) != 1 || f.project[0] != m.dir {
		t.Fatalf("the board reads this chat's folder: %v", f.project)
	}
}
