//go:build harness

package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

type streamEmit struct {
	texts []string
	tools []ToolEvent
	us    []usage.Usage
}

func (r *streamEmit) Text(t string)       { r.texts = append(r.texts, t) }
func (r *streamEmit) Tool(t ToolEvent)    { r.tools = append(r.tools, t) }
func (r *streamEmit) Usage(u usage.Usage) { r.us = append(r.us, u) }
func (r *streamEmit) Ask(context.Context, string, []string) (string, error) {
	return "", nil
}

func TestClaudeStreamReportsToolsTextAndResult(t *testing.T) {
	c := newClaudeStream()
	at := time.Unix(100, 0)
	c.now = func() time.Time { return at }
	r := &streamEmit{}
	lines := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Reading it."},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"main.go"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"package main"}]}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"go vet"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"exit 1","is_error":true}]}}`,
		`{"type":"result","subtype":"success","result":"Fixed add.","total_cost_usd":0.05,"usage":{"input_tokens":3,"output_tokens":4}}`,
		`plain text`,
	}
	var plain int
	for i, l := range lines {
		if i == 2 || i == 4 {
			at = at.Add(1500 * time.Millisecond)
		}
		if !c.line([]byte(l), r) {
			plain++
		}
	}
	if plain != 1 || len(r.texts) != 1 || r.texts[0] != "Reading it." {
		t.Fatalf("plain %d texts %v", plain, r.texts)
	}
	if len(r.tools) != 2 || r.tools[0].Name != "Read" || r.tools[0].Output != "package main" || r.tools[0].Took != "1.5s" ||
		!strings.Contains(string(r.tools[0].Args), "main.go") || !r.tools[1].IsError || r.tools[1].Output != "exit 1" {
		t.Fatalf("tools %+v", r.tools)
	}
	if c.result != "Fixed add." || c.usage == nil || c.usage.Output != 4 {
		t.Fatalf("result %q usage %+v", c.result, c.usage)
	}
}
