//go:build harness

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
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

func TestOptionLetterAloneThenEnterAnswers(t *testing.T) {
	var got []string
	m := newChatScene()
	m.decide = func(id, answer string) tea.Cmd { got = append(got, id+"="+answer); return nil }
	m = typeIn(m, "m")
	if len(got) != 0 || m.input.Value() != "m" {
		t.Fatalf("a letter goes to the composer first: %q %v", m.input.Value(), got)
	}
	m = chatStep(m, ckey("enter"))
	if len(got) != 1 || got[0] != "d1=merge" || m.input.Value() != "" {
		t.Fatalf("letter then enter answers: %v", got)
	}
}

func TestSkillsModalFitsTheScreenWithManySkills(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 140, 35
	s := &daemon.Setup{Paths: map[string]string{}}
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
	if foot < 0 || bottom < 0 {
		t.Fatalf("footer or bottom border cut:\n%s", screen(m))
	}
	for range 45 {
		m = chatStep(m, ckey("down"))
	}
	if !strings.Contains(screen(m), "skill-39") || !strings.Contains(screen(m), "install/update") {
		t.Fatalf("the list scrolls to its end inside the box:\n%s", screen(m))
	}
}

func TestPendingDecisionShowsOnce(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 100, 24
	m = chatStep(m, evNow(1, daemon.KindAsked, daemon.Decision{ID: "d1", Question: "Ship it now?", Options: []string{"yes", "no"}}))
	if n := strings.Count(screen(m), "Ship it now?"); n != 1 {
		t.Fatalf("the question shows %d times:\n%s", n, screen(m))
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

// evNow is ev stamped now: an event that happens while the chat is open, not one replayed.

func evNow(seq int64, kind string, data any) daemon.ClientMsg {
	msg := ev(seq, kind, data)
	msg.Event.Time = time.Now()
	return msg
}
