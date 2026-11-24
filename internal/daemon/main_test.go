//go:build harness

package daemon

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSplitFlags(t *testing.T) {
	f, rest, err := splitFlags([]string{"--bg", "--agent", "codex", "--port", "9000", "--once"})
	if err != nil || !f.bg || f.port != 9000 || strings.Join(rest, " ") != "--agent codex --once" {
		t.Fatalf("%+v %q %v", f, rest, err)
	}
	for _, bad := range [][]string{{"--port"}, {"--port", "x"}, {"--port", "70000"}} {
		if _, _, err := splitFlags(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestFollowPrintsLogLinesOnce(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Append("log", "", map[string]string{"line": "old"})
	sv.Append("log", "", map[string]string{"line": "seen"})
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- follow(ctx, hs.URL, testToken, 1, &out) }()
	time.Sleep(200 * time.Millisecond)
	sv.Append("log", "", map[string]string{"line": "live"})
	sv.Append("other", "", nil)
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	got := out.String()
	if strings.Contains(got, "old") || !strings.Contains(got, "seen") || !strings.Contains(got, "live") || strings.Count(got, "\n") != 2 {
		t.Fatalf("output:\n%s", got)
	}
}
