//go:build harness

package chat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/api"
)

// fakeProjects is a daemon with saved projects and a folder tree to browse.
type fakeProjects struct {
	fakeSessions
	list  api.ProjectList
	tree  map[string][]string
	dirs  []string
	added []string
}

func (f *fakeProjects) StartSession(ctx context.Context, agent, dir, msg string) (api.Session, error) {
	f.dirs = append(f.dirs, dir)
	s, err := f.fakeSessions.StartSession(ctx, agent, dir, msg)
	s.Dir = dir
	return s, err
}

func (f *fakeProjects) Projects(context.Context) (api.ProjectList, error) { return f.list, nil }

func (f *fakeProjects) AddProject(_ context.Context, dir string, def bool) (api.ProjectList, error) {
	f.added = append(f.added, dir)
	f.list.Projects = append(f.list.Projects, api.Project{Dir: dir, Name: filepath.Base(dir)})
	return f.list, nil
}

func (f *fakeProjects) ListDirs(_ context.Context, dir string) (api.DirList, error) {
	if dir == "" {
		dir = "/home"
	}
	l := api.DirList{Dir: dir, Dirs: f.tree[dir]}
	if dir != "/" {
		l.Parent = filepath.Dir(dir)
	}
	return l, nil
}

func projectsTest() (*fakeProjects, chatView) {
	f := &fakeProjects{list: api.ProjectList{
		Projects: []api.Project{{Dir: "/w/alpha", Name: "alpha"}, {Dir: "/w/beta", Name: "beta"}},
		Default:  "/w/alpha",
	}, tree: map[string][]string{"/home": {"code", "notes"}}}
	m := newChatTest(0)
	m.width, m.height = 100, 32
	m.sess = f
	m.dir, m.root = "/w/here", "/w/here"
	return f, m
}

func TestNewSessionAsksWhichProject(t *testing.T) {
	f, m := projectsTest()
	m = typeIn(m, "hello")
	m = cpress(m, ckey("enter"))
	if m.overlay != overlayProject || len(f.dirs) != 0 {
		t.Fatalf("first message opens the project modal, started %v overlay %v", f.dirs, m.overlay)
	}
	out := screen(m)
	for _, s := range []string{"alpha", "beta", "here", "default", "add a project"} {
		if !strings.Contains(out, s) {
			t.Errorf("modal lacks %q:\n%s", s, out)
		}
	}
	if r := m.projRows(); r[m.proj.sel].dir != "/w/alpha" {
		t.Errorf("default preselected, got %+v", r[m.proj.sel])
	}
	m = cpress(m, ckey("down"))
	m = cpress(m, ckey("enter"))
	if len(f.dirs) != 1 || f.dirs[0] != "/w/beta" || f.started[0] != "hello" || m.overlay != overlayNone || m.current != "s-1" {
		t.Fatalf("enter starts in the picked project: dirs %v started %v overlay %v", f.dirs, f.started, m.overlay)
	}
	m = typeIn(m, "again")
	m = cpress(m, ckey("enter"))
	if len(f.dirs) != 1 || len(f.sent) != 1 || m.overlay != overlayNone {
		t.Fatalf("the project is fixed once the session runs: dirs %v sent %v", f.dirs, f.sent)
	}
	m.newSession()
	m = typeIn(m, "third")
	m = cpress(m, ckey("enter"))
	if m.overlay != overlayProject {
		t.Errorf("a new session asks again")
	}
}

func TestProjectModalEscKeepsTheMessage(t *testing.T) {
	f, m := projectsTest()
	m = typeIn(m, "hello")
	m = cpress(m, ckey("enter"))
	m = cpress(m, ckey("esc"))
	if m.overlay != overlayNone || len(f.dirs) != 0 || m.input.Value() != "hello" {
		t.Errorf("esc cancels and keeps the text: overlay %v dirs %v input %q", m.overlay, f.dirs, m.input.Value())
	}
}

func TestNoSavedProjectsStartsHere(t *testing.T) {
	f, m := projectsTest()
	f.list = api.ProjectList{}
	m = typeIn(m, "hello")
	m = cpress(m, ckey("enter"))
	if len(f.dirs) != 1 || f.dirs[0] != "/w/here" || m.overlay != overlayNone {
		t.Fatalf("no projects: start in this folder, dirs %v overlay %v", f.dirs, m.overlay)
	}
}

func TestAddProjectWithFolderPicker(t *testing.T) {
	f, m := projectsTest()
	m = typeIn(m, "hello")
	m = cpress(m, ckey("enter"))
	m.proj.sel = len(m.projRows()) - 1
	m = cpress(m, ckey("enter"))
	if m.proj.browse == nil || m.proj.browse.Dir != "/home" {
		t.Fatalf("add opens the folder picker at the daemon's home: %+v", m.proj.browse)
	}
	if out := screen(m); !strings.Contains(out, "code") || !strings.Contains(out, "notes") || !strings.Contains(out, "/home") {
		t.Errorf("picker lists the folders:\n%s", out)
	}
	m.proj.sel = 1
	m = cpress(m, ckey("enter"))
	if m.proj.browse == nil || m.proj.browse.Dir != "/home/code" {
		t.Fatalf("enter opens a folder: %+v", m.proj.browse)
	}
	m = cpress(m, ckey("a"))
	if len(f.added) != 1 || f.added[0] != "/home/code" || m.proj.browse != nil {
		t.Fatalf("a saves the folder: added %v browse %+v", f.added, m.proj.browse)
	}
	if r := m.projRows(); r[m.proj.sel].dir != "/home/code" {
		t.Errorf("the new project is selected: %+v", r[m.proj.sel])
	}
	m = cpress(m, ckey("enter"))
	if len(f.dirs) != 1 || f.dirs[0] != "/home/code" {
		t.Errorf("starts in the new project: %v", f.dirs)
	}
}
