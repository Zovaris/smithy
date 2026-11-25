// Package mcp is smithy's MCP server (stdio, newline-delimited JSON-RPC 2.0): the tools a driven
// CLI gets through the injection kit. Every tool validates its input, its output is capped and
// cleaned of terminal escapes, and no tool returns a secret's value.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Zovaris/smithy/internal/text"
)

// MaxOutput caps the text one tool call returns, in bytes.
const MaxOutput = 64 << 10

// MaxRequest caps one request line, in bytes.
const MaxRequest = 1 << 20

// ProtocolVersion is the newest MCP revision this server speaks; an older one a client asks for is echoed.
const ProtocolVersion = "2025-06-18"

var versions = []string{"2024-11-05", "2025-03-26", "2025-06-18"}

// Args are one call's arguments.
type Args map[string]any

// Tool is one MCP tool. Call returns the text shown to the agent; an error becomes a tool error.
type Tool struct {
	Name, Description string
	Schema            map[string]any
	Call              func(ctx context.Context, a Args) (string, error)
}

// Server serves Tools over one stdio connection.
type Server struct {
	Name, Version string
	Tools         []Tool
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads requests from in until EOF and writes replies to out. Tool calls run one at a
// time each in its own goroutine so a long ask doesn't block a ping.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	var mu sync.Mutex
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)
	defer wg.Wait()
	defer cancel() // stdin closed: the client is gone, so a waiting ask ends
	send := func(id json.RawMessage, result any, e *rpcError) {
		m := map[string]any{"jsonrpc": "2.0", "id": id}
		if len(id) == 0 {
			m["id"] = nil
		}
		if e != nil {
			m["error"] = e
		} else {
			m["result"] = result
		}
		b, _ := json.Marshal(m)
		mu.Lock()
		defer mu.Unlock()
		out.Write(append(b, '\n'))
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), MaxRequest)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r request
		if err := json.Unmarshal([]byte(line), &r); err != nil || r.Method == "" {
			send(r.ID, nil, &rpcError{-32700, "parse error: expected a JSON-RPC request"})
			continue
		}
		if len(r.ID) == 0 {
			continue // a notification: no reply
		}
		switch r.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			json.Unmarshal(r.Params, &p)
			v := ProtocolVersion
			if slices.Contains(versions, p.ProtocolVersion) {
				v = p.ProtocolVersion
			}
			send(r.ID, map[string]any{"protocolVersion": v, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": s.Name, "version": s.Version}}, nil)
		case "ping":
			send(r.ID, map[string]any{}, nil)
		case "tools/list":
			list := []map[string]any{}
			for _, t := range s.Tools {
				list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
			}
			send(r.ID, map[string]any{"tools": list}, nil)
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(r.Params, &p); err != nil {
				send(r.ID, nil, &rpcError{-32602, "bad params"})
				continue
			}
			i := slices.IndexFunc(s.Tools, func(t Tool) bool { return t.Name == p.Name })
			if i < 0 {
				send(r.ID, nil, &rpcError{-32602, "unknown tool " + text.Clean(p.Name)})
				continue
			}
			t, id := s.Tools[i], r.ID
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, isErr := call(ctx, t, p.Arguments)
				send(id, map[string]any{"content": []map[string]any{{"type": "text", "text": out}}, "isError": isErr}, nil)
			}()
		default:
			send(r.ID, nil, &rpcError{-32601, "method not found: " + text.Clean(r.Method)})
		}
	}
	return sc.Err()
}

func call(ctx context.Context, t Tool, a map[string]any) (string, bool) {
	if a == nil {
		a = map[string]any{}
	}
	props, _ := t.Schema["properties"].(map[string]any)
	for k := range a {
		if _, ok := props[k]; !ok {
			return "unknown argument " + text.Clean(k), true
		}
	}
	out, err := t.Call(ctx, Args(a))
	if err != nil {
		return capOut(text.Clean(err.Error())), true
	}
	return capOut(out), false
}

// capOut cuts s to MaxOutput bytes on a rune boundary and strips terminal escapes.
func capOut(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = text.StripEscapes(l, true)
	}
	s = strings.Join(lines, "\n")
	if len(s) <= MaxOutput {
		return s
	}
	cut := MaxOutput
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n[truncated: %d of %d bytes shown]", cut, len(s))
}

// Object is a JSON schema object with props; required names the props that must be set.
func Object(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

// Str, Int, Bool and List are schema props.
func Str(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func Int(desc string) map[string]any  { return map[string]any{"type": "integer", "description": desc} }
func Bool(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
func List(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

// String is argument k, at most max bytes with no control characters except newline and tab
// in a body; "" when absent and not required.
func (a Args) String(k string, max int, required bool) (string, error) {
	v, ok := a[k]
	if !ok || v == nil {
		if required {
			return "", fmt.Errorf("%s is required", k)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", k)
	}
	if len(s) > max {
		return "", fmt.Errorf("%s is longer than %d bytes", k, max)
	}
	if required && strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s is required", k)
	}
	return s, nil
}

// Line is String with no control characters at all.
func (a Args) Line(k string, max int, required bool) (string, error) {
	s, err := a.String(k, max, required)
	if err == nil && strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		err = fmt.Errorf("%s must be one line with no control characters", k)
	}
	return s, err
}

// Int is argument k in [min, max]; def when absent.
func (a Args) Int(k string, min, max, def int) (int, error) {
	v, ok := a[k]
	if !ok || v == nil {
		return def, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) || int(f) < min || int(f) > max {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", k, min, max)
	}
	return int(f), nil
}

// Bool is argument k; false when absent.
func (a Args) Bool(k string) (bool, error) {
	v, ok := a[k]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be true or false", k)
	}
	return b, nil
}

// Strings is argument k: at most n one-line strings of at most max bytes each.
func (a Args) Strings(k string, n, max int) ([]string, error) {
	v, ok := a[k]
	if !ok || v == nil {
		return nil, nil
	}
	l, ok := v.([]any)
	if !ok || len(l) > n {
		return nil, fmt.Errorf("%s must be a list of at most %d strings", k, n)
	}
	var out []string
	for _, x := range l {
		s, err := Args{k: x}.Line(k, max, true)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
