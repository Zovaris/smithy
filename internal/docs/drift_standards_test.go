package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/testkit"
)

func TestDriftReportsMissingPathInStandards(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testkit.Git(t, dir, "init", "-q")
	if err := os.MkdirAll(filepath.Join(dir, "docs", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.CommitFile(t, dir, "src/keep.go", "package x\n")
	testkit.CommitFile(t, dir, "docs/agents/STANDARDS.md", "# Standards\n\n- Wrap errors — covers: `src/keep.go`, `src/gone.go` — from: PR 1\n")
	testkit.Git(t, dir, "branch", "base")
	testkit.CommitFile(t, dir, "src/keep.go", "package x\n\nvar A = 1\n")
	out := runDrift(t, dir, "base")
	sec := out[strings.Index(out, "## Missing references"):strings.Index(out, "## Public API")]
	if !strings.Contains(sec, "docs/agents/STANDARDS.md") || !strings.Contains(sec, "`src/gone.go`") {
		t.Errorf("missing path not reported:\n%s", sec)
	}
	if strings.Contains(sec, "`src/keep.go`") {
		t.Errorf("flagged an existing path:\n%s", sec)
	}
}
