//go:build harness

package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func splashChat() chatView {
	m := newChatView(nil, "http://127.0.0.1:7477", newTheme(true, colorprofile.TrueColor, true))
	m.splashOn = true
	return m
}

func TestSplashIsShortPrecomputedDitherAndAnvil(t *testing.T) {
	th := newTheme(true, colorprofile.TrueColor, true)
	frames := splashFrames(th, 120, 32)
	if d := time.Duration(len(frames)) * time.Second / fps; d < 600*time.Millisecond || d > 900*time.Millisecond {
		t.Fatalf("splash lasts %v, want 600–900ms", d)
	}
	seen := map[string]bool{}
	for i, f := range frames {
		rows := strings.Split(f, "\n")
		if len(rows) != 32 {
			t.Fatalf("frame %d is %d rows", i, len(rows))
		}
		for _, r := range rows {
			if ansi.StringWidth(r) > 120 {
				t.Fatalf("frame %d row too wide", i)
			}
		}
		seen[f] = true
	}
	if len(seen) < len(frames)-1 {
		t.Fatal("frames repeat: nothing moves")
	}
	if last := ansi.Strip(frames[len(frames)-1]); !strings.Contains(last, strings.TrimRight(wordB[0], " ")) || !strings.ContainsAny(last, "▀▄█") {
		t.Fatalf("last frame lacks the anvil and name:\n%s", last)
	}
}

func TestSplashPlaysOnOpenThenStops(t *testing.T) {
	m := splashChat()
	m = chatStep(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	if m.splash == nil {
		t.Fatal("no splash on open")
	}
	n := len(m.splash)
	for i := 0; i < n+2; i++ {
		var cmd tea.Cmd
		var next tea.Model
		next, cmd = m.Update(splashMsg{})
		m = next.(chatView)
		if m.splash == nil {
			if cmd != nil {
				t.Fatal("the splash keeps ticking after it ends")
			}
			break
		}
	}
	if m.splash != nil {
		t.Fatal("the splash never ends")
	}
	if !strings.Contains(screen(m), "smithy") || strings.Contains(screen(m), "▀▀▀▀▀▀▀▀▀▀") {
		t.Fatal("chat not shown after the splash")
	}
}

func TestAnyKeySkipsSplashWithoutTyping(t *testing.T) {
	m := splashChat()
	m = chatStep(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	m = chatStep(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.splash != nil || m.input.Value() != "" {
		t.Fatalf("key did not just skip: splash %v input %q", m.splash != nil, m.input.Value())
	}
}

func TestSplashSkippedWhenNarrowOrOff(t *testing.T) {
	m := splashChat()
	m = chatStep(m, tea.WindowSizeMsg{Width: 70, Height: 32})
	if m.splash != nil {
		t.Fatal("splash in a narrow terminal")
	}
	m = splashChat()
	m.splashOn = false
	if m = chatStep(m, tea.WindowSizeMsg{Width: 120, Height: 32}); m.splash != nil {
		t.Fatal("splash while off")
	}
	m = splashChat()
	m = chatStep(m, tea.WindowSizeMsg{Width: 120, Height: 32})
	m = chatStep(m, tea.WindowSizeMsg{Width: 130, Height: 32})
	m.splash = nil
	if m = chatStep(m, tea.WindowSizeMsg{Width: 140, Height: 40}); m.splash != nil {
		t.Fatal("splash replays on resize")
	}
}

func TestSplashWantedOnlyOnAColorTTYWithMotionAndSetting(t *testing.T) {
	for _, c := range []struct {
		tty               bool
		noColor, reduce   string
		setting, motionCf string
		want              bool
	}{
		{true, "", "", "true", "false", true},
		{false, "", "", "true", "false", false},
		{true, "1", "", "true", "false", false},
		{true, "", "1", "true", "false", false},
		{true, "", "", "false", "false", false},
		{true, "", "", "true", "true", false},
	} {
		if got := splashWanted(c.tty, c.noColor, c.reduce, c.setting, c.motionCf); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}
