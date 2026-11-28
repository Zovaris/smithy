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

func cli(t *testing.T, main func(workflow.Store, []string, *bytes.Buffer, *bytes.Buffer) int, st workflow.Store, args ...string) (string, string, int) {
	t.Helper()
	var o, e bytes.Buffer
	code := main(st, args, &o, &e)
	return o.String(), e.String(), code
}

func wfMain(st workflow.Store, a []string, o, e *bytes.Buffer) int { return workflowMain(st, a, o, e) }
func schedMain(st workflow.Store, a []string, o, e *bytes.Buffer) int {
	return scheduleMain(st, a, o, e)
}

func TestWorkflowAndScheduleCLI(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	st := workflow.Store{Dir: t.TempDir()}
	if out, _, _ := cli(t, wfMain, st, "list"); !strings.Contains(out, "no workflows") {
		t.Fatalf("empty: %q", out)
	}
	if out, e, code := cli(t, wfMain, st, "example"); code != 0 || !strings.Contains(out, "added workflow dependabot-daily and schedule dependabot-daily") {
		t.Fatalf("example: %d %q %q", code, out, e)
	}
	if out, _, _ := cli(t, wfMain, st, "list"); !strings.Contains(out, "schedules repo-health") {
		t.Fatalf("list: %q", out)
	}
	if out, _, _ := cli(t, schedMain, st, "list"); !strings.Contains(out, "0 17 * * *") || !strings.Contains(out, " off ") {
		t.Fatalf("schedule list: %q", out)
	}
	src := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(src, []byte(`{"name":"greet","inputs":{"who":"x"},"outputs":{"line":"hi {{inputs.who}}"},"steps":[{"id":"say","kind":"run","run":"echo $SMITHY_INPUT_who"}]}`), 0o600)
	if out, e, code := cli(t, wfMain, st, "add", src); code != 0 || out != "added workflow greet\n" {
		t.Fatalf("add: %q %q", out, e)
	}
	if out, e, code := cli(t, wfMain, st, "run", "greet", "--input", "who=ana"); code != 0 || !strings.Contains(out, "output line: hi ana") || !strings.Contains(out, "exit 0 · ana") {
		t.Fatalf("run: %d %q %q", code, out, e)
	}
	if out, _, _ := cli(t, wfMain, st, "show", "greet"); !strings.Contains(out, `input who (default "x")`) {
		t.Fatalf("show: %q", out)
	}
	if _, e, code := cli(t, schedMain, st, "add", "nightly", "--workflow", "greet", "--cron", "0 2 * * *", "--input", "who=bo"); code != 0 {
		t.Fatalf("schedule add: %q", e)
	}
	if _, e, code := cli(t, schedMain, st, "add", "two", "--workflow", "greet", "--cron", "@daily", "--webhook"); code != 1 || !strings.Contains(e, "exactly one") {
		t.Fatalf("two triggers: %q", e)
	}
	if _, _, code := cli(t, schedMain, st, "enable", "nightly"); code != 0 {
		t.Fatal("enable")
	}
	if out, _, _ := cli(t, schedMain, st, "list"); !strings.Contains(out, "nightly") || !strings.Contains(out, " on ") {
		t.Fatalf("on: %q", out)
	}
	if _, e, code := cli(t, schedMain, st, "hook", "nightly"); code != 1 || !strings.Contains(e, "webhook schedule") {
		t.Fatalf("hook on cron: %q", e)
	}
	cli(t, schedMain, st, "enable", "webhook-reply")
	if out, _, _ := cli(t, schedMain, st, "hook", "webhook-reply"); !strings.Contains(out, "/v1/hooks/schedules/webhook-reply") {
		t.Fatalf("hook: %q", out)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{"name":"b","steps":[{"id":"a","kind":"run","run":"echo {{steps.a.output}}"}]}`), 0o600)
	if _, e, code := cli(t, wfMain, st, "validate", bad); code != 1 || !strings.Contains(e, "no templates") {
		t.Fatalf("validate: %q", e)
	}
	if _, _, code := cli(t, schedMain, st, "remove", "nightly"); code != 0 {
		t.Fatal("remove")
	}
	if _, _, code := cli(t, wfMain, st, "bogus"); code != 2 {
		t.Fatal("unknown")
	}
}
