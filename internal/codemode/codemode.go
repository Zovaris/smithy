//go:build harness

// Package codemode lets the API loop's model write one short JavaScript program instead of many
// tool calls. The program runs in QuickJS compiled to WebAssembly (fastschema/qjs on wazero, no
// cgo) with a memory cap, a time cap, no filesystem and no network: its only way out is the
// `tools` object, one function per tool the loop already offers, and every call still goes
// through that tool's permission check. Off by default (setting agent.code_mode).
package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/fastschema/qjs"
)

// ToolName is the tool the model calls with its program.
const ToolName = "run_code"

const (
	defaultMemory = 64 << 20
	defaultTime   = 30 * time.Second
	maxOutput     = 50 << 10
)

// Limits cap one program run; zero values take the defaults (64 MB, 30 s).
type Limits struct {
	Memory int
	Time   time.Duration
}

// Wrap returns tools plus run_code, whose program reaches tools only through their Permit and Call.
func Wrap(tools apiloop.Tools, l Limits) apiloop.Tools {
	if l.Memory <= 0 {
		l.Memory = defaultMemory
	}
	if l.Time <= 0 {
		l.Time = defaultTime
	}
	return &wrapped{inner: tools, lim: l}
}

type wrapped struct {
	inner apiloop.Tools
	lim   Limits
}

func (w *wrapped) Specs() []apiloop.ToolSpec {
	specs := append([]apiloop.ToolSpec(nil), w.inner.Specs()...)
	return append(specs, apiloop.ToolSpec{
		Name:        ToolName,
		Description: describe(w.inner.Specs()),
		Schema:      json.RawMessage(`{"type":"object","properties":{"code":{"type":"string","description":"JavaScript program; its last expression is the result"}},"required":["code"]}`),
	})
}

func (w *wrapped) Permit(c apiloop.ToolCall) (bool, string) {
	if c.Name == ToolName {
		return true, ""
	}
	return w.inner.Permit(c)
}

func (w *wrapped) Call(ctx context.Context, c apiloop.ToolCall) (string, error) {
	if c.Name != ToolName {
		return w.inner.Call(ctx, c)
	}
	var a struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(c.Args, &a); err != nil || strings.TrimSpace(a.Code) == "" {
		return "", errors.New("run_code needs {\"code\": \"<program>\"}")
	}
	return w.exec(ctx, a.Code)
}

func describe(specs []apiloop.ToolSpec) string {
	var b strings.Builder
	b.WriteString("Run a short JavaScript program instead of many tool calls. No filesystem, network, imports or timers; " +
		"console.log is captured. Calls are synchronous and return parsed JSON (or the text); a failed call throws. " +
		"The last expression is the result. Available:\n")
	for _, s := range specs {
		fmt.Fprintf(&b, "- tools.%s(args: %s): %s\n", s.Name, tsType(s.Schema), firstLine(s.Description))
	}
	return b.String()
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

type schema struct {
	Type       any                `json:"type"`
	Properties map[string]*schema `json:"properties"`
	Required   []string           `json:"required"`
	Items      *schema            `json:"items"`
	Enum       []any              `json:"enum"`
}

func tsType(raw json.RawMessage) string {
	var s schema
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return "object"
	}
	return s.ts(0)
}

func (s *schema) ts(depth int) string {
	if s == nil || depth > 4 {
		return "any"
	}
	if len(s.Enum) > 0 {
		var vs []string
		for _, e := range s.Enum {
			b, _ := json.Marshal(e)
			vs = append(vs, string(b))
		}
		return strings.Join(vs, " | ")
	}
	t, _ := s.Type.(string)
	switch t {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		return s.Items.ts(depth+1) + "[]"
	case "object", "":
		if len(s.Properties) == 0 {
			return "object"
		}
		req := map[string]bool{}
		for _, r := range s.Required {
			req[r] = true
		}
		names := make([]string, 0, len(s.Properties))
		for n := range s.Properties {
			names = append(names, n)
		}
		sort.Strings(names)
		var fs []string
		for _, n := range names {
			opt := "?"
			if req[n] {
				opt = ""
			}
			fs = append(fs, n+opt+": "+s.Properties[n].ts(depth+1))
		}
		return "{ " + strings.Join(fs, "; ") + " }"
	}
	return "any"
}

// The QuickJS build mounts a host folder as "/"; exec gives it an empty read-only temp dir.
// prelude builds the tools object over the one host function, then removes every other way out.
const prelude = `(() => {
  const call = globalThis.__smithy_call;
  delete globalThis.__smithy_call;
  const names = JSON.parse(__smithy_names);
  delete globalThis.__smithy_names;
  const tools = {};
  for (const n of names) {
    tools[n] = (args) => {
      const out = call(n, JSON.stringify(args === undefined ? {} : args));
      try { return JSON.parse(out); } catch (_) { return out; }
    };
  }
  Object.defineProperty(globalThis, "tools", { value: Object.freeze(tools), enumerable: true });
  for (const k of ["std", "os", "bjson", "setTimeout", "clearTimeout", "setInterval", "clearInterval", "fetch", "require"]) {
    try { delete globalThis[k]; } catch (_) {}
  }
})();`

func (w *wrapped) exec(ctx context.Context, code string) (out string, err error) {
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	ctx, cancel := context.WithTimeoutCause(ctx, w.lim.Time, errTime)
	defer cancel()
	jail, err := os.MkdirTemp("", "smithy-codemode-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(jail)
	if err := os.Chmod(jail, 0o500); err != nil {
		return "", err
	}
	var logs bytes.Buffer
	rt, err := qjs.New(qjs.Option{
		CWD:                jail,
		Context:            ctx,
		CloseOnContextDone: true,
		MemoryLimit:        w.lim.Memory,
		Stdout:             &capped{b: &logs},
		Stderr:             &capped{b: &logs},
	})
	if err != nil {
		return "", fmt.Errorf("code mode: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = logs.String(), limitErr(ctx, fmt.Errorf("%v", r))
		}
		func() {
			defer func() { _ = recover() }()
			rt.Close()
		}()
	}()
	go watch(ctx, rt.Mem(), uint32(min(2*w.lim.Memory, 1<<31)), stop)
	jc := rt.Context()
	names := []string{}
	for _, s := range w.inner.Specs() {
		names = append(names, s.Name)
	}
	nb, _ := json.Marshal(names)
	jc.Global().SetPropertyStr("__smithy_names", jc.NewString(string(nb)))
	jc.SetFunc("__smithy_call", func(this *qjs.This) (*qjs.Value, error) {
		args := this.Args()
		if len(args) < 2 {
			return nil, errors.New("tool call needs a name and arguments")
		}
		c := apiloop.ToolCall{Name: args[0].String(), Args: json.RawMessage(args[1].String())}
		if ok, why := w.inner.Permit(c); !ok {
			return nil, fmt.Errorf("permission denied for %s: %s", c.Name, why)
		}
		res, err := w.inner.Call(ctx, c)
		if err != nil {
			return nil, err
		}
		return this.Context().NewString(res), nil
	})
	if v, err := rt.Eval("prelude.js", qjs.Code(prelude)); err != nil {
		return "", fmt.Errorf("code mode setup: %w", err)
	} else {
		v.Free()
	}
	v, err := rt.Eval("program.js", qjs.Code(code))
	if err == nil && v.IsPromise() {
		var r *qjs.Value
		r, err = v.Await()
		v.Free()
		v = r
	}
	if err != nil {
		return logs.String(), limitErr(ctx, err)
	}
	defer v.Free()
	res := v.String()
	if !v.IsUndefined() {
		if s, jerr := v.JSONStringify(); jerr == nil && s != "" {
			res = s
		}
	}
	if logs.Len() > 0 {
		res = logs.String() + "\nresult: " + res
	}
	if len(res) > maxOutput {
		res = res[:maxOutput] + "\n[output truncated]"
	}
	return res, nil
}

var (
	errTime   = errors.New("time limit reached")
	errMemory = errors.New("memory limit reached")
)

// watch stops the run when the WebAssembly memory outgrows max: QuickJS's own limit misses
// ArrayBuffer storage, so this is the cap that holds.
func watch(ctx context.Context, m *qjs.Mem, max uint32, stop context.CancelCauseFunc) {
	t := time.NewTicker(5 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if m.Size() > max {
				stop(errMemory)
				return
			}
		}
	}
}

func limitErr(ctx context.Context, err error) error {
	if c := context.Cause(ctx); c != nil {
		return fmt.Errorf("code mode: %w", c)
	}
	return fmt.Errorf("code mode: %w", err)
}

type capped struct{ b *bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if room := maxOutput - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}
