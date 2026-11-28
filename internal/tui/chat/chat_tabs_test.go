//go:build harness

package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
)

func started(m *chatView, id, title string) {
	b, _ := json.Marshal(api.Session{ID: id, Title: title, Dir: m.dir, Status: daemon.StatusDone})
	m.event(daemon.KindSessionStarted, id, time.Now(), b)
}

func click(m chatView, x, y int) chatView {
	return cpress(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func TestTabsSwitchByKeyAndClick(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 120, 30
	m.tabsOn = true
	started(&m, "s-1", "fix the login form")
	started(&m, "s-2", "write release notes")
	m.open("s-1")
	m = cpress(m, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	if m.current != "s-2" {
		t.Fatalf("ctrl+n goes to the next tab, at %q", m.current)
	}
	m = cpress(m, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.current != "s-1" {
		t.Fatalf("ctrl+p goes back, at %q", m.current)
	}
	out := screen(m)
	if !strings.Contains(out, "ctrl+n") || !strings.Contains(out, "Fix the login form") {
		t.Fatalf("tab key hint and full titles:\n%s", out)
	}
	y := rowOf(t, m, "Write release notes")
	x := strings.Index(strings.Split(out, "\n")[y], "Write release")
	m = click(m, x+2, y)
	if m.current != "s-2" {
		t.Fatalf("a click opens the tab, at %q", m.current)
	}
	row := strings.Split(screen(m), "\n")[y]
	m = click(m, strings.LastIndex(row, "+"), y)
	if m.current != "" {
		t.Fatalf("+ starts a new session, at %q", m.current)
	}
}
