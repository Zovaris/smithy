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

func (f *fakeSettings) RemoveLink(_ context.Context, id, ref string) (api.SessionLinks, error) {
	f.removed = append(f.removed, ref)
	return demoLinks, nil
}

func TestLinksAddRemoveOpenAndCopy(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	f := &fakeSettings{}
	m.cfg = f
	m.current = "s-1"
	var opened []string
	m.opener = func(u string) error { opened = append(opened, u); return nil }
	m = cpress(typeIn(m, "/links add https://docs.example.com/a"), ckey("enter"))
	m = cpress(m, ckey("esc"))
	m = cpress(typeIn(m, "/links rm 2"), ckey("enter"))
	if len(f.set) != 1 || f.set[0][1] != "https://docs.example.com/a" || len(f.removed) != 1 || f.removed[0] != demoLinks.Issues[0].URL {
		t.Fatalf("add %v, rm %v", f.set, f.removed)
	}
	m = cpress(m, ckey("esc"))
	m = cpress(typeIn(m, "/links"), ckey("enter"))
	if !strings.Contains(screen(m), "x remove") {
		t.Fatalf("modal names its keys:\n%s", screen(m))
	}
	m = cpress(m, ckey("down"))
	m = cpress(m, ckey("enter"))
	if len(opened) != 1 || opened[0] != demoLinks.PRs[0].URL {
		t.Fatalf("enter opens the selected link: %v", opened)
	}
	if _, cmd := m.Update(ckey("c")); cmd == nil {
		t.Fatal("c copies the URL")
	}
	m = cpress(m, ckey("x"))
	if len(f.removed) != 2 || f.removed[1] != demoLinks.PRs[0].URL {
		t.Fatalf("x removes the selected link: %v", f.removed)
	}
}

func TestLinkStatusChangePostsOneLine(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 120, 30
	started(&m, "s-1", "one")
	m.open("s-1")
	l := demoLinks
	b, _ := json.Marshal(l)
	m.event(daemon.KindSessionLinks, "s-1", time.Now(), b)
	l.PRs = []ghx.PRLink{l.PRs[0]}
	l.PRs[0].Checks = "red"
	b, _ = json.Marshal(l)
	m.event(daemon.KindSessionLinks, "s-1", time.Now(), b)
	l.PRs[0].State = "MERGED"
	b, _ = json.Marshal(l)
	m.event(daemon.KindSessionLinks, "s-1", time.Now(), b)
	out := screen(m)
	if strings.Count(out, "- PR #7 checks red") != 1 || strings.Count(out, "- PR #7 merged") != 1 {
		t.Fatalf("one line per change:\n%s", out)
	}
}

func TestSidebarLinkClickOpens(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 40
	started(&m, "s-1", "one")
	m.open("s-1")
	b, _ := json.Marshal(demoLinks)
	m.event(daemon.KindSessionLinks, "s-1", time.Now(), b)
	var opened []string
	m.opener = func(u string) error { opened = append(opened, u); return nil }
	y := rowOf(t, m, "PR #7")
	m = click(m, m.tw()+4, y)
	if len(opened) != 1 || opened[0] != demoLinks.PRs[0].URL {
		t.Fatalf("a click on a sidebar link opens it: %v", opened)
	}
}
