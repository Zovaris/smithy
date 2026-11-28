package connect

import (
	"context"
	"encoding/json"
	"testing"
)

func dEv(t string, d string) dEvent { return dEvent{T: t, D: json.RawMessage(d)} }

func TestDiscordChatChannel(t *testing.T) {
	a := &restAPI{}
	d := dc(t, a)
	d.ChatChan = "600"
	d.guild = "42"
	in, ok := d.event(dEv("MESSAGE_CREATE", `{"id":"801","channel_id":"600","guild_id":"42","author":{"id":"5"},"content":"start this"}`))
	if !ok || !in.Chat || in.Thread != "" || in.Ref != "801" {
		t.Fatalf("top-level %+v", in)
	}
	if _, ok := d.event(dEv("MESSAGE_CREATE", `{"id":"802","channel_id":"600","guild_id":"42","author":{"id":"6"},"content":"stranger"}`)); ok {
		t.Fatal("stranger counted")
	}
	r, ok := d.event(dEv("MESSAGE_CREATE", `{"id":"803","channel_id":"500","guild_id":"42","author":{"id":"5"},"content":"mine","message_reference":{"message_id":"900","channel_id":"500"}}`))
	if !ok || r.ReplyTo != "500/900" || r.Chat {
		t.Fatalf("reply %+v", r)
	}
	th, err := d.StartChat(context.Background(), in, "start this")
	if err != nil || th != "700" || a.calls[len(a.calls)-1] != "POST /channels/600/messages/801/threads" {
		t.Fatalf("%q %v %v", th, err, a.calls)
	}
	if _, err := d.Send(context.Background(), ChatRoot, Note{Title: "t"}, nil); err != nil || a.calls[len(a.calls)-1] != "POST /channels/600/messages" {
		t.Fatalf("%v %v", err, a.calls)
	}
	if !d.HasChat() {
		t.Fatal("HasChat")
	}
}
