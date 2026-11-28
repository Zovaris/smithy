package connect

import (
	"context"
	"testing"
)

func TestTelegramChatChannel(t *testing.T) {
	api := &botAPI{updates: `[
{"update_id":1,"message":{"message_id":3,"chat":{"id":-100600},"from":{"id":5},"text":"new task"}},
{"update_id":2,"message":{"message_id":4,"message_thread_id":12,"is_topic_message":true,"chat":{"id":-100600},"from":{"id":5},"text":"more"}},
{"update_id":3,"message":{"message_id":5,"chat":{"id":-100600},"from":{"id":6},"text":"stranger"}},
{"update_id":4,"message":{"message_id":6,"chat":{"id":-100500},"from":{"id":5},"text":"my words","reply_to_message":{"message_id":9}}},
{"update_id":5,"message":{"message_id":7,"chat":{"id":-100777},"from":{"id":5},"text":"other chat"}}]`}
	c := tg(t, api)
	c.ChatID = "-100600"
	c.Users = map[string]bool{"5": true}
	ins, err := c.Receive(context.Background())
	if err != nil || len(ins) != 3 {
		t.Fatalf("%v %+v", err, ins)
	}
	if a := ins[0]; !a.Chat || a.Thread != "" || a.Ref != "chat:3" {
		t.Fatalf("top-level %+v", a)
	}
	if b := ins[1]; !b.Chat || b.Thread != "chat:12" {
		t.Fatalf("topic %+v", b)
	}
	if r := ins[2]; r.Chat || r.ReplyTo != "9" {
		t.Fatalf("reply %+v", r)
	}
	th, err := c.StartChat(context.Background(), ins[0], "new task")
	if err != nil || th != "chat:77" || api.forms[len(api.forms)-1]["chat_id"] != "-100600" {
		t.Fatalf("%v %q %v", err, th, api.forms)
	}
	ref, err := c.Send(context.Background(), th, Note{Kind: KindChat, Text: "answer"}, nil)
	f := api.forms[len(api.forms)-1]
	if err != nil || ref != "chat:9" || f["chat_id"] != "-100600" || f["message_thread_id"] != "77" {
		t.Fatalf("%v %q %v", err, ref, f)
	}
	if _, err := c.Send(context.Background(), ChatRoot, Note{Title: "t"}, nil); err != nil || api.forms[len(api.forms)-1]["chat_id"] != "-100600" || api.forms[len(api.forms)-1]["message_thread_id"] != "" {
		t.Fatalf("root %v %v", err, api.forms[len(api.forms)-1])
	}
	if err := c.Update(context.Background(), "chat:9", Note{Title: "x"}); err != nil || api.forms[len(api.forms)-1]["chat_id"] != "-100600" {
		t.Fatal("update in chat")
	}
	if !c.HasChat() {
		t.Fatal("HasChat")
	}
}

func TestTelegramSameChatForBothHasNoPrefix(t *testing.T) {
	api := &botAPI{updates: `[{"update_id":1,"message":{"message_id":3,"chat":{"id":-100500},"from":{"id":5},"text":"hi"}}]`}
	c := tg(t, api)
	c.ChatID = "-100500"
	c.Users = map[string]bool{"5": true}
	ins, _ := c.Receive(context.Background())
	if len(ins) != 1 || !ins[0].Chat || ins[0].Ref != "3" {
		t.Fatalf("%+v", ins)
	}
}
