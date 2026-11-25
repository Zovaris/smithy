package connect

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type fake struct {
	name    string
	threads map[string]string
	sent    []string
	updated []string
	failT   bool
	in      []Inbound
}

func (f *fake) Name() string { return f.name }
func (f *fake) Thread(_ context.Context, session, title string) (string, error) {
	if f.failT {
		return "", errors.New("not a forum")
	}
	if f.threads == nil {
		f.threads = map[string]string{}
	}
	f.threads[session] = "t-" + session
	return "t-" + session, nil
}
func (f *fake) Send(_ context.Context, thread string, n Note, buttons []Button) (string, error) {
	var bs []string
	for _, b := range buttons {
		bs = append(bs, b.Label+"="+b.Data)
	}
	f.sent = append(f.sent, thread+"|"+n.Kind+"|"+n.Title+"|"+strings.Join(bs, ","))
	return "m" + string(rune('0'+len(f.sent))), nil
}
func (f *fake) Update(_ context.Context, ref string, n Note) error {
	f.updated = append(f.updated, ref+"|"+n.Text)
	return nil
}
func (f *fake) Receive(context.Context) ([]Inbound, error) { in := f.in; f.in = nil; return in, nil }

func hub(t *testing.T, cs ...Connector) *Hub {
	return &Hub{Conns: cs, File: filepath.Join(t.TempDir(), "connect.json"),
		Kinds: func(name string) map[string]bool {
			if name == "quiet" {
				return map[string]bool{"blocked": true}
			}
			return map[string]bool{"blocked": true, "decision": true}
		}}
}

func TestNotifyOncePerConnectorAndKind(t *testing.T) {
	a, b := &fake{name: "a"}, &fake{name: "quiet"}
	h := hub(t, a, b)
	ctx := context.Background()
	n := Note{Key: "blocked|b/1", Kind: "blocked", Session: "b/1", Title: "Blocked"}
	h.Notify(ctx, n)
	h.Notify(ctx, n)
	h.Notify(ctx, Note{Key: "decision|x", Kind: "decision", Session: "b/1", Title: "Pick", Options: []string{"yes", "no"}, Decision: "decide:b/1"})
	if len(a.sent) != 2 || a.sent[0] != "t-b/1|blocked|Blocked|" {
		t.Fatalf("a got %q", a.sent)
	}
	if len(a.threads) != 1 {
		t.Fatalf("one thread per session, got %v", a.threads)
	}
	if len(b.sent) != 1 {
		t.Fatalf("quiet connector takes only its kinds, got %q", b.sent)
	}
	if bs := strings.Split(a.sent[1], "|")[3]; !strings.HasPrefix(bs, "yes=") || !strings.Contains(bs, ":0,no=") || !strings.HasSuffix(bs, ":1") {
		t.Fatalf("decision buttons: %q", a.sent[1])
	}
	h2 := hub(t, a)
	h2.File = h.File
	h2.Notify(ctx, n)
	if len(a.sent) != 2 {
		t.Fatal("sent state must survive a restart")
	}
}

func TestThreadFailureFallsBackToMainChat(t *testing.T) {
	a := &fake{name: "a", failT: true}
	h := hub(t, a)
	h.Notify(context.Background(), Note{Key: "k", Kind: "blocked", Session: "b/1", Title: "x"})
	if len(a.sent) != 1 || !strings.HasPrefix(a.sent[0], "|") {
		t.Fatalf("got %q", a.sent)
	}
}

func TestInboundRoutesButtonsAndReplies(t *testing.T) {
	a := &fake{name: "a"}
	h := hub(t, a)
	var answers, inputs []string
	h.Answer = func(id, ans, by string) error { answers = append(answers, id+"="+ans+"@"+by); return nil }
	h.Input = func(session, text, by string) error { inputs = append(inputs, session+":"+text+"@"+by); return nil }
	ctx := context.Background()
	h.Notify(ctx, Note{Key: "d", Kind: "decision", Session: "b/1", Title: "Pick", Options: []string{"yes", "no"}, Decision: "decide:b/1"})
	data := strings.Split(strings.Split(a.sent[0], "|")[3], ",")[1]
	data = strings.TrimPrefix(data, "no=")
	h.Handle(ctx, a, Inbound{Button: "bogus", By: "u1"})
	h.Handle(ctx, a, Inbound{Button: data, Ref: "m9", By: "u1"})
	h.Handle(ctx, a, Inbound{Button: data, Ref: "m1", By: "u1"})
	h.Handle(ctx, a, Inbound{Thread: "t-b/1", Text: "use redis", By: "u1"})
	h.Handle(ctx, a, Inbound{Thread: "unknown", Text: "lost", By: "u1"})
	if len(answers) != 1 || answers[0] != "decide:b/1=no@a:u1" {
		t.Fatalf("answers %q", answers)
	}
	if len(inputs) != 1 || inputs[0] != "b/1:use redis@a:u1" {
		t.Fatalf("inputs %q", inputs)
	}
}

func TestResolveUpdatesDecisionMessage(t *testing.T) {
	a := &fake{name: "a"}
	h := hub(t, a)
	ctx := context.Background()
	h.Notify(ctx, Note{Key: "d", Kind: "decision", Session: "b/1", Title: "Pick", Options: []string{"yes"}, Decision: "decide:b/1"})
	h.Resolve(ctx, "decide:b/1", "yes", "web")
	h.Resolve(ctx, "decide:other", "yes", "web")
	if len(a.updated) != 1 || !strings.Contains(a.updated[0], "yes") || !strings.Contains(a.updated[0], "web") {
		t.Fatalf("updated %q", a.updated)
	}
}

func TestIdeaOf(t *testing.T) {
	for in, want := range map[string]string{
		"idea: dark mode for the board": "dark mode for the board",
		"  /idea  export to CSV ":       "export to CSV",
		"IDEA:x":                        "x",
		"/idea@smithy_bot retry":        "retry",
		"idea:   ":                      "",
		"hello there":                   "",
		"an idea: not a prefix":         "",
		"/ideas list":                   "",
	} {
		if got := IdeaOf(in); got != want {
			t.Errorf("IdeaOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHandleMainChatIdeaGoesToIntake(t *testing.T) {
	a := &fake{name: "a"}
	h := hub(t, a)
	var got []string
	h.Intake = func(idea, by string) error { got = append(got, idea+"|"+by); return nil }
	h.Input = func(session, text, by string) error { t.Fatalf("main chat is not session input"); return nil }
	ctx := context.Background()
	h.Handle(ctx, a, Inbound{Text: "idea: weekly digest", By: "5"})
	h.Handle(ctx, a, Inbound{Text: "just chatting", By: "5"})
	h.Handle(ctx, a, Inbound{Thread: "t-x", Text: "idea: in a thread", By: "5"})
	if len(got) != 1 || got[0] != "weekly digest|a:5" {
		t.Fatalf("intake got %v", got)
	}
}
