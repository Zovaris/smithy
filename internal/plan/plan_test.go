package plan

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/config"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "init.defaultBranch=main"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stackMap(lines []Line) map[string]string {
	m := map[string]string{}
	for _, l := range lines {
		m[l.Key] = l.Value
	}
	return m
}

func TestStack(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  map[string]string
	}{
		{"js", map[string]string{
			"package.json":   `{"scripts":{"test":"vitest","lint":"eslint .","typecheck":""},"devDependencies":{"vitest":"1","@playwright/test":"1","fast-check":"3"}}`,
			"tsconfig.json":  "{}",
			"pnpm-lock.yaml": "",
			"docs/adr/1.md":  "", "docs/adr/2.md": "", "docs/adr/n.txt": "",
		}, map[string]string{
			"lang": "js ts", "pkg-manager": "pnpm", "test": "pnpm test", "single": "npx vitest run <file>",
			"typecheck": "npx tsc --noEmit", "lint": "pnpm run lint", "e2e": "npx playwright test",
			"property": "fast-check", "coverage": "npx vitest run --coverage --changed", "dev": "", "adrs": "2",
		}},
		{"python", map[string]string{
			"pyproject.toml":       "[project]\ndependencies = [\"Mypy\", \"ruff\"]\n# pyright\n",
			"requirements-dev.txt": "hypothesis\ncosmic-ray\n",
			"poetry.lock":          "",
		}, map[string]string{
			"lang": "python", "test": "poetry run pytest -q", "typecheck": "poetry run mypy .",
			"lint": "poetry run ruff check .", "mutation": "poetry run cosmic-ray (see its config)", "property": "hypothesis",
		}},
		{"rust", map[string]string{"Cargo.toml": "[dev-dependencies]\nproptest = \"1\"\n"}, map[string]string{
			"lang": "rust", "test": "cargo test", "lint": "cargo clippy", "property": "proptest",
		}},
		{"go", map[string]string{"go.mod": "module x\n", "CLAUDE.md": ""}, map[string]string{
			"lang": "go", "test": "go test -count=1 ./...", "typecheck": "go vet ./...", "doc": "CLAUDE.md",
		}},
		{"bun over yarn, jest", map[string]string{"package.json": `{"dependencies":{"jest":"1"}}`, "yarn.lock": "", "bun.lock": ""}, map[string]string{
			"pkg-manager": "bun", "test": "", "single": "npx jest <file>",
		}},
		{"bun runs the test script", map[string]string{"package.json": `{"scripts":{"test":"vitest run"},"devDependencies":{"vitest":"1"}}`, "bun.lock": ""}, map[string]string{
			"pkg-manager": "bun", "test": "bun run test",
		}},
		{"empty", nil, map[string]string{"lang": "unknown", "test": "", "base": ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, body := range c.files {
				write(t, filepath.Join(dir, f), body)
			}
			got := stackMap(Stack(dir))
			for k, v := range c.want {
				if g, ok := got[k]; !ok && v != "" || g != v {
					t.Errorf("%s = %q, want %q", k, g, v)
				}
			}
		})
	}
}

func TestStackBase(t *testing.T) {
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare")
	dir := newRepo(t)
	git(t, dir, "remote", "add", "origin", remote)
	git(t, dir, "push", "-q", "origin", "HEAD:main")
	git(t, dir, "remote", "set-head", "origin", "main")
	if got := stackMap(Stack(dir))["base"]; got != "main" {
		t.Errorf("base = %q, want main", got)
	}
}

func TestSource(t *testing.T) {
	in := []string{
		"src/a.ts", "src/a.ts", "src/b.test.ts", "src/c.py", "src/test_c.py", "pkg/x_test.go", "pkg/x.go",
		"tests/t.ts", "lib/__tests__/q.js", "dist/o.js", "build/o.js", "gen/generated/g.go", "types/x.d.ts",
		"README.md", "fixtures/f.rb", "src/e.spec.tsx", "src/p.property.ts", "app/Main.kt", "",
	}
	want := []string{"app/Main.kt", "pkg/x.go", "src/a.ts", "src/c.py"}
	if got := Source(in); !reflect.DeepEqual(got, want) {
		t.Errorf("Source = %v, want %v", got, want)
	}
}

func TestChanged(t *testing.T) {
	dir := newRepo(t)
	base := git(t, dir, "rev-parse", "HEAD")
	for _, f := range []string{"src/a.ts", "src/a.test.ts", "docs/x.md", "dist/b.js"} {
		write(t, filepath.Join(dir, f), "1\n")
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "x")
	write(t, filepath.Join(dir, "src/b.go"), "1\n")
	git(t, dir, "add", "src/b.go")
	git(t, dir, "commit", "-q", "-m", "y")
	write(t, filepath.Join(dir, "src/b.go"), "2\n")
	write(t, filepath.Join(dir, "src/untracked.go"), "1\n")
	t.Chdir(dir)
	if got, want := Changed(base), []string{"src/a.ts", "src/b.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Changed = %v, want %v", got, want)
	}
	if got, want := Changed("HEAD"), []string{"src/b.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Changed = %v, want %v", got, want)
	}
}

func TestGuard(t *testing.T) {
	dir := newRepo(t)
	t.Chdir(t.TempDir())
	hook := func(path string) string {
		return `{"cwd":"` + dir + `","tool_input":{"file_path":"` + path + `"}}`
	}
	run := func(args []string, stdin string) (int, string) {
		var stderr bytes.Buffer
		code := Guard(args, strings.NewReader(stdin), &stderr)
		return code, stderr.String()
	}
	if code, _ := run(nil, hook("tests/a.ts")); code != 0 {
		t.Errorf("not green: exit %d, want 0", code)
	}
	write(t, filepath.Join(dir, ".smelt", "green"), "")
	for path, want := range map[string]int{
		"tests/a.ts": 2, "src/a.test.ts": 2, "test_x.py": 2, "pkg/x_test.go": 2, "a/__snapshots__/s": 2,
		"src/e2e/x.ts": 2, "src/a.ts": 0, "spectacle/x.ts": 0, "src/a.spec.TS": 0,
	} {
		code, msg := run(nil, hook(path))
		if code != want {
			t.Errorf("%s: exit %d, want %d", path, code, want)
		}
		if want == 2 && !strings.Contains(msg, "green phase") {
			t.Errorf("%s: message %q", path, msg)
		}
	}
	if code, _ := run(nil, "garbage"); code != 0 {
		t.Errorf("bad JSON outside the repo: exit %d, want 0", code)
	}
	t.Chdir(dir)
	t.Setenv("PWD", dir)
	if code, _ := run([]string{"tests/a.ts"}, ""); code != 2 {
		t.Errorf("path arg in green repo: exit %d, want 2", code)
	}
	if code, _ := run([]string{"src/a.ts"}, ""); code != 0 {
		t.Errorf("source path arg: exit %d, want 0", code)
	}
}

func TestTaskDir(t *testing.T) {
	cases := []struct {
		name, loc, commit, ignore, gitignore string
		ask, dir                             string
		board                                bool
		wantGitignore, wantExclude           bool
	}{
		{name: "commit unset", ask: "tasks.commit"},
		{name: "board, commit unset", loc: "board", ask: "tasks.commit"},
		{name: "commit true", commit: "true", dir: "docs/specs/s1"},
		{name: "ignore unset", commit: "false", ask: "tasks.ignore"},
		{name: "gitignore", commit: "false", ignore: "gitignore", dir: ".docs/specs/s1", wantGitignore: true},
		{name: "gitignore no newline", commit: "false", ignore: "gitignore", gitignore: "node_modules", dir: ".docs/specs/s1", wantGitignore: true},
		{name: "gitignore has line", commit: "false", ignore: "gitignore", gitignore: ".docs/\n", dir: ".docs/specs/s1", wantGitignore: true},
		{name: "exclude", commit: "false", ignore: "exclude", dir: ".docs/specs/s1", wantExclude: true},
		{name: "board", loc: "board", commit: "true", dir: "docs/specs/s1", board: true},
		{name: "board exclude", loc: "board", commit: "false", ignore: "exclude", dir: ".docs/specs/s1", board: true, wantExclude: true},
		{name: "local", loc: "local"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := newRepo(t)
			home := t.TempDir()
			t.Setenv("SMITHY_CONFIG", filepath.Join(home, "config.json"))
			t.Setenv("SMITHY_HOME", home)
			t.Setenv("SMELT_PLANS", "")
			t.Chdir(dir)
			for k, v := range map[string]string{"tasks.location": c.loc, "tasks.commit": c.commit, "tasks.ignore": c.ignore} {
				if v != "" {
					if err := config.Set(config.RepoFile(), k, v); err != nil {
						t.Fatal(err)
					}
				}
			}
			if c.gitignore != "" {
				write(t, filepath.Join(dir, ".gitignore"), c.gitignore)
			}
			got, board, err := TaskDir("s1")
			if c.ask != "" {
				var ask *AskError
				if !errors.As(err, &ask) || ask.Key != c.ask || !strings.HasPrefix(ask.Error(), "ask "+c.ask+": ") {
					t.Fatalf("err = %v, want ask %s", err, c.ask)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(dir, c.dir)
			if c.loc == "local" {
				want = filepath.Join(home, "plans", filepath.Base(dir), "s1")
			}
			if got != want || board != c.board {
				t.Errorf("TaskDir = %q board=%v, want %q board=%v", got, board, want, c.board)
			}
			if st, err := os.Stat(got); err != nil || !st.IsDir() {
				t.Errorf("%s not created", got)
			}
			if _, _, err := TaskDir("s1"); err != nil {
				t.Fatal(err)
			}
			gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
			ex, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
			check := func(name string, b []byte, want bool) {
				n := strings.Count("\n"+string(b), "\n.docs/\n")
				if want && n != 1 || !want && n != 0 {
					t.Errorf("%s has .docs/ %d times: %q", name, n, b)
				}
			}
			check(".gitignore", gi, c.wantGitignore)
			check("exclude", ex, c.wantExclude)
			if c.gitignore == "node_modules" && string(gi) != "node_modules\n.docs/\n" {
				t.Errorf(".gitignore = %q", gi)
			}
		})
	}
}

func TestTaskDirOutsideRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, _, err := TaskDir("s1"); !errors.Is(err, ErrNotRepo) {
		t.Errorf("err = %v, want ErrNotRepo", err)
	}
}

func TestStackRepoOverride(t *testing.T) {
	d := t.TempDir()
	os.MkdirAll(filepath.Join(d, ".smithy"), 0o755)
	os.WriteFile(filepath.Join(d, ".smithy", "config.json"), []byte(`{"stack":{"test":"claude plugin test mods/*"}}`), 0o644)
	for _, l := range Stack(d) {
		if l.Key == "test" && l.Value != "claude plugin test mods/*" {
			t.Fatalf("test = %q", l.Value)
		}
	}
}

func TestStackIgnoresABranchsOwnOverride(t *testing.T) {
	d := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", d, "-c", "user.email=a@b", "-c", "user.name=a"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	run("init", "-q")
	os.MkdirAll(filepath.Join(d, ".smithy"), 0o755)
	os.WriteFile(filepath.Join(d, ".smithy", "config.json"), []byte(`{"stack":{"test":"true"}}`), 0o644)
	run("add", ".")
	run("commit", "-q", "-m", "x")
	for _, l := range Stack(d) {
		if l.Key == "test" && l.Value == "true" {
			t.Fatal("a committed branch override set the test command")
		}
	}
}
