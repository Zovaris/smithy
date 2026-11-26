//go:build harness

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/bench"
)

func TestBenchRunsAndCompare(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	sv.BenchDir = t.TempDir()
	for i, pass := range []bool{true, false} {
		r := bench.Result{ID: "2026110" + string(rune('1'+i)) + "-run", Agent: "claude", Started: time.Now(), Passed: 0, Total: 1,
			Tasks: []bench.TaskResult{{ID: "bug-a", Kind: "bug", Pass: pass}}}
		b, _ := json.Marshal(r)
		os.WriteFile(filepath.Join(sv.BenchDir, r.ID+".json"), b, 0o644)
	}
	auth := map[string]string{"Authorization": "Bearer " + testToken}
	res := do(t, "GET", hs.URL+"/v1/bench", "", auth)
	var runs []bench.Summary
	json.NewDecoder(res.Body).Decode(&runs)
	if res.StatusCode != 200 || len(runs) != 2 || runs[0].ID != "20261102-run" {
		t.Fatalf("runs %d %+v", res.StatusCode, runs)
	}
	res = do(t, "GET", hs.URL+"/v1/bench/compare?a=20261101-run&b=latest", "", auth)
	var c bench.Comparison
	json.NewDecoder(res.Body).Decode(&c)
	if res.StatusCode != 200 || len(c.Regressed) != 1 || c.Regressed[0] != "bug-a" {
		t.Fatalf("compare %d %+v", res.StatusCode, c)
	}
	if res := do(t, "GET", hs.URL+"/v1/bench/compare?a=nope&b=latest", "", auth); res.StatusCode != 404 {
		t.Fatalf("bad ref status %d, want 404", res.StatusCode)
	}
	if res := do(t, "GET", hs.URL+"/v1/bench/compare?a=../../etc/passwd.json&b=latest", "", auth); res.StatusCode != 404 {
		t.Fatalf("path ref status %d, want 404", res.StatusCode)
	}
	if res := do(t, "GET", hs.URL+"/v1/bench", "", nil); res.StatusCode != 401 {
		t.Fatalf("no token status %d", res.StatusCode)
	}
}

func TestBenchStartOneAtATime(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	release := make(chan struct{})
	got := make(chan []string, 1)
	sv.BenchRun = func(args []string) error {
		got <- args
		<-release
		return nil
	}
	hdr := map[string]string{"Authorization": "Bearer " + testToken}
	status := func() BenchStatus {
		res := do(t, "GET", hs.URL+"/v1/bench/status", "", hdr)
		var s BenchStatus
		json.NewDecoder(res.Body).Decode(&s)
		return s
	}
	if res := do(t, "POST", hs.URL+"/v1/bench/run", `{"agent":"-rf"}`, hdr); res.StatusCode != 400 {
		t.Errorf("flag-like agent: %d, want 400", res.StatusCode)
	}
	if res := do(t, "POST", hs.URL+"/v1/bench/run", `{"agent":"claude","model":"sonnet"}`, hdr); res.StatusCode != 202 {
		t.Fatalf("start: %d", res.StatusCode)
	}
	if a := <-got; strings.Join(a, " ") != "bench run --agent claude --model sonnet" {
		t.Errorf("args: %q", a)
	}
	if s := status(); !s.Running || s.Agent != "claude" {
		t.Errorf("status while running: %+v", s)
	}
	if res := do(t, "POST", hs.URL+"/v1/bench/run", `{"agent":"codex"}`, hdr); res.StatusCode != 409 {
		t.Errorf("second start: %d, want 409", res.StatusCode)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for status().Running && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s := status(); s.Running {
		t.Errorf("still running after the run ended: %+v", s)
	}
}
