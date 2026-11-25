// Package acp drives one prompt turn of an Agent Client Protocol agent (JSON-RPC 2.0, one message
// per line on the agent's stdin and stdout) and maps what it reports to smithy's terms: answer
// text, tool calls, usage and permission requests. The client offers no file system or terminal
// capability, so the agent uses its own tools and every request for ours is refused.
package acp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Zovaris/smithy/internal/usage"
)

// ProtocolVersion is the ACP major version this client speaks.
const ProtocolVersion = 1

// Session is one run: the agent's working directory and the prompt text.
type Session struct {
	Cwd    string
	Prompt string
}

// Tool is a tool call the agent reported or updated (title and kind carried over from the first report).
type Tool struct {
	ID, Title, Kind, Status string
}

// Option is one answer a permission request offers (kind: allow_once, allow_always, reject_once, reject_always).
type Option struct {
	ID   string `json:"optionId"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Permission is the agent asking to run a tool call.
type Permission struct {
	Tool    string
	Title   string
	Options []Option
}

// Sink receives what the agent reports. Permit returns the chosen option id, "" to cancel.
type Sink interface {
	Text(string)
	Tool(Tool)
	Permit(Permission) string
}

// Result is how the turn ended and the usage the agent reported (HasUsage false when none).
type Result struct {
	StopReason string
	Usage      usage.Usage
	HasUsage   bool
}

// Choose picks the one-time option of the wanted kind (allow or reject), else the lasting one,
// else "" (cancel): a run never grants more than it was asked for.
func Choose(opts []Option, allow bool) string {
	want := "reject"
	if allow {
		want = "allow"
	}
	for _, k := range []string{want + "_once", want + "_always"} {
		for _, o := range opts {
			if o.Kind == k {
				return o.ID
			}
		}
	}
	return ""
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type client struct {
	in    *bufio.Scanner
	out   io.Writer
	next  int
	sink  Sink
	tools map[string]Tool
	res   Result
}

// Run initializes the agent, opens a session in s.Cwd with no MCP servers, sends s.Prompt and
// returns when the prompt turn ends. from is the agent's stdout, to its stdin.
func Run(from io.Reader, to io.Writer, s Session, sink Sink) (Result, error) {
	in := bufio.NewScanner(from)
	in.Buffer(make([]byte, 64<<10), 16<<20)
	c := &client{in: in, out: to, sink: sink, tools: map[string]Tool{}}
	var init struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	err := c.call("initialize", map[string]any{
		"protocolVersion":    ProtocolVersion,
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false},
		"clientInfo":         map[string]string{"name": "smithy"},
	}, &init)
	if err != nil {
		return c.res, err
	}
	if init.ProtocolVersion != ProtocolVersion {
		return c.res, fmt.Errorf("acp: agent speaks protocol version %d, smithy speaks %d", init.ProtocolVersion, ProtocolVersion)
	}
	var sess struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.call("session/new", map[string]any{"cwd": s.Cwd, "mcpServers": []any{}}, &sess); err != nil {
		return c.res, err
	}
	if sess.SessionID == "" {
		return c.res, errors.New("acp: session/new returned no sessionId")
	}
	var done struct {
		StopReason string `json:"stopReason"`
		Usage      *struct {
			Input      int64 `json:"inputTokens"`
			Output     int64 `json:"outputTokens"`
			CacheRead  int64 `json:"cachedReadTokens"`
			CacheWrite int64 `json:"cachedWriteTokens"`
		} `json:"usage"`
	}
	err = c.call("session/prompt", map[string]any{
		"sessionId": sess.SessionID,
		"prompt":    []map[string]string{{"type": "text", "text": s.Prompt}},
	}, &done)
	if err != nil {
		return c.res, err
	}
	c.res.StopReason = done.StopReason
	if u := done.Usage; u != nil {
		c.res.HasUsage = true
		c.res.Usage.Input, c.res.Usage.Output, c.res.Usage.CacheRead, c.res.Usage.CacheWrite = u.Input, u.Output, u.CacheRead, u.CacheWrite
	}
	return c.res, nil
}

func (c *client) send(m message) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = c.out.Write(append(b, '\n'))
	return err
}

// call sends one request and serves the agent's notifications and requests until its answer.
func (c *client) call(method string, params, result any) error {
	c.next++
	id := json.RawMessage(fmt.Sprint(c.next))
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err := c.send(message{ID: id, Method: method, Params: p}); err != nil {
		return fmt.Errorf("acp: %s: %w", method, err)
	}
	for c.in.Scan() {
		var m message
		if json.Unmarshal(c.in.Bytes(), &m) != nil {
			continue
		}
		switch {
		case m.Method != "" && len(m.ID) > 0:
			if err := c.request(m); err != nil {
				return err
			}
		case m.Method != "":
			c.notify(m)
		case string(m.ID) == string(id):
			if m.Error != nil {
				return fmt.Errorf("acp: %s: %s (%d)", method, m.Error.Message, m.Error.Code)
			}
			if result != nil && len(m.Result) > 0 {
				return json.Unmarshal(m.Result, result)
			}
			return nil
		}
	}
	if err := c.in.Err(); err != nil {
		return fmt.Errorf("acp: %s: %w", method, err)
	}
	return fmt.Errorf("acp: %s: agent closed its output before answering", method)
}

// request answers what the agent asks of the client: permissions through the sink, the rest refused.
func (c *client) request(m message) error {
	if m.Method != "session/request_permission" {
		return c.send(message{ID: m.ID, Error: &struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{-32601, "method not supported by smithy: " + m.Method}})
	}
	var p struct {
		ToolCall struct {
			ID    string `json:"toolCallId"`
			Title string `json:"title"`
		} `json:"toolCall"`
		Options []Option `json:"options"`
	}
	_ = json.Unmarshal(m.Params, &p)
	title := p.ToolCall.Title
	if title == "" {
		title = c.tools[p.ToolCall.ID].Title
	}
	outcome := map[string]string{"outcome": "cancelled"}
	if id := c.sink.Permit(Permission{Tool: p.ToolCall.ID, Title: title, Options: p.Options}); id != "" {
		outcome = map[string]string{"outcome": "selected", "optionId": id}
	}
	r, _ := json.Marshal(map[string]any{"outcome": outcome})
	return c.send(message{ID: m.ID, Result: r})
}

// notify maps a session/update: message text to the sink, tool calls merged by id, cost in USD.
// Thoughts, plans and other updates are not part of the answer and are dropped.
func (c *client) notify(m message) {
	if m.Method != "session/update" {
		return
	}
	var p struct {
		Update struct {
			Kind    string `json:"sessionUpdate"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			ToolCallID string `json:"toolCallId"`
			Title      string `json:"title"`
			ToolKind   string `json:"kind"`
			Status     string `json:"status"`
			Cost       *struct {
				Amount   float64 `json:"amount"`
				Currency string  `json:"currency"`
			} `json:"cost"`
		} `json:"update"`
	}
	if json.Unmarshal(m.Params, &p) != nil {
		return
	}
	u := p.Update
	switch u.Kind {
	case "agent_message_chunk":
		if u.Content.Type == "text" {
			c.sink.Text(u.Content.Text)
		}
	case "tool_call", "tool_call_update":
		t := c.tools[u.ToolCallID]
		t.ID = u.ToolCallID
		for dst, v := range map[*string]string{&t.Title: u.Title, &t.Kind: u.ToolKind, &t.Status: u.Status} {
			if v != "" {
				*dst = v
			}
		}
		c.tools[t.ID] = t
		c.sink.Tool(t)
	case "usage_update":
		if u.Cost != nil && strings.EqualFold(u.Cost.Currency, "USD") {
			v := u.Cost.Amount
			c.res.Usage.CostUSD = &v
			c.res.HasUsage = true
		}
	}
}
