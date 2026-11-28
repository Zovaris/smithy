//go:build harness

package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Zovaris/smithy/internal/usage"
)

// opencodeStream reads `opencode run --format json` lines and reports them as a session's events:
// each text part, each finished tool call as a tool row, and the tokens and cost every step
// reports. The reply is the text parts joined; the "> build · model" banner the plain format
// prints never appears in this format. Lines that are not its events are left to the caller.
type opencodeStream struct {
	texts []string
	usage *usage.Usage
}

type opencodePart struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Text  string `json:"text"`
	Tool  string `json:"tool"`
	State struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input"`
		Output string          `json:"output"`
		Error  string          `json:"error"`
		Time   struct {
			Start int64 `json:"start"`
			End   int64 `json:"end"`
		} `json:"time"`
	} `json:"state"`
	Cost   float64 `json:"cost"`
	Tokens struct {
		Input, Output, Reasoning int64
		Cache                    struct{ Read, Write int64 }
	} `json:"tokens"`
}

// opencodeTools maps opencode's tool names to the ones the transcript groups and labels.
var opencodeTools = map[string]string{"read": "Read", "shell": "Bash", "bash": "Bash", "edit": "Edit", "write": "Write", "patch": "Edit",
	"grep": "Grep", "glob": "Glob", "list": "LS", "webfetch": "WebFetch", "websearch": "WebSearch", "todowrite": "TodoWrite", "task": "Task"}

func opencodeTool(n string) string {
	if t, ok := opencodeTools[n]; ok {
		return t
	}
	if n == "" {
		return "Tool"
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

// line handles one log line; false when it is not an opencode json event.
func (o *opencodeStream) line(l []byte, e Emit) bool {
	l = bytes.TrimSpace(l)
	if len(l) == 0 || l[0] != '{' {
		return false
	}
	var ev struct {
		Type string       `json:"type"`
		Part opencodePart `json:"part"`
	}
	if json.Unmarshal(l, &ev) != nil || ev.Type == "" {
		return false
	}
	p := ev.Part
	switch ev.Type {
	case "text":
		if strings.TrimSpace(p.Text) != "" {
			o.texts = append(o.texts, p.Text)
			e.Text(p.Text)
		}
	case "tool_use":
		if p.State.Status != "completed" && p.State.Status != "error" {
			return true
		}
		out := p.State.Output
		if p.State.Status == "error" && out == "" {
			out = p.State.Error
		}
		t := ToolEvent{ID: p.ID, Name: opencodeTool(p.Tool), Args: p.State.Input, Output: out, IsError: p.State.Status == "error"}
		if d := p.State.Time.End - p.State.Time.Start; p.State.Time.Start > 0 && d >= 0 {
			t.Took = fmt.Sprintf("%.1fs", float64(d)/1000)
		}
		e.Tool(t)
	case "step_finish":
		u := usage.Usage{Input: p.Tokens.Input, Output: p.Tokens.Output + p.Tokens.Reasoning, CacheRead: p.Tokens.Cache.Read, CacheWrite: p.Tokens.Cache.Write}
		if p.Cost > 0 {
			c := p.Cost
			u.CostUSD = &c
		}
		if o.usage == nil {
			o.usage = &u
		} else {
			s := o.usage.Add(u)
			o.usage = &s
		}
	}
	return true
}

// reply is the turn's answer: its text parts, in order.
func (o *opencodeStream) reply() string { return strings.TrimSpace(strings.Join(o.texts, "\n\n")) }
