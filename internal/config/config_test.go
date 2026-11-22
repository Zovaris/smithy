package config

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type env struct {
	home string
	user string
	repo string
}

func setup(t *testing.T) env {
	t.Helper()
	root := t.TempDir()
	e := env{home: filepath.Join(root, "home"), user: filepath.Join(root, "user.json"), repo: filepath.Join(root, "repo")}
	for _, d := range []string{e.home, e.repo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", e.home)
	t.Setenv("SMITHY_HOME", filepath.Join(e.home, ".smithy"))
	t.Setenv("SMITHY_CONFIG", e.user)
	t.Setenv("WARDEN_HARNESS", "codex")
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Chdir(root)
	return e
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	t.Chdir(dir)
}

func write(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func capture(t *testing.T, f func() int) (string, string, int) {
	t.Helper()
	oo, oe := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	code := f()
	os.Stdout, os.Stderr = oo, oe
	wo.Close()
	we.Close()
	so, _ := io.ReadAll(ro)
	se, _ := io.ReadAll(re)
	return string(so), string(se), code
}

func TestEffectivePrecedence(t *testing.T) {
	e := setup(t)
	gitInit(t, e.repo)

	v, src, err := Effective("warden.max")
	if v != "5" || src != "default" || err != nil {
		t.Fatalf("default: %q %q %v", v, src, err)
	}
	write(t, e.user, `{"warden":{"max":5}}`)
	if v, src, _ = Effective("warden.max"); v != "5" || src != "user" {
		t.Fatalf("user: %q %q", v, src)
	}
	write(t, filepath.Join(e.repo, ".smithy", "config.json"), `{"warden":{"max":7}}`)
	if v, src, _ = Effective("warden.max"); v != "7" || src != "repo" {
		t.Fatalf("repo: %q %q", v, src)
	}
	if got := Get("warden.max", "x"); got != "7" {
		t.Fatalf("Get = %q", got)
	}
}

func TestEffectiveAskAndUnknown(t *testing.T) {
	setup(t)
	for _, k := range []string{"tasks.commit", "tasks.ignore", "no.such.key"} {
		if _, _, err := Effective(k); !errors.Is(err, ErrUnset) {
			t.Fatalf("%s: err = %v", k, err)
		}
	}
	if got := Get("tasks.commit", "fb"); got != "fb" {
		t.Fatalf("Get fallback = %q", got)
	}
}

func TestEffectiveValueTypes(t *testing.T) {
	e := setup(t)
	write(t, e.user, `{"a":{"s":"x","n":1.5,"i":3,"b":true,"l":[1,2],"z":null}}`)
	tests := map[string]string{"a.s": "x", "a.n": "1.5", "a.i": "3", "a.b": "true", "a.l": "[1,2]"}
	for k, want := range tests {
		if got, _, err := Effective(k); err != nil || got != want {
			t.Fatalf("%s = %q, %v; want %q", k, got, err, want)
		}
	}
	if _, _, err := Effective("a.z"); !errors.Is(err, ErrUnset) {
		t.Fatal("null should be unset")
	}
	if _, _, err := Effective("a.s.deeper"); !errors.Is(err, ErrUnset) {
		t.Fatal("descending into string should be unset")
	}
}

func TestEffectiveBrokenFileReported(t *testing.T) {
	e := setup(t)
	write(t, e.user, `{not json`)
	if _, _, err := Effective("warden.max"); err == nil || errors.Is(err, ErrUnset) || !strings.Contains(err.Error(), e.user) {
		t.Fatalf("err = %v", err)
	}
	if got := Get("warden.max", "fb"); got != "fb" {
		t.Fatalf("Get = %q", got)
	}
}

func TestLegacyRepoFile(t *testing.T) {
	e := setup(t)
	gitInit(t, e.repo)
	old := filepath.Join(e.repo, ".agents", "config.json")
	write(t, old, `{"warden":{"max":9}}`)
	if got, _ := filepath.EvalSymlinks(RepoFile()); got != mustEval(t, old) {
		t.Fatalf("RepoFile = %q", got)
	}
	if v, src, _ := Effective("warden.max"); v != "9" || src != "repo" {
		t.Fatalf("%q %q", v, src)
	}
	write(t, filepath.Join(e.repo, ".smithy", "config.json"), `{"warden":{"max":2}}`)
	if v, _, _ := Effective("warden.max"); v != "2" {
		t.Fatalf("new file should win, got %q", v)
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRepoFileOutsideRepo(t *testing.T) {
	setup(t)
	if got := RepoFile(); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestUserFile(t *testing.T) {
	e := setup(t)
	if UserFile() != e.user {
		t.Fatalf("got %q", UserFile())
	}
	t.Setenv("SMITHY_CONFIG", "")
	if got, want := UserFile(), filepath.Join(e.home, ".smithy", "config.json"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSet(t *testing.T) {
	e := setup(t)
	f := filepath.Join(e.home, "deep", "dir", "c.json")
	steps := []struct{ key, val, read, want string }{
		{"a.b.c", "1", "a.b.c", "1"},
		{"a.b.d", `"str"`, "a.b.d", "str"},
		{"a.b.e", "bare text", "a.b.e", "bare text"},
		{"a.list", `["x","y"]`, "a.list", `["x","y"]`},
		{"a.flag", "true", "a.flag", "true"},
		{"top", "v", "top", "v"},
		{"a.b.c", "2", "a.b.c", "2"},
		{"top.nested", "n", "top.nested", "n"},
	}
	for _, s := range steps {
		if err := Set(f, s.key, s.val); err != nil {
			t.Fatal(err)
		}
		if got, ok := readKey(f, s.read); !ok || got != s.want {
			t.Fatalf("%s = %q, %v; want %q", s.read, got, ok, s.want)
		}
	}
	if got, _ := readKey(f, "a.b.d"); got != "str" {
		t.Fatalf("sibling lost: %q", got)
	}
	if _, err := os.Stat(f + ".tmp"); err == nil {
		t.Fatal("tmp file left behind")
	}
	b, _ := os.ReadFile(f)
	if !strings.HasSuffix(string(b), "}\n") {
		t.Fatalf("want trailing newline: %q", b)
	}
	blocker := filepath.Join(e.home, "blocker")
	write(t, blocker, "x")
	if err := Set(filepath.Join(blocker, "c.json"), "k", "v"); err == nil {
		t.Fatal("want mkdir error")
	}
}

func TestModel(t *testing.T) {
	e := setup(t)
	for _, h := range []string{"claude-cli", "codex", "unknown"} {
		t.Setenv("WARDEN_HARNESS", h)
		if _, err := Model("cheap"); !errors.Is(err, ErrUnset) {
			t.Fatalf("%s: err = %v", h, err)
		}
	}
	write(t, e.user, `{"models":{"codex":{"cheap":"mini"},"claude-cli":{"cheap":"haiku-x"}}}`)
	t.Setenv("WARDEN_HARNESS", "codex")
	if m, err := Model("cheap"); err != nil || m != "mini" {
		t.Fatalf("%q %v", m, err)
	}
	if _, err := Model("mid"); !errors.Is(err, ErrUnset) {
		t.Fatal("mid should be unset")
	}
	t.Setenv("WARDEN_HARNESS", "claude-cli")
	if m, _ := Model("cheap"); m != "haiku-x" {
		t.Fatalf("got %q", m)
	}
}

func TestMigrate(t *testing.T) {
	e := setup(t)
	if got := Migrate(); len(got) != 0 {
		t.Fatalf("nothing to move: %v", got)
	}
	old := filepath.Join(e.home, ".agents")
	write(t, filepath.Join(old, "config.json"), `{"x":1}`)
	write(t, filepath.Join(old, "plans", "p.md"), "p")
	write(t, filepath.Join(old, "foreman", "b", "a.json"), "{}")
	write(t, filepath.Join(old, "tend", "t"), "t")
	smithy := filepath.Join(e.home, ".smithy")
	write(t, filepath.Join(smithy, "tend", "keep"), "k")

	moved := Migrate()
	if len(moved) != 3 {
		t.Fatalf("moved = %v", moved)
	}
	for _, p := range []string{"config.json", "plans/p.md", "warden/b/a.json", "tend/keep"} {
		if !exists(filepath.Join(smithy, p)) {
			t.Fatalf("missing %s", p)
		}
	}
	if !exists(filepath.Join(old, "tend", "t")) {
		t.Fatal("existing destination must not be overwritten")
	}
	if exists(filepath.Join(old, "config.json")) {
		t.Fatal("source should be moved")
	}
}

func TestAutoMigrate(t *testing.T) {
	t.Run("runs once", func(t *testing.T) {
		e := setup(t)
		t.Setenv("SMITHY_CONFIG", "")
		write(t, filepath.Join(e.home, ".agents", "config.json"), `{}`)
		_, se, _ := capture(t, func() int { AutoMigrate(); return 0 })
		if !strings.Contains(se, "moved") {
			t.Fatalf("stderr = %q", se)
		}
		if !exists(filepath.Join(e.home, ".smithy", "config.json")) {
			t.Fatal("not migrated")
		}
		_, se, _ = capture(t, func() int { AutoMigrate(); return 0 })
		if se != "" {
			t.Fatalf("second run: %q", se)
		}
	})
	t.Run("skipped with SMITHY_CONFIG", func(t *testing.T) {
		e := setup(t)
		write(t, filepath.Join(e.home, ".agents", "config.json"), `{}`)
		AutoMigrate()
		if exists(filepath.Join(e.home, ".smithy")) {
			t.Fatal("should not migrate")
		}
	})
	t.Run("skipped when smithy home exists", func(t *testing.T) {
		e := setup(t)
		t.Setenv("SMITHY_CONFIG", "")
		write(t, filepath.Join(e.home, ".agents", "config.json"), `{}`)
		write(t, filepath.Join(e.home, ".smithy", "x"), "")
		AutoMigrate()
		if exists(filepath.Join(e.home, ".smithy", "config.json")) {
			t.Fatal("should not migrate")
		}
	})
	t.Run("skipped without old config", func(t *testing.T) {
		e := setup(t)
		t.Setenv("SMITHY_CONFIG", "")
		AutoMigrate()
		if exists(filepath.Join(e.home, ".smithy")) {
			t.Fatal("should not create")
		}
	})
}

func TestMain_(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		harness  string
		code     int
		outHas   string
		errHas   string
		wantUser string
	}{
		{name: "no args", code: 2, errHas: "usage: smithy config"},
		{name: "unknown", args: []string{"bogus"}, code: 2, errHas: "usage: smithy config"},
		{name: "model bad tier", args: []string{"model", "huge"}, code: 2, errHas: "usage"},
		{name: "model missing tier", args: []string{"model"}, code: 2, errHas: "usage"},
		{name: "model unset", args: []string{"model", "cheap"}, code: 3},
		{name: "set-model bad", args: []string{"set-model", "cheap"}, code: 2, errHas: "usage"},
		{name: "set-model", args: []string{"set-model", "cheap", "mini"}, code: 0, outHas: "saved models.codex.cheap = mini", wantUser: "mini"},
		{name: "get no arg", args: []string{"get"}, code: 2},
		{name: "get unset", args: []string{"get", "tasks.commit"}, code: 3},
		{name: "get default", args: []string{"get", "warden.cleanup"}, code: 0, outHas: "auto\n"},
		{name: "get merge asks", args: []string{"get", "warden.merge"}, code: 3},
		{name: "set usage", args: []string{"set", "onlykey"}, code: 2, errHas: "usage: smithy config set"},
		{name: "set", args: []string{"set", "warden.max", "4"}, code: 0, outHas: "saved warden.max = 4"},
		{name: "set --repo outside repo", args: []string{"set", "--repo", "k", "v"}, code: 2, errHas: "not in a git repo"},
		{name: "migrate nothing", args: []string{"migrate"}, code: 0, outHas: "nothing to migrate"},
		{name: "list", args: []string{"list"}, code: 0, outHas: "models.cheap"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := setup(t)
			if tc.harness != "" {
				t.Setenv("WARDEN_HARNESS", tc.harness)
			}
			so, se, code := capture(t, func() int { return Main(tc.args) })
			if code != tc.code {
				t.Fatalf("code = %d want %d (out %q err %q)", code, tc.code, so, se)
			}
			if !strings.Contains(so, tc.outHas) || !strings.Contains(se, tc.errHas) {
				t.Fatalf("out %q err %q", so, se)
			}
			if tc.wantUser != "" {
				if got, _ := readKey(e.user, "models.codex.cheap"); got != tc.wantUser {
					t.Fatalf("user file model = %q", got)
				}
			}
		})
	}
}

func TestMainSetModelHarnessArg(t *testing.T) {
	e := setup(t)
	_, _, code := capture(t, func() int { return Main([]string{"set-model", "mid", "big", "pi"}) })
	if code != 0 {
		t.Fatal(code)
	}
	if got, _ := readKey(e.user, "models.pi.mid"); got != "big" {
		t.Fatalf("got %q", got)
	}
	so, _, code := capture(t, func() int { return Main([]string{"model", "mid"}) })
	if code != 3 || so != "" {
		t.Fatal("codex mid should stay unset")
	}
	t.Setenv("WARDEN_HARNESS", "pi")
	so, _, code = capture(t, func() int { return Main([]string{"model", "mid"}) })
	if code != 0 || so != "big\n" {
		t.Fatalf("%q %d", so, code)
	}
}

func TestMainSetRepo(t *testing.T) {
	e := setup(t)
	gitInit(t, e.repo)
	so, _, code := capture(t, func() int { return Main([]string{"set", "--repo", "warden.max", "8"}) })
	if code != 0 || !strings.Contains(so, "saved warden.max = 8") {
		t.Fatalf("%d %q", code, so)
	}
	if got, _ := readKey(filepath.Join(e.repo, ".smithy", "config.json"), "warden.max"); got != "8" {
		t.Fatalf("got %q", got)
	}
	if v, src, _ := Effective("warden.max"); v != "8" || src != "repo" {
		t.Fatalf("%q %q", v, src)
	}
}

func TestMainSetRepoLegacyRedirect(t *testing.T) {
	e := setup(t)
	gitInit(t, e.repo)
	old := filepath.Join(e.repo, ".agents", "config.json")
	write(t, old, `{"warden":{"max":9}}`)
	_, _, code := capture(t, func() int { return Main([]string{"set", "--repo", "warden.cleanup", "manual"}) })
	if code != 0 {
		t.Fatal(code)
	}
	if got, _ := readKey(filepath.Join(e.repo, ".smithy", "config.json"), "warden.cleanup"); got != "manual" {
		t.Fatalf("got %q", got)
	}
	if b, _ := os.ReadFile(old); strings.Contains(string(b), "cleanup") {
		t.Fatal("legacy file must not be written")
	}
}

func TestMainMigrate(t *testing.T) {
	e := setup(t)
	write(t, filepath.Join(e.home, ".agents", "plans", "p"), "p")
	so, _, code := capture(t, func() int { return Main([]string{"migrate"}) })
	if code != 0 || !strings.Contains(so, "moved ") {
		t.Fatalf("%d %q", code, so)
	}
}

func TestMainListSources(t *testing.T) {
	e := setup(t)
	write(t, e.user, `{"models":{"codex":{"cheap":"mini"}},"warden":{"max":6}}`)
	so, _, code := capture(t, func() int { return Main([]string{"list"}) })
	if code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{"mini", "user", "unset", "warden.max", "tasks.commit"} {
		if !strings.Contains(so, want) {
			t.Fatalf("missing %q in %q", want, so)
		}
	}
	if n := strings.Count(so, "\n"); n != 4+len(Known) {
		t.Fatalf("lines = %d", n)
	}
}

func TestMainSetModelWriteError(t *testing.T) {
	e := setup(t)
	blocker := filepath.Join(e.home, "blocker")
	write(t, blocker, "x")
	t.Setenv("SMITHY_CONFIG", filepath.Join(blocker, "c.json"))
	_, se, code := capture(t, func() int { return Main([]string{"set-model", "cheap", "m"}) })
	if code != 1 || se == "" {
		t.Fatalf("%d %q", code, se)
	}
	_, _, code = capture(t, func() int { return Main([]string{"set", "k", "v"}) })
	if code != 1 {
		t.Fatal(code)
	}
}

func TestSetEffortIsCapped(t *testing.T) {
	setup(t)
	if _, er, c := capture(t, func() int { return Main([]string{"config", "set", "agent.effort", "high"}[1:]) }); c != 0 || !strings.Contains(er, "capped at medium") {
		t.Fatalf("high: %d %q", c, er)
	}
	if v, _, _ := Effective("agent.effort"); v != "medium" {
		t.Fatalf("stored %q, want medium", v)
	}
	if _, _, c := capture(t, func() int { return Main([]string{"set", "agent.effort", "low"}) }); c != 0 {
		t.Fatalf("low: %d", c)
	}
	if v, _, _ := Effective("agent.effort"); v != "low" {
		t.Fatalf("stored %q, want low", v)
	}
	if _, er, c := capture(t, func() int { return Main([]string{"set", "agent.effort", "fast"}) }); c != 2 || !strings.Contains(er, "low or medium") {
		t.Fatalf("bogus: %d %q", c, er)
	}
}

func TestUnset(t *testing.T) {
	e := setup(t)
	capture(t, func() int { return Main([]string{"set", "agent.effort", "low"}) })
	if out, _, c := capture(t, func() int { return Main([]string{"unset", "agent.effort"}) }); c != 0 || !strings.Contains(out, "removed agent.effort") {
		t.Fatalf("unset: %d %q", c, out)
	}
	if v, src, _ := Effective("agent.effort"); v != "medium" || src != "default" {
		t.Fatalf("after unset %q from %q, want the default", v, src)
	}
	if out, _, c := capture(t, func() int { return Main([]string{"unset", "agent.effort"}) }); c != 0 || !strings.Contains(out, "was not set") {
		t.Fatalf("unset twice: %d %q", c, out)
	}
	if ok, err := Unset(e.user, "no.such.key"); ok || err != nil {
		t.Fatalf("missing nested key: %v %v", ok, err)
	}
	if _, _, c := capture(t, func() int { return Main([]string{"unset"}) }); c != 2 {
		t.Fatalf("no key: %d", c)
	}
}

func TestSetRefusesUnreadableFile(t *testing.T) {
	e := setup(t)
	write(t, e.user, "not json")
	if err := Set(e.user, "agent.effort", "low"); err == nil {
		t.Fatal("overwrote a file that isn't JSON")
	}
	if b, _ := os.ReadFile(e.user); string(b) != "not json" {
		t.Fatalf("file changed: %q", b)
	}
}

func TestRepoFileInWorktreeIsTheMainCheckouts(t *testing.T) {
	main := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run(main, "init", "-q")
	run(main, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "x")
	wt := filepath.Join(t.TempDir(), "wt")
	run(main, "worktree", "add", "-q", wt)
	t.Chdir(wt)
	got, _ := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(RepoFile())))
	want, _ := filepath.EvalSymlinks(main)
	if got != want {
		t.Fatalf("RepoFile in worktree = %s, want under %s", RepoFile(), main)
	}
}
