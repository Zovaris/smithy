package acp

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// fakeAgent plays an ACP agent on the other end of the pipes: it answers initialize and
// session/new, streams updates, asks one permission and ends the prompt turn.
func fakeAgent(t *testing.T, in io.Reader, out io.Writer, got chan<- map[string]any) {
	t.Helper()
	r := bufio.NewScanner(in)
	r.Buffer(make([]byte, 1<<20), 1<<20)
	send := func(v any) {
		b, _ := json.Marshal(v)
		out.Write(append(b, '\n'))
	}
	read := func() map[string]any {
		if !r.Scan() {
			return nil
		}
		var m map[string]any
		json.Unmarshal(r.Bytes(), &m)
		return m
	}
	update := func(u map[string]any) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s1", "update": u}})
	}
	for {
		m := read()
		if m == nil {
			return
		}
		switch m["method"] {
		case "initialize":
			got <- m
			send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{"protocolVersion": 1}})
		case "session/new":
			got <- m
			send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{"sessionId": "s1"}})
		case "session/prompt":
			got <- m
			update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "Hello "}})
			update(map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]any{"type": "text", "text": "secret thoughts"}})
			update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Run go test", "kind": "execute", "status": "pending"})
			send(map[string]any{"jsonrpc": "2.0", "id": 99, "method": "fs/read_text_file", "params": map[string]any{"path": "/etc/passwd"}})
			if e := read(); e["error"] == nil {
				t.Errorf("unsupported request answered without error: %v", e)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": 100, "method": "session/request_permission", "params": map[string]any{
				"sessionId": "s1",
				"toolCall":  map[string]any{"toolCallId": "t1", "title": "Run go test", "rawInput": map[string]any{"command": "go test ./...", "file_path": "/work/a.go"}, "locations": []any{map[string]any{"path": "/work/b.go"}}},
				"options": []any{
					map[string]any{"optionId": "yes", "name": "Allow", "kind": "allow_once"},
					map[string]any{"optionId": "no", "name": "Reject", "kind": "reject_once"},
				},
			}})
			got <- read()
			update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed"})
			update(map[string]any{"sessionUpdate": "usage_update", "used": 10, "size": 100, "cost": map[string]any{"amount": 0.25, "currency": "USD"}})
			update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "world"}})
			send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{
				"stopReason": "end_turn",
				"usage":      map[string]any{"inputTokens": 120, "outputTokens": 30, "cachedReadTokens": 7, "cachedWriteTokens": 3, "totalTokens": 160},
			}})
			return
		}
	}
}

type sink struct {
	text  strings.Builder
	tools []Tool
	perms []Permission
	allow bool
}

func (s *sink) Text(t string) { s.text.WriteString(t) }
func (s *sink) Tool(t Tool)   { s.tools = append(s.tools, t) }
func (s *sink) Permit(p Permission) string {
	s.perms = append(s.perms, p)
	return Choose(p.Options, s.allow)
}

func run(t *testing.T, allow bool) (Result, *sink, []map[string]any) {
	t.Helper()
	toAgentR, toAgentW := io.Pipe()
	fromAgentR, fromAgentW := io.Pipe()
	got := make(chan map[string]any, 10)
	go func() {
		fakeAgent(t, toAgentR, fromAgentW, got)
		fromAgentW.Close()
	}()
	s := &sink{allow: allow}
	res, err := Run(fromAgentR, toAgentW, Session{Cwd: "/work", Prompt: "do it"}, s)
	if err != nil {
		t.Fatal(err)
	}
	close(got)
	var msgs []map[string]any
	for m := range got {
		msgs = append(msgs, m)
	}
	return res, s, msgs
}

func TestRunMapsUpdatesToTextToolsAndUsage(t *testing.T) {
	res, s, msgs := run(t, true)
	if s.text.String() != "Hello world" {
		t.Fatalf("text = %q (thoughts must not reach the log)", s.text.String())
	}
	if res.StopReason != "end_turn" {
		t.Fatalf("stop = %q", res.StopReason)
	}
	u := res.Usage
	if u.Input != 120 || u.Output != 30 || u.CacheRead != 7 || u.CacheWrite != 3 || u.CostUSD == nil || *u.CostUSD != 0.25 {
		t.Fatalf("usage = %+v", u)
	}
	if !res.HasUsage {
		t.Fatal("HasUsage false")
	}
	if len(s.tools) != 2 || s.tools[0].Title != "Run go test" || s.tools[0].Status != "pending" || s.tools[1].Status != "completed" || s.tools[1].Title != "Run go test" {
		t.Fatalf("tools = %+v", s.tools)
	}
	if len(msgs) != 4 {
		t.Fatalf("agent saw %d messages", len(msgs))
	}
	if p := msgs[0]["params"].(map[string]any); p["protocolVersion"].(float64) != 1 {
		t.Fatalf("initialize params = %v", p)
	}
	if p := msgs[1]["params"].(map[string]any); p["cwd"] != "/work" {
		t.Fatalf("session/new params = %v", p)
	}
	prompt := msgs[2]["params"].(map[string]any)["prompt"].([]any)[0].(map[string]any)
	if prompt["text"] != "do it" {
		t.Fatalf("prompt = %v", prompt)
	}
	outcome := msgs[3]["result"].(map[string]any)["outcome"].(map[string]any)
	if outcome["outcome"] != "selected" || outcome["optionId"] != "yes" {
		t.Fatalf("permission answer = %v", outcome)
	}
	if len(s.perms) != 1 || s.perms[0].Title != "Run go test" || s.perms[0].Kind != "execute" || s.perms[0].Command != "go test ./..." || strings.Join(s.perms[0].Paths, ",") != "/work/b.go,/work/a.go" {
		t.Fatalf("perms = %+v", s.perms)
	}
}

func TestRunRejectsPermissionWhenNotAllowed(t *testing.T) {
	_, _, msgs := run(t, false)
	outcome := msgs[3]["result"].(map[string]any)["outcome"].(map[string]any)
	if outcome["optionId"] != "no" {
		t.Fatalf("permission answer = %v", outcome)
	}
}

func TestChoose(t *testing.T) {
	opts := []Option{{ID: "a", Kind: "allow_always"}, {ID: "b", Kind: "allow_once"}, {ID: "c", Kind: "reject_always"}, {ID: "d", Kind: "reject_once"}}
	if Choose(opts, true) != "b" || Choose(opts, false) != "d" {
		t.Fatal("prefers the once options")
	}
	if Choose(opts[:1], false) != "" {
		t.Fatal("no reject option: cancel")
	}
}

func TestRunFailsWhenAgentExits(t *testing.T) {
	toAgentR, toAgentW := io.Pipe()
	go io.Copy(io.Discard, toAgentR)
	_, err := Run(strings.NewReader(""), toAgentW, Session{Cwd: "/w", Prompt: "p"}, &sink{})
	if err == nil {
		t.Fatal("no error when the agent closed its output")
	}
}

func TestRunReportsAgentError(t *testing.T) {
	toAgentR, toAgentW := io.Pipe()
	go io.Copy(io.Discard, toAgentR)
	out := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"auth required"}}` + "\n"
	_, err := Run(strings.NewReader(out), toAgentW, Session{Cwd: "/w", Prompt: "p"}, &sink{})
	if err == nil || !strings.Contains(err.Error(), "auth required") {
		t.Fatalf("err = %v", err)
	}
}
