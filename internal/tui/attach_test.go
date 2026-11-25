//go:build harness

package tui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/store"
	"github.com/charmbracelet/colorprofile"
)

func ev(seq int64, kind string, data any) daemon.ClientMsg {
	b, _ := json.Marshal(data)
	return daemon.ClientMsg{Event: &store.Event{Seq: seq, Time: time.Unix(0, 0), Kind: kind, Data: b}}
}

func newAttachTest() attachView {
	m := newAttachView(nil, "http://127.0.0.1:7477", newTheme(true, colorprofile.Ascii, false))
	m.width = 100
	return m
}

func step(m attachView, msg tea.Msg) attachView {
	n, _ := m.Update(msg)
	return n.(attachView)
}

func TestAttachEventsAndDecisions(t *testing.T) {
	m := newAttachTest()
	m = step(m, daemon.ClientMsg{Connected: true})
	m = step(m, ev(1, "log", map[string]string{"line": "cycle started"}))
	m = step(m, ev(2, daemon.KindAsked, daemon.Decision{ID: "d1", Question: "merge PR 12?", Options: []string{"yes", "no"}}))
	m = step(m, ev(2, daemon.KindAsked, daemon.Decision{ID: "d1", Question: "merge PR 12?", Options: []string{"yes", "no"}}))
	if len(m.pending) != 1 || m.last != 2 {
		t.Fatalf("pending %d last %d", len(m.pending), m.last)
	}
	out := m.View().Content
	for _, want := range []string{"connected", "cycle started", "merge PR 12?", "[1] yes", "[2] no"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "cycle started") != 1 {
		t.Fatalf("duplicated event:\n%s", out)
	}
	m = step(m, ev(3, daemon.KindAnswered, daemon.Outcome{ID: "d1", Answer: "yes", By: "web"}))
	if len(m.pending) != 0 {
		t.Fatal("answered decision still pending")
	}
}

func TestAttachAnswerKeyAndOutcome(t *testing.T) {
	m := newAttachTest()
	var asked [2]string
	m.decide = func(id, answer string) (daemon.Outcome, error) {
		asked = [2]string{id, answer}
		return daemon.Outcome{ID: id, Answer: "no", By: "phone"}, nil
	}
	m = step(m, ev(1, daemon.KindAsked, daemon.Decision{ID: "d1", Question: "q", Options: []string{"yes", "no"}}))
	_, cmd := m.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	if cmd == nil {
		t.Fatal("key 1 sent nothing")
	}
	m = step(m, cmd())
	if asked != [2]string{"d1", "yes"} {
		t.Fatalf("asked %v", asked)
	}
	if !strings.Contains(m.View().Content, "already answered: no (phone)") {
		t.Fatalf("lost answer not shown:\n%s", m.View().Content)
	}
}

func TestAttachReconnecting(t *testing.T) {
	m := newAttachTest()
	m = step(m, daemon.ClientMsg{Connected: true})
	m = step(m, daemon.ClientMsg{Err: errors.New("connection refused")})
	if out := m.View().Content; !strings.Contains(out, "reconnecting") {
		t.Fatalf("no reconnecting state:\n%s", out)
	}
}
