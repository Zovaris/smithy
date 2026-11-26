//go:build harness

package tui

import (
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/daemon"
)

func TestAgentsListThisProjectFirst(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 100, 40
	m.dir = "/w/alpha"
	for _, s := range []daemon.Session{{ID: "a1", Title: "here one", Dir: "/w/alpha"}, {ID: "b1", Title: "elsewhere", Dir: "/w/beta"}, {ID: "a2", Title: "here two", Dir: "/w/alpha/sub"}} {
		m.meta(s.ID, s)
	}
	got := m.listed()
	if len(got) != 2 || got[0].id != "a1" || got[1].id != "a2" {
		t.Fatalf("this folder only: %+v", got)
	}
	view := strings.Join(m.agentsBox(make([]string, 40), 100, 40), "\n")
	if strings.Contains(view, "elsewhere") || !strings.Contains(view, "1 in other projects") {
		t.Errorf("other projects hidden behind a switch:\n%s", view)
	}
	n, _ := m.agentsKey("a")
	m = n.(chatView)
	got = m.listed()
	if len(got) != 3 || got[2].id != "b1" {
		t.Fatalf("a shows every project, this one first: %+v", got)
	}
	view = strings.Join(m.agentsBox(make([]string, 40), 100, 40), "\n")
	if !strings.Contains(view, "elsewhere") || !strings.Contains(view, "beta") {
		t.Errorf("other project rows name their folder:\n%s", view)
	}
	m.agentSel = 2
	n, _ = m.agentsKey("enter")
	if n.(chatView).current != "b1" {
		t.Errorf("enter opens the listed row, got %q", n.(chatView).current)
	}
}

func TestAgentsPanelShowsTheSessionsPRs(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 120, 40
	m.dir = "/w/alpha"
	m.meta("a1", daemon.Session{ID: "a1", Title: "ship it", Dir: "/w/alpha", PRs: []string{"https://github.com/acme/app/pull/7", "https://github.com/acme/app/pull/12"}})
	view := strings.Join(m.agentsBox(make([]string, 40), 120, 40), "\n")
	if !strings.Contains(view, "PR #7 #12") {
		t.Errorf("the row names the PRs the session opened:\n%s", view)
	}
}
