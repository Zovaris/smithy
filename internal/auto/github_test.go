package auto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeGH = `#!/bin/sh
D="$FAKE_GH"
echo "$@" >> "$D/calls"
case "$1 $2" in
  "auth status") exit 0 ;;
  "issue list") cat "$D/issues.json" ;;
  "issue comment") exit 0 ;;
esac
`

func fakeGitHub(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GH", root)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	return root
}

func TestGitHubPollAndReport(t *testing.T) {
	d := fakeGitHub(t)
	os.WriteFile(filepath.Join(d, "issues.json"), []byte(`[{"number":7,"url":"https://github.com/o/r/issues/7","title":"T","body":"B","labels":[{"name":"auto"},{"name":"model:mid"}]}]`), 0o644)
	g := GitHub{Settings: defaults()}
	is, err := g.Poll(at("2026-01-01 10:00")())
	if err != nil || len(is) != 1 || is[0].ID != "7" || is[0].Labels[1] != "model:mid" || is[0].Body != "B" {
		t.Fatalf("poll = %+v %v", is, err)
	}
	if err := g.Report("7", "completed", "https://github.com/o/r/pull/9"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, "calls"))
	calls := string(b)
	if !strings.Contains(calls, "--label auto") {
		t.Errorf("labeled mode should filter by label: %s", calls)
	}
	if !strings.Contains(calls, "issue comment 7 --body smithy auto: done. PR: https://github.com/o/r/pull/9") {
		t.Errorf("comment call: %s", calls)
	}
	if err := g.Report("ENG-1", "started", ""); err == nil {
		t.Error("non-numeric id accepted")
	}
}
