//go:build harness

package codemode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/fastschema/qjs"
)

// evalPrelude gives a workflow's code step its input and helpers, then removes every way out.
const evalPrelude = `(() => {
  const freeze = (o) => { if (o && typeof o === "object") { Object.values(o).forEach(freeze); Object.freeze(o); } return o; };
  const input = JSON.parse(globalThis.__smithy_input);
  delete globalThis.__smithy_input;
  for (const k of ["steps", "inputs", "vars"]) {
    Object.defineProperty(globalThis, k, { value: freeze(input[k] || {}), enumerable: true });
  }
  for (const k of ["item", "index"]) {
    if (k in input) Object.defineProperty(globalThis, k, { value: freeze(input[k]), enumerable: true });
  }
  const lines = (s) => String(s ?? "").split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
  const json = (s) => (typeof s === "string" ? JSON.parse(s) : s);
  Object.defineProperty(globalThis, "lines", { value: lines, enumerable: true });
  Object.defineProperty(globalThis, "json", { value: json, enumerable: true });
  for (const k of ["std", "os", "bjson", "setTimeout", "clearTimeout", "setInterval", "clearInterval", "fetch", "require"]) {
    try { delete globalThis[k]; } catch (_) {}
  }
})();`

// Eval runs a workflow code step over input (JSON with steps, inputs, vars and, in a foreach,
// item and index): its return value, or else its last expression, is the result, strings bare and
// the rest as JSON. The sandbox and limits are run_code's, with no tools at all.
func Eval(ctx context.Context, code, input string, l Limits) (out string, err error) {
	if l.Memory <= 0 {
		l.Memory = defaultMemory
	}
	if l.Time <= 0 {
		l.Time = defaultTime
	}
	ctx, cancel := context.WithTimeoutCause(ctx, l.Time, errTime)
	defer cancel()
	var logs bytes.Buffer
	rt, done, err := sandbox(ctx, l, &logs)
	if err != nil {
		return "", err
	}
	defer done()
	defer func() {
		if r := recover(); r != nil {
			out, err = "", limitErr(ctx, fmt.Errorf("%v", r))
		}
		func() {
			defer func() { _ = recover() }()
			rt.Close()
		}()
	}()
	rt.Context().Global().SetPropertyStr("__smithy_input", rt.Context().NewString(input))
	pv, err := rt.Eval("prelude.js", qjs.Code(evalPrelude))
	if err != nil {
		return "", fmt.Errorf("code setup: %w", err)
	}
	pv.Free()
	v, fn, err := evalStep(rt, code)
	if err != nil {
		if c := context.Cause(ctx); c != nil {
			return "", fmt.Errorf("code: %w", c)
		}
		return "", jsErr(err, fn, code)
	}
	defer v.Free()
	switch {
	case v.IsUndefined():
		out = ""
	case v.IsString():
		out = v.String()
	default:
		if out, err = v.JSONStringify(); err != nil {
			return "", jsErr(err, fn, code)
		}
	}
	if len(out) > maxOutput {
		return "", fmt.Errorf("code: the result is over %d KB", maxOutput>>10)
	}
	return out, nil
}

// evalStep runs code as a top-level async script (the last expression is the result) and, when
// it returns, as an async function body instead; fn says which. QuickJS keeps line numbers only
// for top-level code, so the script is tried first.
func evalStep(rt *qjs.Runtime, code string) (v *qjs.Value, fn bool, err error) {
	v, err = rt.Eval("code.js", qjs.Code(code), qjs.FlagAsync())
	if err != nil && strings.Contains(err.Error(), "return not in a function") {
		fn = true
		v, err = rt.Eval("code.js", qjs.Code("(async () => {"+code+"\n})().then((v) => v === undefined ? \"\" : typeof v === \"string\" ? v : (JSON.stringify(v) ?? \"\"))"))
	}
	if err == nil && v.IsPromise() {
		var r *qjs.Value
		r, err = v.Await()
		v.Free()
		v = r
	}
	return v, fn, err
}

var frameRe = regexp.MustCompile(`at (\S+) \(code\.js:(\d+):(\d+)\)`)

// jsErr is the JS message and the line in the step's code it came from. A top-level script's
// <eval> frame holds the true line; inside a function body only a thrown Error's frame does, so
// a frame at the body's start (1:1) names no line rather than a wrong one, unless the code is one line.
func jsErr(err error, fn bool, code string) error {
	msg := err.Error()
	first, _, _ := strings.Cut(msg, "\n")
	first = strings.TrimSpace(strings.TrimPrefix(first, "Error: "))
	line := ""
	for _, m := range frameRe.FindAllStringSubmatch(msg, -1) {
		if !fn && m[1] == "<eval>" || fn && m[1] != "<eval>" && m[2]+":"+m[3] != "1:1" {
			line = m[2]
			break
		}
	}
	if line == "" && !strings.Contains(msg, "    at ") {
		if m := regexp.MustCompile(`code\.js:(\d+)`).FindStringSubmatch(msg); m != nil {
			line = m[1]
		}
	}
	if line == "" && !strings.Contains(code, "\n") {
		line = "1"
	}
	if line != "" {
		return fmt.Errorf("%s (line %s)", first, line)
	}
	return errors.New(first)
}
