//go:build harness

package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/daemon"
)

func busEv(t *testing.T, m *chatView, kind, id string, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if !m.sessionEvent(kind, id, b) {
		t.Fatalf("%s not handled", kind)
	}
}

func TestAgentsTreeAndBusMessages(t *testing.T) {
	m := newChatTest(0)
	m.width, m.height = 100, 32
	m.dir, m.root = "/w/p", "/w/p"
	busEv(t, &m, daemon.KindSessionStarted, "s-a", daemon.Session{ID: "s-a", Title: "parent work", Dir: "/w/p"})
	busEv(t, &m, daemon.KindSessionStarted, "s-z", daemon.Session{ID: "s-z", Title: "other top", Dir: "/w/p"})
	busEv(t, &m, daemon.KindSessionStarted, "s-b", daemon.Session{ID: "s-b", Title: "child work", Dir: "/w/p", Parent: "s-a"})
	var order []string
	for _, s := range m.listed() {
		order = append(order, s.id)
	}
	if strings.Join(order, ",") != "s-a,s-b,s-z" {
		t.Fatalf("a child follows its parent: %v", order)
	}
	if m.depth("s-b") != 1 || m.depth("s-a") != 0 {
		t.Fatal("depth")
	}
	busEv(t, &m, daemon.KindSessionMail, "s-a", daemon.Mail{From: "s-b", To: "s-a", Text: "done: PR 7"})
	busEv(t, &m, daemon.KindSessionMessage, "s-a", map[string]string{"role": "user", "by": "session:s-b", "text": "Message from session s-b (framed) …"})
	m.current = ""
	got := m.convs["s-a"]
	if len(got) != 1 || !strings.Contains(got[0].text, "s-b") || !strings.Contains(got[0].text, "done: PR 7") {
		t.Fatalf("the receiver shows the message once, not the framed prompt: %+v", got)
	}
	if s := m.convs["s-b"]; len(s) != 1 || !strings.Contains(s[0].text, "s-a") {
		t.Fatalf("the sender shows what it sent: %+v", s)
	}
	if v := strings.Join(m.agentsBox(make([]string, 40), 100, 40), "\n"); !strings.Contains(v, "└ child work") {
		t.Fatalf("the agents modal draws the tree:\n%s", v)
	}
}
