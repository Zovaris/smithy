//go:build harness

package chat

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func clearsScreen(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			if clearsScreen(c) {
				return true
			}
		}
	default:
		return fmt.Sprintf("%T", msg) == "tea.clearScreenMsg"
	}
	return false
}

// WezTerm's cmd+k clears the viewport and sends ctrl+l; the frame must be repainted whole, or
// only the changed rows come back and the composer lands on the top row.
func TestCtrlKeysRepaintTheWholeScreen(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{ckey("ctrl+k"), {Code: 'l', Mod: tea.ModCtrl}} {
		for _, ov := range []chatOverlay{overlayNone, overlayAgents, overlayPalette} {
			m := framedChat("steel", 100)
			m.overlay = ov
			_, cmd := m.Update(k)
			if !clearsScreen(cmd) {
				t.Fatalf("%s with overlay %d does not clear the screen", k.String(), ov)
			}
		}
	}
}

func TestTypingDoesNotRepaintTheWholeScreen(t *testing.T) {
	_, cmd := framedChat("steel", 100).Update(ckey("a"))
	if clearsScreen(cmd) {
		t.Fatal("a printable key clears the screen")
	}
}
