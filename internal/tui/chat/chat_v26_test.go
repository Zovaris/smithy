//go:build harness

package chat

import (
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

func TestDoctorOpensGroupedModalNotTranscript(t *testing.T) {
	f := &fakeSettings{doctor: []string{
		"warden doctor",
		"  ok    git found",
		"  warn  sandbox off",
		"        fix: smithy config set auto.sandbox native",
		"  FAIL  gh missing",
		"        fix: brew install gh",
	}}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = f
	m = cpress(typeIn(m, "/doctor"), ckey("enter"))
	if m.overlay != overlayInfo || m.info.title != "Doctor" {
		t.Fatalf("doctor must open the shared modal, overlay %v title %q", m.overlay, m.info.title)
	}
	for _, mm := range m.msgs {
		if strings.Contains(strings.Join(mm.lines, " ")+mm.text, "git found") || strings.Contains(mm.text, "doctor") {
			t.Fatalf("doctor dumped into the transcript: %q", mm.text)
		}
	}
	out := screen(m)
	for _, want := range []string{"NEEDS A FIX", "gh missing", "brew install gh", "WORTH A LOOK", "sandbox off", "smithy config set auto.sandbox native", "OK", "git found"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor modal lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "gh missing") > strings.Index(out, "sandbox off") || strings.Index(out, "sandbox off") > strings.Index(out, "git found") {
		t.Fatalf("fail, then warn, then ok:\n%s", out)
	}
}

func TestUsageNamesOnlyTheCommandAndPrintsOnce(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = &fakeSettings{}
	m = cpress(typeIn(m, "/set x"), ckey("enter"))
	m = cpress(typeIn(m, "/unset"), ckey("enter"))
	out := screen(m)
	if strings.Count(out, "usage: /set [<key> <value>]") != 1 || strings.Count(out, "usage: /unset <key>") != 1 || strings.Contains(out, "/doctor") {
		t.Fatalf("each usage once, naming only its command:\n%s", out)
	}
}

func TestHeaderShowsTendState(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = &fakeSettings{}
	m = run(m, m.loadTend())
	if h := strings.Join(m.header(160), ""); !strings.Contains(h, "tend fix · 3 PRs") {
		t.Fatalf("header lacks the tend state: %q", h)
	}
}

func TestTendModalCyclesModeWithLeftRight(t *testing.T) {
	f := &fakeSettings{}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = f
	m = cpress(typeIn(m, "/tend"), ckey("enter"))
	m = cpress(m, ckey("right"))
	if !strings.Contains(screen(m), "‹ off ›") || len(f.set) != 0 {
		t.Fatalf("right cycles fix → off without saving:\n%s", screen(m))
	}
	m = cpress(m, ckey("left"))
	m = cpress(m, ckey("left"))
	m = cpress(m, ckey("enter"))
	if len(f.set) != 1 || f.set[0] != [2]string{"tend.daemon", "watch"} {
		t.Fatalf("enter saves the mode: %v", f.set)
	}
}

func TestServeModalCyclesWithLeftRight(t *testing.T) {
	f := &fakeSettings{}
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m.cfg = f
	m = cpress(typeIn(m, "/serve"), ckey("enter"))
	m = cpress(m, ckey("right"))
	m = cpress(m, ckey("enter"))
	if len(f.serve) != 1 || f.serve[0] {
		t.Fatalf("serve on → right → off, enter saves: %v", f.serve)
	}
}

func TestThemeAndEffortCycleWithLeftRight(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 160, 48
	m = cpress(typeIn(m, "/theme"), ckey("enter"))
	s := m.themeSel
	m = cpress(m, ckey("right"))
	if m.themeSel == s {
		t.Fatal("right must move the theme")
	}
	m = cpress(m, ckey("left"))
	if m.themeSel != s {
		t.Fatal("left must move it back")
	}
	m = cpress(m, ckey("left"))
	if s == 0 && m.themeSel != len(kit.Themes)-1 {
		t.Fatal("left on the first theme wraps")
	}
	m.overlay, m.setupStep, m.effortSel = overlayPicker, stepEffort, 0
	m = cpress(m, ckey("right"))
	if m.effortSel != 1 {
		t.Fatal("right must move the effort")
	}
}
