//go:build harness

package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/api"
)

type themeSettings struct {
	fakeSettings
	theme string
}

func (f *themeSettings) Settings(context.Context) (api.Settings, error) {
	return api.Settings{Settings: []api.SettingRow{{Key: "ui.theme", Value: f.theme}}}, nil
}

func TestThemeModalPicksAndSavesTheTheme(t *testing.T) {
	m := newChatTest(0)
	f := &themeSettings{theme: "steel"}
	m.cfg = f
	m = sendKey(typeIn(m, "/theme"), ckey("enter"))
	s := screen(m)
	for _, n := range Themes {
		if !strings.Contains(s, n) {
			t.Fatalf("the modal lists %s:\n%s", n, s)
		}
	}
	m = sendKey(m, ckey("down"))
	m = sendKey(m, ckey("enter"))
	if len(f.set) != 1 || f.set[0] != [2]string{"ui.theme", Themes[1]} {
		t.Fatalf("saved %v", f.set)
	}
	if m.th.name != Themes[1] || m.overlay != overlayNone {
		t.Errorf("theme %s overlay %v", m.th.name, m.overlay)
	}
}

func TestThemeCommandWithANameSetsIt(t *testing.T) {
	m := newChatTest(0)
	f := &themeSettings{}
	m.cfg = f
	m = sendKey(typeIn(m, "/theme plum"), ckey("enter"))
	if m.th.name != "plum" || len(f.set) != 1 {
		t.Fatalf("theme %s set %v", m.th.name, f.set)
	}
	m = sendKey(typeIn(m, "/theme nope"), ckey("enter"))
	if m.th.name != "plum" || !strings.Contains(screen(m), "unknown theme") {
		t.Fatalf("an unknown name is refused:\n%s", screen(m))
	}
}

func TestChatStartsInTheSavedTheme(t *testing.T) {
	m := newChatTest(0)
	m.cfg = &themeSettings{theme: "frost"}
	m = run(m, m.loadTheme())
	if m.th.name != "frost" {
		t.Fatalf("theme %s", m.th.name)
	}
}
