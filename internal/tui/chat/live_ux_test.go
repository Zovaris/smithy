//go:build harness

package chat

import (
	"strings"
	"testing"
)

func TestModelSpaceCompletesTheAgentsModels(t *testing.T) {
	_, m := noSessionScene()
	m = typeIn(m, "/model")
	n, cmd := m.Update(ckey(" "))
	m = run(n.(chatView), cmd)
	var got []string
	for _, it := range m.popup() {
		got = append(got, it.fill)
	}
	if !strings.Contains(strings.Join(got, ","), "/model opencode/beam-free") {
		t.Fatalf("models %v", got)
	}
	m.input.SetValue("/model beam")
	if p := m.popup(); len(p) != 1 || p[0].fill != "/model opencode/beam-free" {
		t.Fatalf("filtered %v", p)
	}
}

func paletteScene(t *testing.T) (*fakeSettings, chatView) {
	t.Helper()
	fs, m := noSessionScene()
	n, cmd := m.Update(ckey("ctrl+k"))
	return fs, run(n.(chatView), cmd)
}

func TestPaletteFindsSettingsByKeyAndLabel(t *testing.T) {
	_, m := paletteScene(t)
	m = typeIn(m, "sett")
	if len(m.palItems()) < 3 {
		t.Fatalf("sett finds the settings rows: %v", m.palItems())
	}
	m.pal.query = "theme"
	got := m.palItems()
	if len(got) == 0 || got[0].label != "ui.theme" {
		t.Fatalf("a setting is found by its key: %+v", got)
	}
}

func TestPaletteSettingCyclesWithLeftRight(t *testing.T) {
	fs, m := paletteScene(t)
	m.pal.query = "chat.mode"
	before := m.mode
	m = chatStep(m, ckey("right"))
	if m.mode == before {
		t.Fatalf("right cycles chat.mode")
	}
	m = chatStep(m, ckey("left"))
	if m.mode != before {
		t.Fatalf("left cycles chat.mode back")
	}
	m.pal = palette{query: "ui.theme"}
	n, cmd := m.Update(ckey("right"))
	m = run(n.(chatView), cmd)
	if len(fs.set) != 1 || fs.set[0] != [2]string{"ui.theme", "frost"} {
		t.Fatalf("right saves the next option: %v", fs.set)
	}
	if !strings.Contains(screen(m), "frost") {
		t.Fatalf("the row shows the new value:\n%s", screen(m))
	}
	m.pal = palette{query: "auto.paused"}
	n, cmd = m.Update(ckey("left"))
	run(n.(chatView), cmd)
	if fs.set[len(fs.set)-1] != [2]string{"auto.paused", "true"} {
		t.Fatalf("a bool flips: %v", fs.set)
	}
}

func TestBareSetOpensTheSettingsList(t *testing.T) {
	_, m := noSessionScene()
	m = typeIn(m, "/set")
	n, cmd := m.Update(ckey("enter"))
	m = run(n.(chatView), cmd)
	if m.overlay != overlayPalette {
		t.Fatalf("one enter on /set opens the settings list, overlay %v", m.overlay)
	}
	for _, it := range m.palItems() {
		if it.group != "SETTINGS" {
			t.Fatalf("filtered to settings, got %+v", it)
		}
	}
}
