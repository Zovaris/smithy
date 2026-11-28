//go:build harness

package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/api"
)

func TestSlashPopupHasNoAccentBarOnTop(t *testing.T) {
	m := framedChat("steel", 100)
	items := m.slashPopup("")
	for _, r := range m.popupRows(items, 60) {
		if strings.Contains(ansi.Strip(r), "▄") {
			t.Fatalf("no thick accent bar above the popup: %q", ansi.Strip(r))
		}
	}
}

func TestSlashPopupWrapsAround(t *testing.T) {
	m := typeIn(framedChat("steel", 100), "/")
	n := len(m.popup())
	if n < 3 {
		t.Fatalf("popup rows: %d", n)
	}
	m = sendKey(m, ckey("up"))
	if m.popSel != n-1 {
		t.Fatalf("up on the first row goes to the last: %d of %d", m.popSel, n)
	}
	m = sendKey(m, ckey("down"))
	if m.popSel != 0 {
		t.Fatalf("down on the last row goes to the first: %d", m.popSel)
	}
}

func configChat(rows ...api.SettingRow) chatView {
	m := newChatTest(0)
	m.width, m.height = 140, 40
	m.cfg = &fakeSettings{}
	m.overlay, m.pal = overlayConfig, palette{}
	m.palSet = rows
	return m
}

func TestEveryListWrapsAround(t *testing.T) {
	m := configChat()
	n := len(m.configRows())
	m = sendKey(m, ckey("up"))
	if m.pal.sel != n-1 {
		t.Fatalf("settings: up on the first row goes to the last: %d of %d", m.pal.sel, n)
	}
	m = sendKey(m, ckey("down"))
	if m.pal.sel != 0 {
		t.Fatalf("settings: down on the last goes to the first: %d", m.pal.sel)
	}
	p := newChatTest(0)
	p.overlay = overlayPalette
	if k := len(p.palItems()); k > 1 {
		p = sendKey(p, ckey("up"))
		if p.pal.sel != k-1 {
			t.Fatalf("palette: up on the first row goes to the last: %d of %d", p.pal.sel, k)
		}
	}
}

func TestModalsOpenAndCloseWithoutMotion(t *testing.T) {
	m := framedChat("steel", 100)
	m.still = false
	m = sendKey(typeIn(m, "/theme"), ckey("enter"))
	if m.animating() {
		t.Fatal("a modal opens at rest: no entrance frames")
	}
	at := screen(m)
	m = chatStep(m, chatFrameMsg{})
	if screen(m) != at {
		t.Fatal("the modal does not move after it opens")
	}
	m = sendKey(m, ckey("esc"))
	if m.animating() {
		t.Fatal("a modal closes at once")
	}
}

func TestDemoCommandsAreGone(t *testing.T) {
	for _, c := range []string{"ask", "answer", "welcome", "prompt"} {
		if isCommand(c) {
			t.Errorf("/%s is a demo or adds nothing; it is not a command", c)
		}
		n, _ := newChatScene().command("/" + c)
		m := n.(chatView)
		if m.overlay != overlayNone || !strings.Contains(screen(m), "unknown command /"+c) {
			t.Errorf("/%s still does something:\n%s", c, screen(m))
		}
	}
}
