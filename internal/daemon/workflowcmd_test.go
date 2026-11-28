//go:build harness && !windows

package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/workflow"
)

func wf(t *testing.T, st workflow.Store, args ...string) (string, string, int) {
	t.Helper()
	var o, e bytes.Buffer
	code := workflowMain(st, args, &o, &e)
	return o.String(), e.String(), code
}

func TestWorkflowCLI(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	st := workflow.Store{Dir: t.TempDir()}
	if out, _, _ := wf(t, st, "list"); !strings.Contains(out, "no workflows") {
		t.Fatalf("empty list: %q", out)
	}
	if out, e, code := wf(t, st, "example"); code != 0 || !strings.Contains(out, "dependabot-daily") {
		t.Fatalf("example: %d %q %q", code, out, e)
	}
	if out, _, _ := wf(t, st, "list"); !strings.Contains(out, "dependabot-daily") || !strings.Contains(out, " off ") {
		t.Fatalf("list: %q", out)
	}
	if out, _, _ := wf(t, st, "show", "dependabot-daily"); !strings.Contains(out, "judge") || !strings.Contains(out, "(yesno)") {
		t.Fatalf("show: %q", out)
	}
	src := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(src, []byte(`{"name":"echo","schedule":"@daily","steps":[{"id":"say","kind":"run","run":"echo hello"},{"id":"tell","kind":"notify","message":"got {{steps.say.output}}"}]}`), 0o600)
	if _, e, code := wf(t, st, "add", src); code != 0 {
		t.Fatalf("add: %q", e)
	}
	if _, _, code := wf(t, st, "enable", "echo"); code != 0 {
		t.Fatal("enable")
	}
	if out, _, _ := wf(t, st, "list"); !strings.Contains(out, "echo") || !strings.Contains(out, " on ") {
		t.Fatalf("list after enable: %q", out)
	}
	out, e, code := wf(t, st, "run", "echo")
	if code != 0 || !strings.Contains(out, "notify: echo\ngot hello") || !strings.Contains(out, "ok") {
		t.Fatalf("run: %d %q %q", code, out, e)
	}
	if out, _, _ := wf(t, st, "history", "echo"); !strings.Contains(out, "exit 0 · hello") {
		t.Fatalf("history: %q", out)
	}
	if out, _, code := wf(t, st, "validate", "repo-health"); code != 0 || out != "valid\n" {
		t.Fatalf("validate stored: %d %q", code, out)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"name":"b","steps":[{"id":"a","kind":"run","run":"echo {{steps.a.output}}"}]}`), 0o600)
	if _, e, code := wf(t, st, "validate", bad); code != 1 || !strings.Contains(e, "no templates") {
		t.Fatalf("validate bad: %d %q", code, e)
	}
	if _, e, code := wf(t, st, "hook", "webhook-reply"); code != 1 || !strings.Contains(e, "enable first") {
		t.Fatalf("hook before enable: %q", e)
	}
	wf(t, st, "enable", "webhook-reply")
	if out, _, _ := wf(t, st, "hook", "webhook-reply"); !strings.Contains(out, "/v1/hooks/workflows/webhook-reply") || !strings.Contains(out, "X-Smithy-Hook: ") {
		t.Fatalf("hook: %q", out)
	}
	os.WriteFile(src, []byte(`{"name":"greet","inputs":{"who":"x"},"outputs":{"line":"hi {{inputs.who}}"},"steps":[{"id":"say","kind":"run","run":"echo $SMITHY_INPUT_who"}]}`), 0o600)
	wf(t, st, "add", src)
	if out, e, code := wf(t, st, "run", "greet", "--input", "who=ana"); code != 0 || !strings.Contains(out, "output line: hi ana") || !strings.Contains(out, "exit 0 · ana") {
		t.Fatalf("run with input: %d %q %q", code, out, e)
	}
	if _, _, code := wf(t, st, "run"); code != 2 {
		t.Fatal("run without a name")
	}
	if _, _, code := wf(t, st, "bogus"); code != 2 {
		t.Fatal("unknown subcommand")
	}
}
