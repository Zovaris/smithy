//go:build harness

package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
)

func askScene() (chatView, *[]string) {
	var got []string
	m := newChatScene()
	m.decide = func(id, answer string) tea.Cmd {
		got = append(got, id+"="+answer)
		return func() tea.Msg { return chatDecidedMsg{id: id} }
	}
	return m, &got
}

func TestAskCardOpensOnEnterAndAnswersByNumber(t *testing.T) {
	m, got := askScene()
	if strings.Contains(screen(m), "Questions") {
		t.Fatal("card open before asked")
	}
	m = chatStep(m, ckey("enter"))
	out := screen(m)
	for _, s := range []string{"Questions", "Merge #142 into release/v0.17?", "merge", "later", "Type your own answer"} {
		if !strings.Contains(out, s) {
			t.Fatalf("card missing %q:\n%s", s, out)
		}
	}
	n, cmd := m.Update(ckey("2"))
	m = n.(chatView)
	if cmd == nil || len(*got) != 1 || (*got)[0] != "d1=later" {
		t.Fatalf("decide %v", *got)
	}
	m = chatStep(m, cmd())
	if m.overlay != overlayNone || len(m.pending) != 0 {
		t.Fatal("card stays after the answer")
	}
}

func TestAskCardArrowsAndEnter(t *testing.T) {
	m, got := askScene()
	m = chatStep(m, ckey("enter"))
	m = chatStep(m, ckey("down"))
	chatStep(m, ckey("enter"))
	if len(*got) != 1 || (*got)[0] != "d1=later" {
		t.Fatalf("decide %v", *got)
	}
}

func TestAskCardFreeTextOther(t *testing.T) {
	m, got := askScene()
	m = chatStep(m, ckey("enter"))
	m = typeIn(m, "wait for CI 3")
	if !strings.Contains(screen(m), "wait for CI 3") || len(*got) != 0 {
		t.Fatalf("typed text not in other:\n%s %v", screen(m), *got)
	}
	chatStep(m, ckey("enter"))
	if len(*got) != 1 || (*got)[0] != "d1=wait for CI 3" {
		t.Fatalf("decide %v", *got)
	}
}

func TestAskCardMultiSelect(t *testing.T) {
	m, got := askScene()
	m.setPending([]api.Decision{{ID: "d9", Question: "Which checks?", Options: []string{"lint", "test", "e2e"}, Multi: true}})
	m = chatStep(m, ckey("enter"))
	m = chatStep(m, ckey("1"))
	m = chatStep(m, ckey("down"))
	m = chatStep(m, ckey("down"))
	m = chatStep(m, ckey(" "))
	if len(*got) != 0 {
		t.Fatalf("multi answered on a pick: %v", *got)
	}
	m = chatStep(m, ckey("enter"))
	if len(*got) != 1 || (*got)[0] != "d9=lint, e2e" {
		t.Fatalf("decide %v", *got)
	}
}

func TestAskArrivingOpensCardOnlyWhenNotTyping(t *testing.T) {
	m, _ := askScene()
	m.setPending(nil)
	m = chatStep(m, ev(30, daemon.KindAsked, api.Decision{ID: "d2", Question: "Approve secret POST?", Options: []string{"approve", "deny"}}))
	if m.overlay != overlayAsk {
		t.Fatal("an ask on an idle composer does not open the card")
	}
	m = chatStep(m, ckey("esc"))
	if m.overlay != overlayNone || len(m.pending) != 1 {
		t.Fatal("esc must close the card and keep the question")
	}
	m, _ = askScene()
	m.setPending(nil)
	m = typeIn(m, "half a messa")
	m = chatStep(m, ev(31, daemon.KindAsked, api.Decision{ID: "d3", Question: "Ship?", Options: []string{"yes"}}))
	m = typeIn(m, "ge")
	if m.overlay == overlayAsk || m.input.Value() != "half a message" {
		t.Fatalf("typed text eaten: overlay %v input %q", m.overlay, m.input.Value())
	}
	if !strings.Contains(strings.Split(screen(m), "\n")[0], "waiting on you") {
		t.Fatalf("header does not say waiting on you:\n%s", screen(m))
	}
}

func TestAskCardShowsOtherOnMultiSelect(t *testing.T) {
	m, _ := askScene()
	m.setPending([]api.Decision{{ID: "d9", Question: "Which checks?", Options: []string{"lint", "unit tests", "e2e", "screenshots"}, Multi: true}})
	m.width, m.height = 140, 40
	m = chatStep(m, ckey("enter"))
	if !strings.Contains(screen(m), "Type your own answer") {
		t.Fatalf("Other row cut off:\n%s", screen(m))
	}
}
