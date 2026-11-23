package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAutoStatusAndTakeEmpty(t *testing.T) {
	repo, env := sandbox(t)
	out, errOut, code := run(t, repo, env, "auto", "take")
	if code != 0 || strings.TrimSpace(out) != "auto none: empty queue" {
		t.Errorf("take: %d %q %q", code, out, errOut)
	}
	out, errOut, code = run(t, repo, env, "auto", "status", "--json")
	var st struct {
		Alive bool
		Cap   int
		Queue []any
	}
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || st.Alive || st.Cap != 20 || st.Queue == nil {
		t.Errorf("status: %d %q %q", code, out, errOut)
	}
	if _, _, code = run(t, repo, env, "auto", "report", "github:1", "exploded"); code == 0 {
		t.Error("bad state accepted")
	}
	out, _, _ = run(t, repo, env, "config", "list")
	for _, k := range []string{"auto.sources", "auto.max_per_day", "auto.hours", "linear.key_env", "linear.states"} {
		if !strings.Contains(out, k) {
			t.Errorf("config list lacks %s", k)
		}
	}
}
