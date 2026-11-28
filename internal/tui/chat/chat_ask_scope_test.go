//go:build harness

package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
)

func TestAnsweredAskStaysInItsSession(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 120, 30
	started(&m, "s-1", "one")
	started(&m, "s-2", "two")
	m.open("s-1")
	ask := func(id, session, q string) {
		b, _ := json.Marshal(api.Decision{ID: id, Question: q, Options: []string{"yes", "stop"}, Session: session})
		m.event(daemon.KindAsked, session, time.Now(), b)
		a, _ := json.Marshal(api.Outcome{ID: id, Answer: "stop"})
		m.event(daemon.KindAnswered, "", time.Now(), a)
	}
	ask("ask:k1", "s-2", "Raise the budget?")
	ask("decide:b/i/k", "", "Warden question?")
	m.overlay = overlayNone
	if out := screen(m); strings.Contains(out, "Raise the budget") || strings.Contains(out, "Warden question") {
		t.Fatalf("another session's or a batch's ask shows here:\n%s", out)
	}
	m.newSession()
	if out := screen(m); strings.Contains(out, "Raise the budget") || strings.Contains(out, "Warden question") {
		t.Fatalf("a new session shows old asks:\n%s", out)
	}
	m.open("s-2")
	if out := screen(m); !strings.Contains(out, "Raise the budget") {
		t.Fatalf("the asking session keeps its answered ask:\n%s", out)
	}
}
