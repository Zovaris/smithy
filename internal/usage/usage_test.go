package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A real `claude -p --output-format json` result line (trimmed), recorded 2026-11-23.
const claudeResult = `{"type":"result","subtype":"success","is_error":false,"result":"ok","total_cost_usd":0.301288,"usage":{"input_tokens":2,"cache_creation_input_tokens":37650,"cache_read_input_tokens":10,"output_tokens":4}}`

func TestFromClaude(t *testing.T) {
	u, text, ok := FromClaude([]byte(claudeResult))
	if !ok || text != "ok" {
		t.Fatalf("ok=%v text=%q", ok, text)
	}
	if u.Input != 2 || u.Output != 4 || u.CacheRead != 10 || u.CacheWrite != 37650 || u.CostUSD == nil || *u.CostUSD != 0.301288 {
		t.Fatalf("usage %+v", u)
	}
	if _, _, ok := FromClaude([]byte(`{"type":"assistant"}`)); ok {
		t.Fatal("non-result line parsed")
	}
	if _, _, ok := FromClaude([]byte("plain text")); ok {
		t.Fatal("text parsed")
	}
}

func TestFinishRewritesLogAndWritesSidecar(t *testing.T) {
	log := filepath.Join(t.TempDir(), "w.log")
	os.WriteFile(log, []byte("warning: something on stderr\n"+claudeResult+"\n"), 0o600)
	u, ok := Finish(log)
	if !ok || u.Output != 4 {
		t.Fatalf("finish %+v %v", u, ok)
	}
	b, _ := os.ReadFile(log)
	if string(b) != "warning: something on stderr\nok\n" {
		t.Fatalf("log %q", b)
	}
	got, ok := Read(log)
	if !ok || got.CacheWrite != 37650 || *got.CostUSD != 0.301288 {
		t.Fatalf("sidecar %+v %v", got, ok)
	}
}

func TestFinishWithoutResultLeavesLog(t *testing.T) {
	log := filepath.Join(t.TempDir(), "w.log")
	os.WriteFile(log, []byte("smithy agent: timed out after 1m\n"), 0o600)
	if _, ok := Finish(log); ok {
		t.Fatal("usage from nothing")
	}
	if _, err := os.Stat(Sidecar(log)); err == nil {
		t.Fatal("sidecar written")
	}
}

func TestAddAndCost(t *testing.T) {
	c := 0.5
	a := Usage{Input: 1, Output: 2, CostUSD: &c}
	b := Usage{Input: 3}
	s := a.Add(b)
	if s.Input != 4 || s.Output != 2 || s.CostUSD == nil || *s.CostUSD != 0.5 {
		t.Fatalf("sum %+v", s)
	}
	if (Usage{}).Add(Usage{}).CostUSD != nil {
		t.Fatal("cost guessed from nothing")
	}
	if Cost(nil) != "-" || Cost(&c) != "$0.50" {
		t.Fatalf("cost %q %q", Cost(nil), Cost(&c))
	}
	if !strings.Contains(s.String(), "in 4") {
		t.Fatal(s.String())
	}
}
