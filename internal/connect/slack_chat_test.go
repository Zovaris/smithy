package connect

import (
	"encoding/json"
	"testing"
)

func slackEnv(t *testing.T, s string) slackEnvelope {
	var e slackEnvelope
	if err := json.Unmarshal([]byte(s), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSlackChatChannelInbound(t *testing.T) {
	s := &Slack{Channel: "C111111", ChatChan: "C222222", Users: map[string]bool{"U1": true}}
	top := s.inbound(slackEnv(t, `{"type":"events_api","payload":{"event":{"type":"message","user":"U1","channel":"C222222","text":"build it","ts":"10.1"}}}`))
	if len(top) != 1 || !top[0].Chat || top[0].Thread != "chat:10.1" {
		t.Fatalf("top %+v", top)
	}
	rep := s.inbound(slackEnv(t, `{"type":"events_api","payload":{"event":{"type":"message","user":"U1","channel":"C222222","text":"more","ts":"11.1","thread_ts":"10.1"}}}`))
	if len(rep) != 1 || !rep[0].Chat || rep[0].Thread != "chat:10.1" {
		t.Fatalf("reply %+v", rep)
	}
	if x := s.inbound(slackEnv(t, `{"type":"events_api","payload":{"event":{"type":"message","user":"U9","channel":"C222222","text":"x","ts":"12.1"}}}`)); len(x) != 0 {
		t.Fatalf("stranger %+v", x)
	}
	if x := s.inbound(slackEnv(t, `{"type":"events_api","payload":{"event":{"type":"message","user":"U1","channel":"C111111","text":"x","ts":"12.1"}}}`)); len(x) != 0 {
		t.Fatalf("notify top-level %+v", x)
	}
	btn := s.inbound(slackEnv(t, `{"type":"interactive","payload":{"type":"block_actions","user":{"id":"U1"},"channel":{"id":"C222222"},"message":{"ts":"13.1"},"actions":[{"value":"ab:0"}]}}`))
	if len(btn) != 1 || btn[0].Ref != "chat:13.1" {
		t.Fatalf("button %+v", btn)
	}
	if ch, id := s.split("chat:10.1"); ch != "C222222" || id != "10.1" {
		t.Fatal(ch, id)
	}
	if ch, id := s.split(ChatRoot); ch != "C222222" || id != "" {
		t.Fatal(ch, id)
	}
	if !s.HasChat() {
		t.Fatal("HasChat")
	}
}
