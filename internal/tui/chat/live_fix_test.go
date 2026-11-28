//go:build harness

package chat

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/charmbracelet/x/ansi"
)

func TestTypingAMessageNeverAnswersTheDecision(t *testing.T) {
	var got []string
	m := newChatScene()
	m.decide = func(id, answer string) tea.Cmd { got = append(got, id+"="+answer); return nil }
	m = typeIn(m, "make it so")
	if v := m.input.Value(); v != "make it so" || len(got) != 0 {
		t.Fatalf("typing ate a letter: input %q, decide %v", v, got)
	}
	m = chatStep(m, ckey("enter"))
	if len(got) != 0 || len(m.pending) != 1 {
		t.Fatalf("a typed message answered the decision: %v", got)
	}
}

func TestSkillsModalFitsTheScreenWithManySkills(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 140, 35
	s := &api.Setup{Paths: map[string]string{}}
	for i := range 40 {
		s.Skills = append(s.Skills, fmt.Sprintf("skill-%02d", i))
	}
	m.setupSt, m.overlay = s, overlaySkills
	lines := strings.Split(screen(m), "\n")
	if len(lines) > m.height {
		t.Fatalf("screen is %d rows, over %d", len(lines), m.height)
	}
	foot, bottom := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "install/update") {
			foot = i
		}
		if strings.Contains(l, "+-") && i > foot && foot >= 0 && bottom < 0 {
			bottom = i
		}
	}
	if foot < 0 || foot >= len(lines)-1 && bottom < 0 {
		t.Fatalf("footer or bottom border cut:\n%s", screen(m))
	}
	m = chatStep(m, ckey("up"))
	if !strings.Contains(screen(m), "skill-39") || !strings.Contains(screen(m), "install/update") {
		t.Fatalf("the list scrolls to its end inside the box:\n%s", screen(m))
	}
}

func TestPendingDecisionShowsOnce(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 100, 24
	m = chatStep(m, evNow(1, daemon.KindAsked, api.Decision{ID: "d1", Question: "Ship it now?", Options: []string{"yes", "no"}}))
	if n := strings.Count(screen(m), "Ship it now?"); n != 1 {
		t.Fatalf("the question shows %d times:\n%s", n, screen(m))
	}
}

func TestHeaderSaysWaitingWhileADecisionWaits(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 100, 24
	m.setPending([]api.Decision{{ID: "d1", Question: "Ship?", Options: []string{"yes"}}})
	head := strings.Split(screen(m), "\n")[0]
	if !strings.Contains(head, "waiting") || strings.Contains(head, "idle") {
		t.Fatalf("header pill: %q", head)
	}
}

func TestRoutedEventStaysOffTheStatusLine(t *testing.T) {
	m := newChatTest(0)
	m = chatStep(m, evNow(1, daemon.KindRouted, map[string]string{"id": "d1"}))
	if strings.Contains(screen(m), "decision_routed") {
		t.Fatalf("raw event name leaks:\n%s", screen(m))
	}
}

func TestReplayedServeStartStaysOffTheStatusLine(t *testing.T) {
	m := newChatTest(0)
	line := map[string]string{"line": "serve started: pid 42, agent claude"}
	m = chatStep(m, ev(1, "log", line))
	if strings.Contains(screen(m), "serve started") {
		t.Fatalf("an old event from the replay shows as new:\n%s", screen(m))
	}
	m = chatStep(m, evNow(2, "log", map[string]string{"line": "serve started: pid 43, agent claude"}))
	if !strings.Contains(screen(m), "pid 43") {
		t.Fatalf("a fresh serve start shows:\n%s", screen(m))
	}
}

func TestStatusLineHasAGapAboveTheComposer(t *testing.T) {
	m := newChatTest(0)
	m.status("hello status")
	lines := strings.Split(screen(m), "\n")
	y := rowOf(t, m, "hello status")
	if strings.TrimSpace(lines[y+1]) != "" {
		t.Fatalf("no gap between the status line and the input:\n%s", screen(m))
	}
}

func TestWelcomeDefaultNotePrintedOnce(t *testing.T) {
	m := newChatTest(0)
	m.welcome = welcomeAgents
	m = chatStep(m, setupSavedMsg{note: "new sessions start with claude"})
	m.welcome = welcomeModel
	m = chatStep(m, setupSavedMsg{note: "new sessions start with claude"})
	m.closeWelcome()
	if n := strings.Count(screen(m), "new sessions start with claude"); n != 1 {
		t.Fatalf("note printed %d times:\n%s", n, screen(m))
	}
}

func TestKeysOpensAModalWithInChatCommands(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 100, 30
	m.cfg = &fakeSettings{}
	m = sendKey(typeIn(m, "/keys"), ckey("enter"))
	out := screen(m)
	if m.overlay != overlayInfo || !strings.Contains(out, "linear") || !strings.Contains(out, "/key rm linear") {
		t.Fatalf("/keys modal:\n%s", out)
	}
	if strings.Contains(out, "smithy tui") {
		t.Fatalf("/keys points at another screen:\n%s", out)
	}
	if strings.Contains(strings.Join(func() []string {
		var d []string
		for _, c := range slashCommands {
			d = append(d, c.desc)
		}
		return d
	}(), "\n"), "smithy tui") {
		t.Fatal("a command description points at smithy tui")
	}
}

func TestWelcomeIntroWraps(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 80, 30
	m.welcome, m.overlay = welcomeAgents, overlaySetup
	m.setupSt = &api.Setup{}
	out := screen(m)
	if !strings.Contains(out, "later.") {
		t.Fatalf("welcome intro cut:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "…") && strings.Contains(l, "ctrl+l") {
			t.Fatalf("intro truncated: %q", ansi.Strip(l))
		}
	}
}

func TestTypingNeverLosesALeadingShortcutLetter(t *testing.T) {
	for _, s := range []string{"verify the build", "? why does it fail"} {
		m := newChatTest(0)
		mode := m.mode
		m = typeIn(m, s)
		if m.input.Value() != s || m.mode != mode || m.overlay != overlayNone {
			t.Fatalf("%q: input %q mode %v overlay %v", s, m.input.Value(), m.mode, m.overlay)
		}
	}
}

func TestCtrlOCyclesTheViewAndLoneQuestionMarkOpensHelp(t *testing.T) {
	m := newChatTest(0)
	mode := m.mode
	m = chatStep(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.mode == mode {
		t.Fatal("ctrl+o does not cycle the view mode")
	}
	m = chatStep(typeIn(m, "?"), ckey("enter"))
	if m.overlay != overlayHelp {
		t.Fatal("? then enter does not open help")
	}
}

// evNow is ev stamped now: an event that happens while the chat is open, not one replayed.

func evNow(seq int64, kind string, data any) api.ClientMsg {
	msg := ev(seq, kind, data)
	msg.Event.Time = time.Now()
	return msg
}
