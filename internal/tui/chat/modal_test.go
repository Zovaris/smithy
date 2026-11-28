//go:build harness

package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// footCol is the column where the modal's key row starts, or -1 without one.
func footCol(m chatView) int {
	for _, l := range strings.Split(screen(m), "\n") {
		if i := strings.Index(l, "↑↓"); i >= 0 {
			return len([]rune(l[:i]))
		}
	}
	return -1
}

func TestEveryPickerIsTheCenteredModal(t *testing.T) {
	f := firstRunSetup()
	f.s.Chosen = true
	m := setupScene(f)
	bw, _ := m.boxSize(m.width, m.height)
	want := (m.width-bw)/2 + 2 // the padding
	palette := sendKey(m, ckey("ctrl+k"))
	if got := footCol(palette); got != want {
		t.Fatalf("palette key row at %d, want %d", got, want)
	}
	cases := map[string]chatView{
		"agent picker": sendKey(m, tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}),
		"skills":       sendKey(typeIn(m, "/skills"), ckey("enter")),
		"agents":       sendKey(m, ckey("tab")),
	}
	_, mm := modelsScene()
	m2, cmd := mm.modelCommand("")
	cases["model"] = run(m2.(chatView), cmd)
	for name, v := range cases {
		if got := footCol(v); got != want {
			t.Errorf("%s: key row at %d, want %d like the palette:\n%s", name, got, want, screen(v))
		}
	}
}
