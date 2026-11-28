//go:build harness

package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/tui/kit"
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
	for _, n := range kit.Themes {
		if !strings.Contains(s, n) {
			t.Fatalf("the modal lists %s:\n%s", n, s)
		}
	}
	m = sendKey(m, ckey("down"))
	m = sendKey(m, ckey("enter"))
	if len(f.set) != 1 || f.set[0] != [2]string{"ui.theme", kit.Themes[1]} {
		t.Fatalf("saved %v", f.set)
	}
	if m.th.Name != kit.Themes[1] || m.overlay != overlayNone {
		t.Errorf("theme %s overlay %v", m.th.Name, m.overlay)
	}
}

func TestThemeCommandWithANameSetsIt(t *testing.T) {
	m := newChatTest(0)
	f := &themeSettings{}
	m.cfg = f
	m = sendKey(typeIn(m, "/theme plum"), ckey("enter"))
	if m.th.Name != "plum" || len(f.set) != 1 {
		t.Fatalf("theme %s set %v", m.th.Name, f.set)
	}
	m = sendKey(typeIn(m, "/theme nope"), ckey("enter"))
	if m.th.Name != "plum" || !strings.Contains(screen(m), "unknown theme") {
		t.Fatalf("an unknown name is refused:\n%s", screen(m))
	}
}

func TestChatStartsInTheSavedTheme(t *testing.T) {
	m := newChatTest(0)
	m.cfg = &themeSettings{theme: "frost"}
	m = run(m, m.loadTheme())
	if m.th.Name != "frost" {
		t.Fatalf("theme %s", m.th.Name)
	}
}
