package ghx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fakeGH(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSkillDir(t *testing.T) {
	t.Setenv("SMITHY_SKILL_DIR", "")
	if SkillDir() != "" {
		t.Fatal("want empty")
	}
	t.Setenv("SMITHY_SKILL_DIR", "/s")
	if SkillDir() != "/s" {
		t.Fatal("want /s")
	}
}

func TestRepoRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	plain := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))
	if got := RepoRoot(plain); got != "" {
		t.Fatalf("outside repo: got %q", got)
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(repo)
	for _, dir := range []string{repo, sub} {
		got, _ := filepath.EvalSymlinks(RepoRoot(dir))
		if got != want {
			t.Fatalf("RepoRoot(%s) = %q want %q", dir, got, want)
		}
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		want    string
		wantErr string
	}{
		{"stdout trimmed", "printf '  hi  \\n\\n'", "hi", ""},
		{"stderr first line", "printf 'boom\\nsecond\\n' >&2; exit 1", "", "boom"},
		{"stderr trimmed", "printf '  only  ' >&2; exit 1", "", "only"},
		{"no stderr uses exit error", "exit 3", "", "exit status 3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Run("sh", "-c", tc.script)
			if tc.wantErr == "" {
				if err != nil || got != tc.want {
					t.Fatalf("got %q, %v", got, err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
		})
	}
	if _, err := Run("definitely-not-a-command-xyz"); err == nil {
		t.Fatal("want error for missing command")
	}
}

func TestGH(t *testing.T) {
	t.Run("decodes json", func(t *testing.T) {
		fakeGH(t, `echo '{"number":5,"state":"OPEN"}'`)
		var v struct {
			Number int
			State  string
		}
		if err := GH(&v, "pr", "view"); err != nil {
			t.Fatal(err)
		}
		if v.Number != 5 || v.State != "OPEN" {
			t.Fatalf("got %+v", v)
		}
	})
	t.Run("passes args", func(t *testing.T) {
		fakeGH(t, `printf '"%s"' "$*"`)
		var s string
		if err := GH(&s, "pr", "list", "--json", "x"); err != nil {
			t.Fatal(err)
		}
		if s != "pr list --json x" {
			t.Fatalf("got %q", s)
		}
	})
	t.Run("empty output is null", func(t *testing.T) {
		fakeGH(t, `exit 0`)
		v := []int{1}
		if err := GH(&v); err != nil {
			t.Fatal(err)
		}
		if v != nil {
			t.Fatalf("got %v", v)
		}
	})
	t.Run("stderr error", func(t *testing.T) {
		fakeGH(t, `echo 'gh: not logged in' >&2; echo more >&2; exit 4`)
		var v any
		err := GH(&v, "auth", "status")
		if err == nil || err.Error() != "gh: not logged in" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("bad json", func(t *testing.T) {
		fakeGH(t, `echo 'not json'`)
		var v any
		if err := GH(&v); err == nil || !strings.Contains(err.Error(), "invalid") {
			t.Fatalf("err = %v", err)
		}
	})
}
