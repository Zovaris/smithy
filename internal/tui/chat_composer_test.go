//go:build harness

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestComposerDefaultsToThreeRows(t *testing.T) {
	m := newChatTest(0)
	if h := m.input.Height(); h != 3 {
		t.Fatalf("empty composer height %d, want 3", h)
	}
}

func TestComposerNewlineKeysBreakTheLine(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{
		{Code: tea.KeyEnter, Mod: tea.ModShift},
		{Code: tea.KeyEnter, Mod: tea.ModAlt},
		{Code: 'j', Mod: tea.ModCtrl},
	} {
		m := newChatTest(0)
		m = typeIn(m, "a")
		m = chatStep(m, k)
		m = typeIn(m, "b")
		if got := m.input.Value(); got != "a\nb" {
			t.Fatalf("%s: got %q", k.String(), got)
		}
	}
}

func TestComposerPasteKeepsNewlines(t *testing.T) {
	m := newChatTest(0)
	m = chatStep(m, tea.PasteMsg{Content: "one\ntwo\nthree"})
	if got := m.input.Value(); got != "one\ntwo\nthree" {
		t.Fatalf("got %q", got)
	}
}

func TestComposerGrowsToTenThenScrollsAndShrinksAfterSend(t *testing.T) {
	f := &fakeSessions{}
	m := newChatTest(0)
	m.sess = f
	m = chatStep(m, tea.PasteMsg{Content: strings.Repeat("line\n", 5) + "end"})
	if h := m.input.Height(); h != 6 {
		t.Fatalf("6 lines: height %d", h)
	}
	m = chatStep(m, tea.PasteMsg{Content: strings.Repeat("\nmore", 14)})
	if h := m.input.Height(); h != 10 {
		t.Fatalf("20 lines: height %d, want 10", h)
	}
	if n := strings.Count(m.input.Value(), "\n"); n != 19 {
		t.Fatalf("content clipped: %d newlines", n)
	}
	m = cpress(m, ckey("enter"))
	if m.input.Value() != "" || m.input.Height() != 3 {
		t.Fatalf("after send: %q height %d", m.input.Value(), m.input.Height())
	}
	if len(f.started) != 1 || !strings.Contains(f.started[0], "line\nline") {
		t.Fatalf("sent %q", f.started)
	}
}

func TestFooterShowsNewlineHint(t *testing.T) {
	m := newChatTest(0)
	if !strings.Contains(screen(m), "shift+enter newline") {
		t.Fatalf("no newline hint:\n%s", screen(m))
	}
}
