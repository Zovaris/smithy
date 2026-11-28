//go:build harness

package chat

import (
	"strings"
	"testing"
)

const refineReply = "Here it is.\n\n```refined\n# Goal\nShip the thing.\n```\n\nCopy it or start a new chat."

func TestRefinedPromptTakesTheLastRefinedBlock(t *testing.T) {
	if p, ok := refinedPrompt(refineReply); !ok || p != "# Goal\nShip the thing." {
		t.Fatalf("got %q %v", p, ok)
	}
	if _, ok := refinedPrompt("```go\nx\n```"); ok {
		t.Fatal("a plain code block is no refined prompt")
	}
}

func refineChat() (chatView, *fakeSessions) {
	f := &fakeSessions{}
	m := newChatTest(0)
	m.width, m.height = 120, 30
	m.sess, m.current = f, "s-0"
	m.push(chatMsg{who: "claude", text: refineReply})
	return m, f
}

func TestRefineStripOffersCopyAndNewSession(t *testing.T) {
	m, _ := refineChat()
	out := screen(m)
	for _, w := range []string{"refined prompt", "copy", "new session"} {
		if !strings.Contains(out, w) {
			t.Fatalf("strip lacks %q:\n%s", w, out)
		}
	}
}

func TestRefineNewStartsASessionWithThePrompt(t *testing.T) {
	m, f := refineChat()
	_ = cpress(typeIn(m, "n"), ckey("enter"))
	if len(f.started) != 1 || f.started[0] != "# Goal\nShip the thing." {
		t.Fatalf("started %q", f.started)
	}
}

func TestRefineCopyPutsThePromptOnTheClipboard(t *testing.T) {
	m, f := refineChat()
	n, cmd := typeIn(m, "c").Update(ckey("enter"))
	if cmd == nil || len(f.started)+len(f.sent) != 0 {
		t.Fatalf("copy must only set the clipboard: %v %v", f.started, f.sent)
	}
	if !strings.Contains(screen(n.(chatView)), "copied") {
		t.Fatal("no copied status")
	}
}

func TestRefineStripGoneAfterAnotherMessage(t *testing.T) {
	m, _ := refineChat()
	m.push(chatMsg{who: "you", text: "thanks"})
	if strings.Contains(screen(m), "refined prompt") {
		t.Fatal("strip must follow only the last agent reply")
	}
}
