package agents

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
)

// fakeACP is the ACP agent the tests run (the test binary with SMITHY_TEST_FAKE_ACP=1): it streams
// an answer, asks one permission, reports usage, ends the turn and then waits on stdin as a real
// ACP server does, until smithy closes it.
func fakeACP() {
	in := bufio.NewScanner(os.Stdin)
	send := func(v any) {
		b, _ := json.Marshal(v)
		os.Stdout.Write(append(b, '\n'))
	}
	update := func(u map[string]any) {
		send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s", "update": u}})
	}
	os.Stderr.WriteString("fake agent starting\n")
	for in.Scan() {
		var m map[string]any
		json.Unmarshal(in.Bytes(), &m)
		switch m["method"] {
		case "initialize":
			send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{"protocolVersion": 1}})
		case "session/new":
			send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{"sessionId": "s"}})
		case "session/prompt":
			text := m["params"].(map[string]any)["prompt"].([]any)[0].(map[string]any)["text"].(string)
			update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t", "title": "Edit main.go", "kind": "edit", "status": "pending"})
			send(map[string]any{"jsonrpc": "2.0", "id": "p1", "method": "session/request_permission", "params": map[string]any{
				"sessionId": "s", "toolCall": map[string]any{"toolCallId": "t"},
				"options": []any{map[string]any{"optionId": "ok", "kind": "allow_once"}, map[string]any{"optionId": "nope", "kind": "reject_once"}},
			}})
			in.Scan()
			var ans map[string]any
			json.Unmarshal(in.Bytes(), &ans)
			picked := ans["result"].(map[string]any)["outcome"].(map[string]any)["optionId"]
			status := "completed"
			if picked != "ok" {
				status = "failed"
			}
			update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t", "status": status})
			update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "answer to: " + text}})
			send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{"stopReason": "end_turn", "usage": map[string]any{"inputTokens": 11, "outputTokens": 5}}})
		}
	}
}

func acpRun(t *testing.T, extra ...string) (string, string, int) {
	t.Helper()
	exe, _ := os.Executable()
	t.Setenv("SMITHY_HOME", t.TempDir())
	t.Setenv("SMITHY_TEST_FAKE_ACP", "1")
	t.Setenv("AGENT_PROTOCOL", "acp")
	t.Setenv("AGENT_CMD", "'"+exe+"'")
	dir := t.TempDir()
	p := filepath.Join(dir, "prompt.md")
	os.WriteFile(p, []byte("fix it\n"), 0o644)
	log := filepath.Join(dir, "run.log")
	rc := AgentMain(append(append([]string{"--timeout", "1"}, extra...), "--log", log, p))
	b, _ := os.ReadFile(log)
	return string(b), log, rc
}

func TestAgentDrivesAnACPAgent(t *testing.T) {
	old := addDecision
	addDecision = func(q, choice, why, ref string) error {
		t.Errorf("decision recorded for an allowed tool: %s", q)
		return nil
	}
	t.Cleanup(func() { addDecision = old })
	start := time.Now()
	out, log, rc := acpRun(t)
	if rc != 0 {
		t.Fatalf("rc = %d, log:\n%s", rc, out)
	}
	if !strings.Contains(out, "answer to: fix it") || !strings.Contains(out, "fake agent starting") {
		t.Fatalf("log = %q", out)
	}
	if time.Since(start) > 20*time.Second {
		t.Fatal("smithy waited on the ACP server after the turn ended")
	}
	if u, ok := usage.Read(filepath.Join(filepath.Dir(log), "run.log")); !ok || u.Input != 11 {
		t.Fatalf("sidecar = %+v, %v", u, ok)
	}
	es, _ := events.Read(time.Time{}, 100)
	kinds := map[string]events.Event{}
	for _, e := range es {
		kinds[e.Kind] = e
	}
	if e, ok := kinds["usage"]; !ok || e.Usage.Input != 11 || e.Usage.Output != 5 {
		t.Fatalf("usage event = %+v (events %+v)", e, es)
	}
	if e, ok := kinds["tool"]; !ok || e.Detail != "Edit main.go: completed" {
		t.Fatalf("tool event = %+v", e)
	}
	if e, ok := kinds["permission"]; !ok || !strings.Contains(e.Detail, "Edit main.go") || !strings.Contains(e.Detail, "allowed") {
		t.Fatalf("permission event = %+v", e)
	}
}

func TestAgentReadOnlyACPRejectsAndRecordsTheDecision(t *testing.T) {
	var got []string
	old := addDecision
	addDecision = func(q, choice, why, ref string) error { got = append(got, q+" -> "+choice+" ("+why+")"); return nil }
	t.Cleanup(func() { addDecision = old })
	out, _, rc := acpRun(t, "--readonly")
	if rc != 0 {
		t.Fatalf("rc = %d, log:\n%s", rc, out)
	}
	if len(got) != 1 || !strings.Contains(got[0], "Edit main.go") || !strings.Contains(got[0], "rejected") {
		t.Fatalf("decisions = %v", got)
	}
	es, _ := events.Read(time.Time{}, 100)
	for _, e := range es {
		if e.Kind == "tool" && e.Detail != "Edit main.go: failed" {
			t.Fatalf("tool event = %+v", e)
		}
	}
}

func TestACPProtocolErrorStopsTheChild(t *testing.T) {
	old := acpGrace
	acpGrace = 100 * time.Millisecond
	t.Cleanup(func() { acpGrace = old })
	exe, _ := os.Executable()
	t.Setenv("SMITHY_HOME", t.TempDir())
	t.Setenv("SMITHY_TEST_FAKE_ACP", "broken")
	t.Setenv("AGENT_PROTOCOL", "acp")
	t.Setenv("AGENT_CMD", "'"+exe+"'")
	dir := t.TempDir()
	p := filepath.Join(dir, "prompt.md")
	os.WriteFile(p, []byte("fix it\n"), 0o644)
	start := time.Now()
	rc := AgentMain([]string{"--timeout", "1", "--log", filepath.Join(dir, "run.log"), p})
	if rc == 0 {
		t.Fatal("protocol error reported success")
	}
	if time.Since(start) > 20*time.Second {
		t.Fatal("the child kept running after the protocol error")
	}
}

func TestBuildACP(t *testing.T) {
	acp := env(map[string]string{"AGENT_PROTOCOL": "acp"})
	cases := []struct {
		cli  string
		o    Options
		want []string
	}{
		{"gemini", Options{Model: "m"}, []string{"gemini", "--experimental-acp", "-m", "m"}},
		{"gemini", Options{ReadOnly: true}, []string{"gemini", "--experimental-acp", "--approval-mode", "plan"}},
		{"opencode", Options{}, []string{"opencode", "acp"}},
	}
	for _, c := range cases {
		s, err := Build(c.cli, c.o, "P", nil, "", acp)
		if err != nil || !reflect.DeepEqual(s.Argv, c.want) || !s.ACP || s.PromptStdin {
			t.Errorf("Build(%s) = %+v, %v; want %q over ACP", c.cli, s, err, c.want)
		}
	}
	if _, err := Build("codex", Options{}, "P", nil, "", acp); err == nil || !strings.Contains(err.Error(), "ACP") {
		t.Fatalf("codex over ACP: err = %v", err)
	}
	s, _ := Build("custom", Options{}, "", nil, "", env(map[string]string{"AGENT_PROTOCOL": "acp", "AGENT_CMD": "x"}))
	if !s.ACP || s.PromptStdin {
		t.Fatalf("custom over ACP = %+v", s)
	}
	if _, err := Build("gemini", Options{}, "P", nil, "", env(map[string]string{"AGENT_PROTOCOL": "grpc"})); err == nil {
		t.Fatal("unknown protocol accepted")
	}
}
