package search

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	root := t.TempDir()
	write(t, root, "a.go", "package a\nfunc Needle() {}\n")
	write(t, root, "sub/b.txt", "no match\nneedle here\nNeedle again\n")
	write(t, root, ".git/config", "Needle in git dir\n")
	write(t, root, "bin.dat", "Needle\x00binary\n")
	return root
}

func paths(ms []Match) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Path)
	}
	return out
}

func TestWalkFindsMatchesSkipsGitAndBinary(t *testing.T) {
	root := fixture(t)
	r, err := Run(Options{Root: root, Pattern: "Needle", Engine: EngineWalk})
	if err != nil {
		t.Fatal(err)
	}
	if r.Engine != EngineWalk {
		t.Fatalf("engine %s", r.Engine)
	}
	got := strings.Join(paths(r.Matches), ",")
	if got != "a.go,sub/b.txt" {
		t.Fatalf("paths %q", got)
	}
	if r.Matches[1].Line != 3 || r.Matches[1].Text != "Needle again" {
		t.Fatalf("match %+v", r.Matches[1])
	}
}

func TestFixedAndIgnoreCase(t *testing.T) {
	root := t.TempDir()
	write(t, root, "x.txt", "a.b\naxb\nA.B\n")
	r, _ := Run(Options{Root: root, Pattern: "a.b", Fixed: true, Engine: EngineWalk})
	if len(r.Matches) != 1 {
		t.Fatalf("fixed %+v", r.Matches)
	}
	r, _ = Run(Options{Root: root, Pattern: "a.b", Fixed: true, IgnoreCase: true, Engine: EngineWalk})
	if len(r.Matches) != 2 {
		t.Fatalf("ignore case %+v", r.Matches)
	}
}

func TestCaps(t *testing.T) {
	root := t.TempDir()
	write(t, root, "x.txt", strings.Repeat("hit "+strings.Repeat("y", 500)+"\n", 50))
	r, _ := Run(Options{Root: root, Pattern: "hit", MaxMatches: 5, Engine: EngineWalk})
	if len(r.Matches) != 5 || !r.Truncated {
		t.Fatalf("max matches: %d %v", len(r.Matches), r.Truncated)
	}
	if n := len([]rune(r.Matches[0].Text)); n > DefaultMaxLine+1 {
		t.Fatalf("line not capped: %d", n)
	}
	r, _ = Run(Options{Root: root, Pattern: "hit", MaxLine: 10, MaxBytes: 100, Engine: EngineWalk})
	if !r.Truncated || r.Bytes() > 100 {
		t.Fatalf("max bytes: %d %v", r.Bytes(), r.Truncated)
	}
}

func TestBadPattern(t *testing.T) {
	if _, err := Run(Options{Root: t.TempDir(), Pattern: "(", Engine: EngineWalk}); err == nil {
		t.Fatal("want error")
	}
	if _, err := Run(Options{Root: t.TempDir(), Pattern: ""}); err == nil {
		t.Fatal("want error for empty pattern")
	}
}

func gitRepo(t *testing.T) string {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := fixture(t)
	os.RemoveAll(filepath.Join(root, ".git"))
	write(t, root, ".gitignore", "ignored/\n")
	write(t, root, "ignored/c.txt", "Needle ignored\n")
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	return root
}

func TestGitEngineHonoursIgnore(t *testing.T) {
	root := gitRepo(t)
	r, err := Run(Options{Root: root, Pattern: "Needle"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Engine == EngineWalk {
		t.Fatalf("repo searched with walker")
	}
	for _, m := range r.Matches {
		if strings.HasPrefix(m.Path, "ignored/") {
			t.Fatalf("ignored file searched: %+v", m)
		}
	}
	r, _ = Run(Options{Root: root, Pattern: "Needle", Engine: EngineGit})
	if strings.Join(paths(r.Matches), ",") != "a.go,sub/b.txt" {
		t.Fatalf("git paths %v", paths(r.Matches))
	}
}

func TestRgEngineMatchesWalk(t *testing.T) {
	rg, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("no rg on PATH")
	}
	root := gitRepo(t)
	r, err := Run(Options{Root: root, Pattern: "needle", IgnoreCase: true, Rg: rg})
	if err != nil {
		t.Fatal(err)
	}
	if r.Engine != EngineRg {
		t.Fatalf("engine %s", r.Engine)
	}
	if strings.Join(paths(r.Matches), ",") != "a.go,sub/b.txt,sub/b.txt" {
		t.Fatalf("rg paths %v", paths(r.Matches))
	}
	r, _ = Run(Options{Root: root, Pattern: "Needle", MaxMatches: 1, Rg: rg})
	if len(r.Matches) != 1 || !r.Truncated {
		t.Fatalf("rg cap %+v", r)
	}
}

func TestFindRg(t *testing.T) {
	dir := t.TempDir()
	name := "rg"
	if exeSuffix != "" {
		name += exeSuffix
	}
	write(t, dir, name, "#!/bin/sh\n")
	os.Chmod(filepath.Join(dir, name), 0o755)
	t.Setenv("SMITHY_RG", "")
	if got := FindRg(dir); got != filepath.Join(dir, name) {
		t.Fatalf("next to binary: %q", got)
	}
	t.Setenv("SMITHY_RG", "off")
	if got := FindRg(dir); got != "" {
		t.Fatalf("off: %q", got)
	}
}
