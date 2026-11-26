//go:build harness

package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
