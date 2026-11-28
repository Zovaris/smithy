//go:build harness

package chat

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/store"
)

func status(m *chatView, id, st string) {
	b, _ := json.Marshal(map[string]string{"status": st})
	m.event(daemon.KindSessionStatus, id, time.Now(), b)
}

func TestQueueHoldsMessagesUntilTheTurnEnds(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 120, 30
	f := &fakeSessions{}
	m.sess = f
	started(&m, "s-1", "one")
	m.open("s-1")
	status(&m, "s-1", daemon.StatusWorking)
	m = cpress(typeIn(m, "first"), ckey("enter"))
	m = cpress(typeIn(m, "second"), ckey("enter"))
	if len(f.sent) != 0 {
		t.Fatalf("sent during the turn: %v", f.sent)
	}
	for _, c := range m.msgs {
		if c.text == "first" {
			t.Fatal("a queued message is not in the transcript")
		}
	}
	out := screen(m)
	if !strings.Contains(out, "queued") || !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Fatalf("queue under the input:\n%s", out)
	}
	m = cpress(m, ckey("up"))
	if m.input.Value() != "first" || len(m.queues["s-1"]) != 1 {
		t.Fatalf("up takes the next queued one to edit: %q %v", m.input.Value(), m.queues)
	}
	m.input.SetValue("")
	m = cpress(typeIn(m, "first, edited"), ckey("enter"))
	if q := m.queues["s-1"]; len(q) != 2 || q[0] != "first, edited" {
		t.Fatalf("the edited message keeps its place: %v", q)
	}
	b, _ := json.Marshal(map[string]string{"status": daemon.StatusDone})
	m = cpress(m, api.ClientMsg{Event: &store.Event{Seq: 5, Kind: daemon.KindSessionStatus, Session: "s-1", Data: b}})
	if len(f.sent) != 1 || f.sent[0] != [2]string{"s-1", "first, edited"} {
		t.Fatalf("the turn's end sends the next one: %v", f.sent)
	}
	m = cpress(m, ckey("esc"))
	if len(f.stopped) != 1 {
		t.Fatalf("esc with an empty input interrupts the turn: %v", f.stopped)
	}
}
