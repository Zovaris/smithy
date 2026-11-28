//go:build harness

package daemon

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

// testdata/opencode-stream.jsonl is a recorded `opencode run --format json` turn: a read, a
// shell call, then the answer.
func TestOpencodeStreamReportsToolsOnceAndOneReply(t *testing.T) {
	b, err := os.ReadFile("testdata/opencode-stream.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	o, r := &opencodeStream{}, &streamEmit{}
	for _, l := range bytes.SplitAfter(b, []byte("\n")) {
		if len(bytes.TrimSpace(l)) > 0 && !o.line(l, r) {
			t.Fatalf("not taken: %s", l)
		}
	}
	if len(r.tools) != 2 || r.tools[0].Name != "Read" || r.tools[1].Name != "Bash" || !strings.Contains(string(r.tools[1].Args), "ls /tmp") || r.tools[0].Took == "" {
		t.Fatalf("tools: %+v", r.tools)
	}
	if len(r.texts) != 1 || !strings.HasPrefix(o.reply(), "The `go.mod` declares") || strings.Contains(o.reply(), "> build") {
		t.Fatalf("texts %q reply %q", r.texts, o.reply())
	}
	if o.usage == nil || o.usage.Input != 11344+2296 || o.usage.CacheRead != 11264 {
		t.Fatalf("usage: %+v", o.usage)
	}
	if o.line([]byte("> build · hy4-flash-free\n"), r) {
		t.Fatal("plain lines are left to the caller")
	}
}

func TestOpencodeStreamKeepsTheErrorEvent(t *testing.T) {
	o, r := &opencodeStream{}, &streamEmit{}
	l := `{"type":"error","error":{"type":"provider.free-limit","message":"Free usage limit reached. Connect a provider or subscribe to OpenCode Go to keep going, or switch models.","status":429}}`
	if !o.line([]byte(l), r) {
		t.Fatal("error event not taken")
	}
	if !strings.HasPrefix(o.err, "Free usage limit reached.") {
		t.Fatalf("err = %q", o.err)
	}
}

func TestTurnErrorNamesTheRealCauseOnce(t *testing.T) {
	stderr := "smithy agent: opencode has no effort flag; it runs at its own default (AGENT_EFFORT=medium is set)\nsmithy agent: pool: none\n"
	if got := turnError(stderr, "Free usage limit reached.", errors.New("exit status 1")).Error(); got != "smithy agent: Free usage limit reached." {
		t.Errorf("stream error first: %q", got)
	}
	if got := turnError("smithy agent: boom\n", "", errors.New("exit status 1")).Error(); got != "smithy agent: boom" {
		t.Errorf("no double prefix: %q", got)
	}
	if got := turnError("", "", errors.New("exit status 1")).Error(); got != "smithy agent: exit status 1" {
		t.Errorf("fallback: %q", got)
	}
}
