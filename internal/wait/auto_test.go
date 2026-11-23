package wait

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWaitAutoWakesOnlyForNewQualifiedIssues(t *testing.T) {
	e := sandbox(t)
	e.put(t, "issues.json", `[{"number":3,"url":"https://github.com/o/r/issues/3","title":"T","body":"","labels":[{"name":"auto"},{"name":"speed:fast"}]}]`)
	out, _, code := mainOut(t, "auto", "--max", "0.001", "--every", "0.01")
	if code != 0 || !strings.Contains(out, "auto github 3 queued - fast https://github.com/o/r/issues/3") {
		t.Fatalf("first wait: %d %q", code, out)
	}
	out, _, code = mainOut(t, "auto", "--max", "0.001", "--every", "0.01")
	if code != 4 || strings.Contains(out, "queued") {
		t.Errorf("second wait should heartbeat: %d %q", code, out)
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("SMITHY_HOME"), "auto", "state.json"))
	if err != nil || !strings.Contains(string(b), `"last_poll"`) || !strings.Contains(string(b), `"github"`) {
		t.Errorf("poll time not recorded: %s %v", b, err)
	}
}
