package connect

import (
	"context"
	"strings"
	"testing"
)

type chatFake struct {
	fake
	started []string
}

func (f *chatFake) StartChat(_ context.Context, in Inbound, title string) (string, error) {
	f.started = append(f.started, in.Ref+"|"+title)
	return "chat:t-" + in.Ref, nil
}
func (f *chatFake) HasChat() bool { return true }

func TestNewChatMessageStartsASessionInItsOwnThread(t *testing.T) {
	c := &chatFake{fake: fake{name: "a"}}
	h := hub(t, c)
	var got []string
	h.Start = func(text, by string) (string, error) { got = append(got, text+"|"+by); return "s-1", nil }
	var input []string
	h.Input = func(s, text, by string) error { input = append(input, s+"|"+text); return nil }
	ctx := context.Background()
	h.Handle(ctx, c, Inbound{Chat: true, Ref: "m9", Text: "fix the login bug\nmore", By: "u1"})
	if len(got) != 1 || got[0] != "fix the login bug\nmore|a:u1" || len(c.started) != 1 || c.started[0] != "m9|fix the login bug" {
		t.Fatalf("start %v thread %v", got, c.started)
	}
	h.Handle(ctx, c, Inbound{Chat: true, Thread: "chat:t-m9", Text: "and the logout one", By: "u1"})
	if len(input) != 1 || input[0] != "s-1|and the logout one" || len(got) != 1 {
		t.Fatalf("reply should continue the session: input %v start %v", input, got)
	}
	h.Reply(ctx, "s-1", "7", "done, both fixed")
	if len(c.sent) != 1 || !strings.HasPrefix(c.sent[0], "chat:t-m9|chat|") {
		t.Fatalf("answer not in the chat thread: %v", c.sent)
	}
	if !h.IsChat("s-1") || h.IsChat("s-2") {
		t.Fatal("IsChat")
	}
}

func TestThreadAlreadyGivenStartsWithoutMakingOne(t *testing.T) {
	c := &chatFake{fake: fake{name: "a"}}
	h := hub(t, c)
	h.Start = func(text, by string) (string, error) { return "s-2", nil }
	h.Handle(context.Background(), c, Inbound{Chat: true, Thread: "chat:1700.1", Text: "hi", By: "u"})
	if len(c.started) != 0 || !h.IsChat("s-2") {
		t.Fatalf("started %v", c.started)
	}
}

func TestNotificationChannelMessageStartsNothing(t *testing.T) {
	c := &chatFake{fake: fake{name: "a"}}
	h := hub(t, c)
	h.Start = func(text, by string) (string, error) { t.Fatal("started"); return "", nil }
	h.Handle(context.Background(), c, Inbound{Text: "hello", By: "u"})
}

func TestIdeaInChatGoesToIntake(t *testing.T) {
	c := &chatFake{fake: fake{name: "a"}}
	h := hub(t, c)
	var idea string
	h.Intake = func(i, by string) error { idea = i; return nil }
	h.Start = func(text, by string) (string, error) { t.Fatal("started"); return "", nil }
	h.Handle(context.Background(), c, Inbound{Chat: true, Ref: "m1", Text: "idea: dark mode", By: "u"})
	if idea != "dark mode" {
		t.Fatalf("idea %q", idea)
	}
}

func TestChatAnswerOnlyGoesToChatSessions(t *testing.T) {
	c := &chatFake{fake: fake{name: "a"}}
	h := hub(t, c)
	h.Reply(context.Background(), "other", "1", "hello")
	if len(c.sent) != 0 {
		t.Fatalf("sent %v", c.sent)
	}
}

func TestLongAnswerIsSplitIntoChunks(t *testing.T) {
	c := &chatFake{fake: fake{name: "a"}}
	h := hub(t, c)
	h.Start = func(text, by string) (string, error) { return "s", nil }
	ctx := context.Background()
	h.Handle(ctx, c, Inbound{Chat: true, Ref: "m", Text: "go", By: "u"})
	h.Reply(ctx, "s", "1", strings.Repeat("x", ChatChunk*2+10))
	if len(c.sent) != 3 {
		t.Fatalf("chunks %d", len(c.sent))
	}
}

func TestReplyToAskMessageAnswersInOwnWords(t *testing.T) {
	c := &chatFake{fake: fake{name: "a"}}
	h := hub(t, c)
	h.Free = func(id string) bool { return strings.HasPrefix(id, "ask:") }
	var ans []string
	h.Answer = func(id, a, by string) error { ans = append(ans, id+"="+a); return nil }
	ctx := context.Background()
	h.Notify(ctx, Note{Key: "asked|ask:k", Kind: "decision", Title: "Decision needed", Text: "which?", Decision: "ask:k", Options: []string{"a", "b"}})
	h.Notify(ctx, Note{Key: "asked|perm:x", Kind: "decision", Title: "Decision needed", Decision: "perm:x", Options: []string{"once", "deny"}})
	h.Handle(ctx, c, Inbound{ReplyTo: "m1", Text: "neither, use c", By: "u"})
	h.Handle(ctx, c, Inbound{ReplyTo: "m2", Text: "sure", By: "u"})
	if len(ans) != 1 || ans[0] != "ask:k=neither, use c" {
		t.Fatalf("answers %v", ans)
	}
	h.Notify(ctx, Note{Key: "asked|ask:j", Kind: "decision", Title: "Decision needed", Decision: "ask:j", Options: []string{"a"}})
	h.Handle(ctx, c, Inbound{Thread: "m3", Text: "thread reply", By: "u"})
	if len(ans) != 2 || ans[1] != "ask:j=thread reply" {
		t.Fatalf("thread reply under the ask: %v", ans)
	}
	if !strings.Contains(c.sent[0], "a=") {
		t.Fatalf("buttons missing: %v", c.sent)
	}
}

func TestTestSendsToBothChannels(t *testing.T) {
	c := &chatFake{fake: fake{name: "a"}}
	f := &fake{name: "b"}
	out := hub(t, c, f).Test(context.Background())
	if strings.Join(out, ";") != "a notifications: sent;a chat: sent;b notifications: sent" || c.sent[1][:5] != "chat:" {
		t.Fatalf("%v %v", out, c.sent)
	}
}
