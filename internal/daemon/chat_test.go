//go:build harness

package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/connect"
)

type chatRec struct{ rec }

func (c *chatRec) StartChat(_ context.Context, in connect.Inbound, _ string) (string, error) {
	return "chat:" + in.Ref, nil
}

func TestChatSessionAnswerGoesBackToItsThread(t *testing.T) {
	sv, _ := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	c := &chatRec{}
	h := &connect.Hub{Conns: []connect.Connector{c}, File: filepath.Join(t.TempDir(), "c.json"),
		Start: func(text, by string) (string, error) { return "s-1", nil }}
	ctx := context.Background()
	sv.NotesOnce(ctx, h)
	h.Handle(ctx, c, connect.Inbound{Chat: true, Ref: "m1", Text: "hello", By: "5"})
	sv.Append(KindSessionText, "s-1", map[string]string{"text": "Hi, "})
	sv.Append(KindSessionText, "s-1", map[string]string{"text": "done."})
	sv.Append(KindSessionText, "s-9", map[string]string{"text": "not a chat session"})
	sv.NotesOnce(ctx, h)
	if len(c.sent) != 0 {
		t.Fatalf("sent before the turn ended: %v", c.sent)
	}
	sv.Append(KindSessionStatus, "s-1", map[string]string{"status": StatusDone})
	sv.Append(KindSessionStatus, "s-9", map[string]string{"status": StatusDone})
	sv.NotesOnce(ctx, h)
	if strings.Join(c.sent, ",") != connect.KindChat {
		t.Fatalf("sent %v", c.sent)
	}
}

func TestAskToolTakesAnAnswerInOwnWords(t *testing.T) {
	sv, _ := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Ask("ask:k1", "Which db?", []string{"pg", "sqlite"})
	if o, err := sv.Answer("ask:k1", "neither, use files", "slack:U1"); err != nil || o.Answer != "neither, use files" {
		t.Fatalf("%+v %v", o, err)
	}
	sv.Ask("decide:b/v1/k", "Pick?", []string{"a", "b"})
	if _, err := sv.Answer("decide:b/v1/k", "c", "web"); err == nil {
		t.Fatal("other decisions still take only their options")
	}
	if !FreeAnswer("ask:k1") || FreeAnswer("perm:s/1") {
		t.Fatal("FreeAnswer")
	}
}

func TestConnectTestSendsThroughFreshConnectors(t *testing.T) {
	sv, c := settingsClient(t)
	r := &rec{}
	sv.Connectors = func() ([]connect.Connector, error) { return []connect.Connector{r}, nil }
	res, err := c.ConnectTest(context.Background())
	if err != nil || strings.Join(res.Results, ";") != "rec notifications: sent" || len(r.sent) != 1 {
		t.Fatalf("%+v %v %v", res, err, r.sent)
	}
	sv.Connectors = func() ([]connect.Connector, error) { return nil, nil }
	if res, err := c.ConnectTest(context.Background()); err != nil || res.Error == "" {
		t.Fatalf("none set up must say so: %+v %v", res, err)
	}
}
