//go:build harness

package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/apiloop"
)

type fakeTools struct {
	calls  []apiloop.ToolCall
	deny   string
	answer func(c apiloop.ToolCall) (string, error)
}

func (f *fakeTools) Specs() []apiloop.ToolSpec {
	return []apiloop.ToolSpec{
		{Name: "search_issues", Description: "Search issues.", Schema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`)},
		{Name: "delete_repo", Description: "Delete a repo."},
	}
}

func (f *fakeTools) Permit(c apiloop.ToolCall) (bool, string) {
	if c.Name == f.deny {
		return false, "denied by profile"
	}
	return true, ""
}

func (f *fakeTools) Call(_ context.Context, c apiloop.ToolCall) (string, error) {
	f.calls = append(f.calls, c)
	if f.answer != nil {
		return f.answer(c)
	}
	return `{"items":[{"id":1},{"id":2}]}`, nil
}

func run(t *testing.T, tools apiloop.Tools, code string, l Limits) (string, error) {
	t.Helper()
	args, _ := json.Marshal(map[string]string{"code": code})
	return Wrap(tools, l).Call(context.Background(), apiloop.ToolCall{ID: "1", Name: ToolName, Args: args})
}

func TestWrapAddsRunCodeWithTypedBindings(t *testing.T) {
	specs := Wrap(&fakeTools{}, Limits{}).Specs()
	if len(specs) != 3 || specs[2].Name != ToolName {
		t.Fatalf("specs = %+v", specs)
	}
	d := specs[2].Description
	for _, want := range []string{"tools.search_issues(args: { query: string })", "tools.delete_repo(args: object)"} {
		if !strings.Contains(d, want) {
			t.Errorf("description lacks %q:\n%s", want, d)
		}
	}
}

func TestProgramCallsToolsAndReturnsLastValue(t *testing.T) {
	f := &fakeTools{}
	out, err := run(t, f, `const r = tools.search_issues({query: "bug"}); console.log("n", r.items.length); r.items.map(i => i.id)`, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[1,2]") || !strings.Contains(out, "n 2") {
		t.Errorf("out = %q", out)
	}
	if len(f.calls) != 1 || string(f.calls[0].Args) != `{"query":"bug"}` {
		t.Errorf("calls = %+v", f.calls)
	}
}

func TestPermitStillDecidesEveryInnerCall(t *testing.T) {
	f := &fakeTools{deny: "delete_repo"}
	out, err := run(t, f, `tools.delete_repo({name: "x"})`, Limits{})
	if err == nil || !strings.Contains(err.Error()+out, "denied by profile") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if len(f.calls) != 0 {
		t.Errorf("denied tool ran: %+v", f.calls)
	}
}

func TestToolErrorBecomesCatchableException(t *testing.T) {
	f := &fakeTools{answer: func(apiloop.ToolCall) (string, error) { return "", errors.New("boom") }}
	out, err := run(t, f, `let m; try { tools.search_issues({query: "a"}) } catch (e) { m = "caught " + e.message } m`, Limits{})
	if err != nil || !strings.Contains(out, "caught") || !strings.Contains(out, "boom") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestTimeCapStopsInfiniteLoop(t *testing.T) {
	start := time.Now()
	_, err := run(t, &fakeTools{}, `while (true) {}`, Limits{Time: 300 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "time limit") {
		t.Fatalf("want a time-limit error, got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
}

func TestMemoryCapStopsHugeAllocation(t *testing.T) {
	for _, code := range []string{
		`let a = []; while (true) { a.push("x".repeat(1 << 20)) }`,
		`let a = []; while (true) { a.push(new Uint8Array(1 << 20)) }`,
		`let s = "x"; while (true) { s = s + s }`,
	} {
		start := time.Now()
		_, err := run(t, &fakeTools{}, code, Limits{Memory: 16 << 20, Time: 20 * time.Second})
		if err == nil || !strings.Contains(err.Error(), "memory") {
			t.Errorf("%s: want a memory-limit error, got %v", code, err)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("%s: took %s", code, time.Since(start))
		}
	}
}

func TestNoFilesystemAccess(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, code := range []string{
		`typeof std === "undefined" && typeof os === "undefined" && typeof require === "undefined" && typeof fetch === "undefined"`,
	} {
		out, err := run(t, &fakeTools{}, code, Limits{})
		if err != nil || !strings.Contains(out, "true") {
			t.Errorf("%s: out=%q err=%v", code, out, err)
		}
	}
	for _, code := range []string{
		`import("qjs:std").then(s => s.loadFile("/secret.txt"))`,
		`import("qjs:std").then(s => s.loadFile("` + secret + `"))`,
		`import("qjs:os")`,
		`import("/secret.txt")`,
	} {
		out, err := run(t, &fakeTools{}, code, Limits{})
		if strings.Contains(out, "hunter2") {
			t.Errorf("%s read the file: %q", code, out)
		}
		_ = err
	}
	out, _ := run(t, &fakeTools{}, `import("qjs:std").then(s => { const f = s.open("/x.txt", "w"); if (f) { f.puts("x".repeat(1000)); f.close(); } return !!f })`, Limits{})
	if strings.Contains(out, "true") {
		t.Errorf("program could write a file: %q", out)
	}
	if b, _ := os.ReadFile(secret); string(b) != "hunter2" {
		t.Error("secret changed")
	}
}

func TestBadArgs(t *testing.T) {
	_, err := Wrap(&fakeTools{}, Limits{}).Call(context.Background(), apiloop.ToolCall{Name: ToolName, Args: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("want an error for empty code")
	}
}

func TestOtherCallsPassThrough(t *testing.T) {
	f := &fakeTools{deny: "delete_repo"}
	w := Wrap(f, Limits{})
	if ok, _ := w.Permit(apiloop.ToolCall{Name: "delete_repo"}); ok {
		t.Error("inner deny lost")
	}
	if ok, _ := w.Permit(apiloop.ToolCall{Name: ToolName}); !ok {
		t.Error("run_code should be permitted; its inner calls are checked")
	}
	if _, err := w.Call(context.Background(), apiloop.ToolCall{Name: "search_issues", Args: json.RawMessage(`{"query":"q"}`)}); err != nil || len(f.calls) != 1 {
		t.Errorf("pass-through failed: %v %+v", err, f.calls)
	}
}
