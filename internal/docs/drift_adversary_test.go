package docs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func advPut(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", name)
	gitIn(t, dir, "commit", "-q", "-m", name)
}

func advSection(out string) string {
	i := strings.Index(out, "## Always-loaded files")
	if i < 0 {
		return ""
	}
	rest := out[i:]
	if j := strings.Index(rest[3:], "\n## "); j >= 0 {
		rest = rest[:j+3]
	}
	return rest
}

func advRules(n int) string {
	return "# Standards\n" + strings.Repeat("- rule\n", n)
}

func TestAdvAgentsMdSizeLine(t *testing.T) {
	dir := staleCloneOnFeature(t)
	advPut(t, dir, "AGENTS.md", "a\nb\nc\n")
	out := runDrift(t, dir, "main")
	if !strings.Contains(advSection(out), "AGENTS.md: 3 lines, 6 bytes") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestAdvAllFourFiles(t *testing.T) {
	dir := staleCloneOnFeature(t)
	advPut(t, dir, "AGENTS.md", "x\n")
	advPut(t, dir, "CLAUDE.md", "x\ny\n")
	advPut(t, dir, "docs/agents/STANDARDS.md", "- a\n- b\n")
	advPut(t, dir, "scripts/SCRIPTS.md", "s\n")
	sec := advSection(runDrift(t, dir, "main"))
	for _, w := range []string{"AGENTS.md: 1 lines, 2 bytes", "CLAUDE.md: 2 lines, 4 bytes", "docs/agents/STANDARDS.md: 2 lines, 8 bytes, 2 rules", "scripts/SCRIPTS.md: 1 lines, 2 bytes"} {
		if !strings.Contains(sec, w) {
			t.Errorf("missing %q in:\n%s", w, sec)
		}
	}
}

func TestAdvBudgetBoundary(t *testing.T) {
	for _, c := range []struct {
		n    int
		over bool
	}{{0, false}, {29, false}, {30, false}, {31, true}, {100, true}} {
		dir := staleCloneOnFeature(t)
		advPut(t, dir, "docs/agents/STANDARDS.md", advRules(c.n))
		sec := advSection(runDrift(t, dir, "main"))
		if !strings.Contains(sec, ", "+strconv.Itoa(c.n)+" rules") {
			t.Errorf("n=%d no rule count:\n%s", c.n, sec)
		}
		if got := strings.Contains(sec, ", over budget (30)"); got != c.over {
			t.Errorf("n=%d over=%v want %v:\n%s", c.n, got, c.over, sec)
		}
	}
}

func TestAdvOnlyDashSpaceLinesAreRules(t *testing.T) {
	dir := staleCloneOnFeature(t)
	text := "# T\n-nospace\n--- \n* star\n1. num\n-\n- real\ntext - not rule\n"
	advPut(t, dir, "docs/agents/STANDARDS.md", text)
	sec := advSection(runDrift(t, dir, "main"))
	if !strings.Contains(sec, ", 1 rules") {
		t.Fatalf("want exactly 1 rule (`- real`):\n%s", sec)
	}
}

func TestAdvMissingFilesNotListed(t *testing.T) {
	dir := staleCloneOnFeature(t)
	advPut(t, dir, "AGENTS.md", "x\n")
	sec := advSection(runDrift(t, dir, "main"))
	for _, n := range []string{"CLAUDE.md", "STANDARDS.md", "SCRIPTS.md"} {
		if strings.Contains(sec, n) {
			t.Errorf("%s listed though absent:\n%s", n, sec)
		}
	}
}

func TestAdvEmptyFile(t *testing.T) {
	dir := staleCloneOnFeature(t)
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "e")
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	sec := advSection(runDrift(t, dir, "main"))
	if !strings.Contains(sec, "CLAUDE.md: 0 lines, 0 bytes") {
		t.Fatalf("empty:\n%s", sec)
	}
}

func TestAdvBytesAreBytesNotRunes(t *testing.T) {
	dir := staleCloneOnFeature(t)
	advPut(t, dir, "AGENTS.md", "héé🙂\n") // 1+2+2+4+1 = 10
	sec := advSection(runDrift(t, dir, "main"))
	if !strings.Contains(sec, "AGENTS.md: 1 lines, 10 bytes") {
		t.Fatalf("unicode:\n%s", sec)
	}
}

func TestAdvCRLFLines(t *testing.T) {
	dir := staleCloneOnFeature(t)
	advPut(t, dir, "docs/agents/STANDARDS.md", "- a\r\n- b\r\n")
	sec := advSection(runDrift(t, dir, "main"))
	if !strings.Contains(sec, "2 lines, 10 bytes, 2 rules") {
		t.Fatalf("crlf:\n%s", sec)
	}
}

func TestAdvNoTrailingNewline(t *testing.T) {
	// spec-ambiguous: 2 lines is the human reading of "a\nb"; wc -l would say 1.
	dir := staleCloneOnFeature(t)
	advPut(t, dir, "AGENTS.md", "a\nb")
	sec := advSection(runDrift(t, dir, "main"))
	if !strings.Contains(sec, "AGENTS.md: 2 lines, 3 bytes") {
		t.Fatalf("no trailing newline:\n%s", sec)
	}
}

func TestAdvRulesOnlyOnStandards(t *testing.T) {
	dir := staleCloneOnFeature(t)
	advPut(t, dir, "AGENTS.md", advRules(40))
	sec := advSection(runDrift(t, dir, "main"))
	if strings.Contains(sec, "rules") || strings.Contains(sec, "over budget") {
		t.Fatalf("rules/budget leaked onto AGENTS.md:\n%s", sec)
	}
}

func TestAdvWorksWithoutBaseArg(t *testing.T) {
	dir := staleCloneOnFeature(t)
	advPut(t, dir, "AGENTS.md", "a\n")
	sec := advSection(runDrift(t, dir))
	if !strings.Contains(sec, "AGENTS.md: 1 lines, 2 bytes") {
		t.Fatalf("default base:\n%s", sec)
	}
}

func TestAdvUncommittedChangeReflected(t *testing.T) {
	dir := staleCloneOnFeature(t)
	advPut(t, dir, "AGENTS.md", "a\n")
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sec := advSection(runDrift(t, dir, "main"))
	if !strings.Contains(sec, "AGENTS.md: 3 lines, 6 bytes") {
		t.Fatalf("working tree size expected:\n%s", sec)
	}
}
