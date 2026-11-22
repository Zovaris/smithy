package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sizeSection(out string) string {
	i := strings.Index(out, "## Always-loaded files")
	if i < 0 {
		return ""
	}
	return out[i:]
}

func TestDriftReportsSizeOfAlwaysLoadedFiles(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q")
	commitFile(t, dir, "AGENTS.md", "# A\n\nline\n")
	gitIn(t, dir, "branch", "base")
	commitFile(t, dir, "x.go", "package x\n")
	sec := sizeSection(runDrift(t, dir, "base"))
	if !strings.Contains(sec, "AGENTS.md: 3 lines, 10 bytes") {
		t.Errorf("size of AGENTS.md not reported:\n%s", sec)
	}
	if strings.Contains(sec, "CLAUDE.md") || strings.Contains(sec, "STANDARDS.md") {
		t.Errorf("reported a file that does not exist:\n%s", sec)
	}
}

func TestDriftFlagsStandardsOverBudget(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q")
	mkdirAll(t, dir, "docs/agents")
	small := "# Standards\n" + strings.Repeat("- rule\n", standardsBudget)
	commitFile(t, dir, "docs/agents/STANDARDS.md", small)
	gitIn(t, dir, "branch", "base")
	commitFile(t, dir, "x.go", "package x\n")
	sec := sizeSection(runDrift(t, dir, "base"))
	if strings.Contains(sec, "over budget") {
		t.Errorf("flagged a standards file at its budget:\n%s", sec)
	}
	commitFile(t, dir, "docs/agents/STANDARDS.md", small+"- one more\n")
	sec = sizeSection(runDrift(t, dir, "base"))
	want := "docs/agents/STANDARDS.md: 32 lines"
	if !strings.Contains(sec, want) || !strings.Contains(sec, "31 rules, over budget (30)") {
		t.Errorf("over-budget standards not flagged:\n%s", sec)
	}
}

func mkdirAll(t *testing.T, dir, sub string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
		t.Fatal(err)
	}
}
