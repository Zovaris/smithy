//go:build harness

package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSelectModeReleasesTheMouse(t *testing.T) {
	m := newChatTest(3)
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse on by default")
	}
	m = cpress(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.View().MouseMode != tea.MouseModeNone || !strings.Contains(screen(m), "select") {
		t.Fatalf("ctrl+s hands the mouse to the terminal:\n%s", screen(m))
	}
	m = cpress(m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("ctrl+s again takes it back")
	}
}
