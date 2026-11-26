//go:build harness

package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
)

type fakeSettings struct {
	serve   []bool
	set     [][2]string
	unset   []string
	removed []string
}

func (f *fakeSettings) Settings(context.Context) (daemon.Settings, error) {
	return daemon.Settings{Serve: true, Keys: []daemon.KeyRow{{Service: "linear", Masked: "****wxyz"}}}, nil
}
func (f *fakeSettings) SetSetting(_ context.Context, k, v string) error {
	f.set = append(f.set, [2]string{k, v})
	return nil
}
func (f *fakeSettings) UnsetSetting(_ context.Context, k string) error {
	f.unset = append(f.unset, k)
	return nil
}
func (f *fakeSettings) RemoveKey(_ context.Context, s string) error {
	f.removed = append(f.removed, s)
	return nil
}
func (f *fakeSettings) SetServe(_ context.Context, on bool) error {
	f.serve = append(f.serve, on)
	return nil
}
func (f *fakeSettings) Doctor(context.Context) ([]string, error) {
	return []string{"ok   git found"}, nil
}

func TestDetachQuitsAndLeavesTheDaemon(t *testing.T) {
	m := newChatTest(0)
	m.where = whereBackground
	m = typeIn(m, "/detach")
	_, cmd := m.Update(ckey("enter"))
	if cmd == nil {
		t.Fatal("/detach must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("/detach must quit")
	}
	m = newChatTest(0)
	m.where = whereStandalone
	m = typeIn(m, "/detach")
	n, cmd := m.Update(ckey("enter"))
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("standalone: the daemon dies with this process, so /detach must not quit")
		}
	}
	if !strings.Contains(screen(n.(chatView)), "standalone") {
		t.Fatal("standalone /detach must say why it stays")
	}
}

func TestSettingsCommandsGoThroughTheDaemon(t *testing.T) {
	f := &fakeSettings{}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = f
	for _, line := range []string{"/serve off", "/serve on", "/set linear.team ENG", "/unset linear.team", "/key rm linear"} {
		m = cpress(typeIn(m, line), ckey("enter"))
	}
	if len(f.serve) != 2 || f.serve[0] || !f.serve[1] {
		t.Fatalf("serve: %v", f.serve)
	}
	if len(f.set) != 1 || f.set[0] != [2]string{"linear.team", "ENG"} || len(f.unset) != 1 || len(f.removed) != 1 {
		t.Fatalf("set %v unset %v removed %v", f.set, f.unset, f.removed)
	}
	m = cpress(typeIn(m, "/doctor"), ckey("enter"))
	if !strings.Contains(screen(m), "git found") {
		t.Fatalf("doctor lines missing:\n%s", screen(m))
	}
	m = cpress(typeIn(m, "/keys"), ckey("enter"))
	if out := screen(m); !strings.Contains(out, "linear ****wxyz") || !strings.Contains(out, "serve on") {
		t.Fatalf("keys/serve missing:\n%s", out)
	}
}
