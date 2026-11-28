//go:build harness

package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/Zovaris/smithy/internal/tui/kit"
)

func framedChat(name string, w int) chatView {
	m := newChatTest(0)
	m.th = kit.NamedTheme(name, true, colorprofile.TrueColor, true)
	m.width, m.height = w, 24
	return m
}

func boxRows(t *testing.T, m chatView) []string {
	t.Helper()
	var rows []string
	for _, l := range strings.Split(screen(m), "\n") {
		if strings.Contains(l, "▄▄▄") || len(rows) > 0 && len(rows) < 40 && !strings.Contains(l, "▀▀▀") {
			rows = append(rows, l)
		}
		if strings.Contains(l, "▀▀▀") {
			break
		}
	}
	if len(rows) < 6 {
		t.Fatalf("no framed box on screen:\n%s", screen(m))
	}
	return rows
}

func TestModalHasAFrameTitleBarAndFooter(t *testing.T) {
	for _, name := range kit.Themes {
		m := framedChat(name, 100)
		m = sendKey(typeIn(m, "/theme"), ckey("enter"))
		rows := boxRows(t, m)
		if !strings.Contains(rows[0], "▄") {
			t.Fatalf("%s: a flat accent edge on top:\n%s", name, strings.Join(rows, "\n"))
		}
		title := strings.TrimSpace(rows[1])
		if !strings.Contains(title, "Theme") || !strings.Contains(title, "esc") {
			t.Fatalf("%s: title bar has the title left and esc right: %q", name, title)
		}
		if !strings.Contains(rows[len(rows)-1], "enter") {
			t.Fatalf("%s: key footer on the last row: %q", name, rows[len(rows)-1])
		}
		if strings.ContainsAny(strings.Join(rows, ""), "┌┐└┘│") {
			t.Fatalf("%s: no box-drawing frame:\n%s", name, strings.Join(rows, "\n"))
		}
	}
}

func TestModalMarksTheSelectedRowWithABarNotABlock(t *testing.T) {
	m := framedChat("steel", 100)
	m = sendKey(typeIn(m, "/theme"), ckey("enter"))
	m = sendKey(m, ckey("down"))
	var sel string
	for _, r := range boxRows(t, m) {
		if strings.Contains(r, "▌") {
			sel = r
		}
	}
	if !strings.Contains(sel, kit.Themes[1]) {
		t.Fatalf("the selected row carries the accent bar: %q", sel)
	}
	raw := m.View().Content
	if strings.Contains(raw, "\x1b[48;2;44;55;70m") {
		t.Fatal("the selected row is not painted with the border-tier block")
	}
}

func TestModalFitsANarrowTerminal(t *testing.T) {
	for _, w := range []int{30, 44} {
		m := framedChat("frost", w)
		m = sendKey(m, ckey("ctrl+k"))
		for _, l := range strings.Split(screen(m), "\n") {
			if ansi.StringWidth(l) > w {
				t.Fatalf("width %d: row overflows: %q", w, l)
			}
		}
		boxRows(t, m)
	}
}

func TestRepeatedLogLineShowsOnce(t *testing.T) {
	m := newChatTest(0)
	line := map[string]string{"line": "serve: poll: github: needs the GitHub CLI or GH_TOKEN"}
	m = chatStep(m, ev(1, "log", line))
	m = chatStep(m, ev(2, "log", line))
	m = chatStep(m, ev(3, "log", line))
	if len(m.noise) != 1 {
		t.Fatalf("a repeated log line is kept once, got %d", len(m.noise))
	}
	m.noise[0].at = time.Now().Add(-time.Minute)
	if strings.Contains(screen(m), "needs the GitHub CLI") {
		t.Fatalf("an old log line leaves the status area:\n%s", screen(m))
	}
}
