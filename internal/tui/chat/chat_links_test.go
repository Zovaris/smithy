//go:build harness

package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/ghx"
)

var demoLinks = api.SessionLinks{Session: "s-1",
	PRs:    []ghx.PRLink{{URL: "https://github.com/o/r/pull/7", Number: 7, Title: "Live links", State: "OPEN", Checks: "green", Review: "APPROVED", Mergeable: "MERGEABLE", Additions: 120, Deletions: 8}},
	Issues: []api.LinkIssue{{Kind: "linear", ID: "TK-112", Title: "Live PR links", Status: "In Progress", Assignee: "Ana", Milestone: "v0.26", URL: "https://linear.app/i/TK-112"}, {Kind: "github", ID: "#12", Title: "Bug", Status: "OPEN", URL: "https://github.com/o/r/issues/12"}},
}

func (f *fakeSettings) SessionLinks(_ context.Context, id string) (api.SessionLinks, error) {
	return demoLinks, nil
}

func (f *fakeSettings) AddLink(_ context.Context, id, ref string) (api.SessionLinks, error) {
	f.set = append(f.set, [2]string{"link:" + id, ref})
	return demoLinks, nil
}

func TestLinksModalShowsPRsAndIssues(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	f := &fakeSettings{}
	m.cfg = f
	m.current = "s-1"
	m = cpress(typeIn(m, "/links"), ckey("enter"))
	out := screen(m)
	for _, want := range []string{"Links", "PR #7 Live links", "green", "approved", "+120 −8", "pull/7", "TK-112 Live PR links", "In Progress", "Ana", "v0.26", "issue #12 Bug"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/links lacks %q:\n%s", want, out)
		}
	}
	m = cpress(m, ckey("esc"))
	m = cpress(typeIn(m, "/links tk-9"), ckey("enter"))
	if len(f.set) != 1 || f.set[0] != [2]string{"link:s-1", "tk-9"} {
		t.Fatalf("/links <ref> posts the ref: %v", f.set)
	}
}

func TestLinksNeedAnOpenSession(t *testing.T) {
	m := newChatTest(0)
	m.cfg = &fakeSettings{}
	m = cpress(typeIn(m, "/links"), ckey("enter"))
	if out := screen(m); !strings.Contains(out, "open a session first") {
		t.Fatalf("no session:\n%s", out)
	}
}

func TestAgentsPanelShowsLiveLinks(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	s, _ := json.Marshal(api.Session{ID: "s-1", Title: "build links", Dir: m.dir, Status: daemon.StatusDone})
	m.event(daemon.KindSessionStarted, "s-1", time.Now(), s)
	b, _ := json.Marshal(demoLinks)
	m.event(daemon.KindSessionLinks, "s-1", time.Now(), b)
	m = cpress(m, ckey("tab"))
	out := screen(m)
	for _, want := range []string{"PR #7 green +120 −8", "TK-112 In Progress"} {
		if !strings.Contains(out, want) {
			t.Fatalf("agents panel lacks %q:\n%s", want, out)
		}
	}
}
