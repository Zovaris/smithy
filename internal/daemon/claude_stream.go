//go:build harness

package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/usage"
)

// claudeStream reads `claude -p --output-format stream-json --verbose` lines and reports them as a
// session's events: assistant text, each tool call with its result once the result arrives, and
// the final result's text and usage. Lines that are not stream JSON are left to the caller.
type claudeStream struct {
	pending map[string]pendingTool
	result  string
	usage   *usage.Usage
	now     func() time.Time
}

type pendingTool struct {
	name  string
	input json.RawMessage
	at    time.Time
}

func newClaudeStream() *claudeStream {
	return &claudeStream{pending: map[string]pendingTool{}, now: time.Now}
}

type streamBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// line handles one log line; false when it is not a claude stream event.
func (c *claudeStream) line(l []byte, e Emit) bool {
	l = bytes.TrimSpace(l)
	if len(l) == 0 || l[0] != '{' {
		return false
	}
	var ev struct {
		Type    string `json:"type"`
		Message struct {
			Content []streamBlock `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(l, &ev) != nil || ev.Type == "" {
		return false
	}
	switch ev.Type {
	case "assistant":
		for _, b := range ev.Message.Content {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) != "" {
					e.Text(b.Text)
				}
			case "tool_use":
				c.pending[b.ID] = pendingTool{name: b.Name, input: b.Input, at: c.now()}
			}
		}
	case "user":
		for _, b := range ev.Message.Content {
			if b.Type != "tool_result" {
				continue
			}
			p, ok := c.pending[b.ToolUseID]
			if !ok {
				continue
			}
			delete(c.pending, b.ToolUseID)
			e.Tool(ToolEvent{ID: b.ToolUseID, Name: p.name, Args: p.input, Output: blockText(b.Content), IsError: b.IsError,
				Took: fmt.Sprintf("%.1fs", c.now().Sub(p.at).Seconds())})
		}
	case "result":
		if u, text, ok := usage.FromClaude(l); ok {
			c.result, c.usage = text, &u
		}
	}
	return true
}

// blockText is a tool_result's content: a string, or the text of its text blocks.
func blockText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []streamBlock
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		if p.Type == "text" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "\n")
}
