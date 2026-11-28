//go:build harness

package daemon

import (
	"bytes"
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
