package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "smithy-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "smithy")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// sandbox is a temp git repo plus a temp home; gh and agent CLIs are kept off PATH.
func sandbox(t *testing.T) (repo string, env []string) {
	t.Helper()
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	home := filepath.Join(root, "home")
	for _, d := range []string{repo, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	pathDir := filepath.Join(root, "bin")
	os.MkdirAll(pathDir, 0o755)
	if err := os.Symlink(gitPath, filepath.Join(pathDir, "git")); err != nil {
		t.Fatal(err)
	}
	env = []string{
		"HOME=" + home,
		"SMITHY_HOME=" + filepath.Join(home, ".smithy"),
		"SMITHY_CONFIG=" + filepath.Join(home, "config.json"),
		"WARDEN_HOME=" + filepath.Join(home, "warden"),
		"PATH=" + pathDir + string(os.PathListSeparator) + "/bin:/usr/bin",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		c := exec.Command("git", args...)
		c.Dir, c.Env = repo, env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo, env
}

func run(t *testing.T, dir string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	c := exec.Command(bin, args...)
	c.Dir, c.Env = dir, env
	c.Stdin = strings.NewReader("")
	var o, e bytes.Buffer
	c.Stdout, c.Stderr = &o, &e
	err := c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return o.String(), e.String(), code
}

func listed() []string {
	var names []string
	for n, c := range commands {
		if c.help != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

func TestEveryCommandHelpAndUnknownOption(t *testing.T) {
	repo, env := sandbox(t)
	for _, name := range listed() {
		t.Run(name, func(t *testing.T) {
			for _, h := range [][]string{{"--help"}, {"-h"}, {"x", "--help"}} {
				out, errOut, code := run(t, repo, env, append([]string{name}, h...)...)
				if code != 0 || strings.TrimSpace(out) == "" {
					t.Errorf("%s %v: exit %d, stdout %q, stderr %q", name, h, code, out, errOut)
				}
			}
			out, errOut, code := run(t, repo, env, name, "--bogus-flag")
			if code != 2 || !strings.Contains(errOut, "smithy "+name+": unknown option --bogus-flag") || out != "" {
				t.Errorf("%s --bogus-flag: exit %d, stdout %q, stderr %q", name, code, out, errOut)
			}
		})
	}
	entries, _ := os.ReadDir(repo)
	for _, e := range entries {
		if e.Name() != ".git" {
			t.Errorf("side effect: %s created", e.Name())
		}
	}
}

func TestUsageMistakesAndSilentFailures(t *testing.T) {
	repo, env := sandbox(t)
	outside := t.TempDir()
	cases := []struct {
		dir    string
		args   []string
		code   int
		errHas string
	}{
		{repo, []string{"adr", "--bogus"}, 2, "unknown option"},
		{outside, []string{"changed"}, 2, "not a git repo"},
		{repo, []string{"comments", "no/such/file.go"}, 2, "no/such/file.go"},
		{repo, []string{"commit", "fix: x"}, 2, "paths"},
		{repo, []string{"skills", "no-such-skill"}, 1, "no-such-skill"},
		{repo, []string{"config", "model", "cheap"}, 3, "cheap"},
		{repo, []string{"tasks", "where"}, 3, "no task dir"},
		{repo, []string{"cards", "get", "b", "01"}, 1, "no card"},
		{repo, []string{"wait", "bogus:1"}, 2, "bogus:1"},
		{repo, []string{"agent", "missing.md"}, 2, "prompt file not found: missing.md"},
		{repo, []string{"search", "--", "--x"}, 1, "no match"},
	}
	for _, c := range cases {
		out, errOut, code := run(t, c.dir, env, c.args...)
		if code != c.code || !strings.Contains(errOut, c.errHas) {
			t.Errorf("%v: exit %d (want %d), stdout %q, stderr %q (want %q)", c.args, code, c.code, out, errOut, c.errHas)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "docs")); err == nil {
		t.Error("adr --bogus created docs/")
	}
}

func TestTasksAfterTaskDir(t *testing.T) {
	repo, env := sandbox(t)
	if _, e, code := run(t, repo, env, "config", "set", "--repo", "tasks.commit", "true"); code != 0 {
		t.Fatal(e)
	}
	dir, e, code := run(t, repo, env, "task-dir", "demo")
	if code != 0 {
		t.Fatal(e)
	}
	dir = strings.TrimSpace(dir)
	out, e, code := run(t, repo, env, "tasks", "add", "first")
	if code != 0 || !strings.HasPrefix(strings.TrimSpace(out), dir) {
		t.Fatalf("tasks add after task-dir: exit %d, out %q, err %q, dir %q", code, out, e, dir)
	}
	if out, e, code := run(t, repo, env, "tasks", "list"); code != 0 || !strings.Contains(out, "first") {
		t.Fatalf("tasks list: exit %d, out %q, err %q", code, out, e)
	}
}

func TestDoubleDashReachesNoCommand(t *testing.T) {
	repo, env := sandbox(t)
	os.MkdirAll(filepath.Join(repo, ".smelt"), 0o755)
	os.WriteFile(filepath.Join(repo, ".smelt", "green"), nil, 0o644)
	if _, e, code := run(t, repo, env, "guard-tests", "--", "hello_test.go"); code != 2 {
		t.Errorf("guard-tests -- hello_test.go: exit %d, stderr %q", code, e)
	}
	os.RemoveAll(filepath.Join(repo, ".smelt"))
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b\n"), 0o644)
	if out, e, code := run(t, repo, env, "commit", "feat: add b", "--", "b.txt"); code != 0 || !strings.Contains(out, "add b") {
		t.Errorf("commit -- b.txt: exit %d, out %q, err %q", code, out, e)
	}
	os.WriteFile(filepath.Join(repo, "f.go"), []byte("package f\n"), 0o644)
	if _, e, code := run(t, repo, env, "comments", "--", "f.go"); code != 0 {
		t.Errorf("comments -- f.go: exit %d, err %q", code, e)
	}
	if out, e, code := run(t, repo, env, "drift", "--", "main"); code == 2 || strings.Contains(out+e, "--") {
		t.Errorf("drift -- main: exit %d, out %q, err %q", code, out, e)
	}
	if out, e, code := run(t, repo, env, "names", "branch", "--", "x", "T"); code != 0 || strings.Contains(out, "--") {
		t.Errorf("names branch -- x T: exit %d, out %q, err %q", code, out, e)
	}
	if _, e, code := run(t, repo, env, "search", "--", "--all"); code != 1 || !strings.Contains(e, "no match") {
		t.Errorf("search -- --all: exit %d, err %q", code, e)
	}
	out, e, code := run(t, repo, env, "receipt", "run", "--claim", "c", "--", "git", "--version")
	if code != 0 || !strings.Contains(out, "git version") {
		t.Errorf("receipt run -- git --version: exit %d, out %q, err %q", code, out, e)
	}
}

func TestFlagEqualsValue(t *testing.T) {
	repo, env := sandbox(t)
	for _, args := range [][]string{
		{"search", "--max=1", "x"},
		{"route", "--text=fix a typo"},
		{"scan", "--base=main"},
		{"pr", "create", "--base=main"},
		{"receipt", "run", "--claim=a", "--", "git", "--version"},
		{"tasks", "add", "t", "--key=X"},
	} {
		if out, e, code := run(t, repo, env, args...); strings.Contains(e, "unknown option") {
			t.Errorf("%v: exit %d, out %q, err %q", args, code, out, e)
		}
	}
	if out, e, code := run(t, repo, env, "route", "--json=x"); code != 2 {
		t.Errorf("route --json=x: exit %d, out %q, err %q", code, out, e)
	}
}

func TestValueFlagWithoutValue(t *testing.T) {
	repo, env := sandbox(t)
	dir := filepath.Join(repo, "plan")
	os.MkdirAll(dir, 0o755)
	for _, args := range [][]string{
		{"tasks", "add", dir, "x", "--blocked-by"},
		{"tasks", "add", dir, "x", "--key", "--blocked-by", "01"},
		{"search", "q", "--max"},
	} {
		if out, e, code := run(t, repo, env, args...); code != 2 || !strings.Contains(e, "needs a value") {
			t.Errorf("%v: exit %d, out %q, err %q", args, code, out, e)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("side effect: %d files in %s", len(entries), dir)
	}
}

func TestTasksFlagsPerSubcommand(t *testing.T) {
	repo, env := sandbox(t)
	for _, args := range [][]string{{"tasks", "ready", "--all"}, {"tasks", "list", "--key", "x"}, {"tasks", "board", "--key=x"}} {
		if out, e, code := run(t, repo, env, args...); code != 2 || !strings.Contains(e, "unknown option") {
			t.Errorf("%v: exit %d, out %q, err %q", args, code, out, e)
		}
	}
	if _, e, _ := run(t, repo, env, "tasks", "board", "--all"); strings.Contains(e, "unknown option") {
		t.Errorf("tasks board --all: %q", e)
	}
}

func TestVersionVerbose(t *testing.T) {
	repo, env := sandbox(t)
	out, e, code := run(t, repo, env, "version", "--verbose")
	if code != 0 || e != "" {
		t.Fatalf("version --verbose: exit %d, stderr %q", code, e)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "binary: ") || strings.TrimPrefix(lines[1], "binary: ") == "" {
		t.Errorf("version --verbose: want version, binary and build lines, got %q", out)
	}
	if !strings.HasPrefix(lines[2], "build: ") {
		t.Errorf("version --verbose: missing build line, got %q", out)
	}
	plain, _, code := run(t, repo, env, "version")
	if code != 0 || strings.Contains(plain, "binary:") {
		t.Errorf("version: want version only, got %q (exit %d)", plain, code)
	}
}

func TestArgMistakes(t *testing.T) {
	repo, env := sandbox(t)
	cases := []struct {
		args   []string
		code   int
		errHas string
	}{
		{[]string{"comments", "--diff"}, 2, "--diff needs a base"},
		{[]string{"changed", "nosuch"}, 2, "smithy changed: no such base nosuch"},
		{[]string{"drift", "main", "extra"}, 2, "smithy drift: unexpected argument extra"},
		{[]string{"version", "extra"}, 2, "smithy version: unexpected argument extra"},
		{[]string{"commit", "fix: x", "nosuch.txt"}, 2, "smithy commit: no such path: nosuch.txt"},
	}
	for _, c := range cases {
		out, errOut, code := run(t, repo, env, c.args...)
		if code != c.code || !strings.Contains(errOut, c.errHas) {
			t.Errorf("%v: exit %d (want %d), stdout %q, stderr %q (want %q)", c.args, code, c.code, out, errOut, c.errHas)
		}
	}
}

func TestCleanupMissingBranch(t *testing.T) {
	repo, env := sandbox(t)
	out, e, code := run(t, repo, env, "cleanup", repo, "nosuchbr")
	if code != 1 || !strings.Contains(out+e, "no branch nosuchbr") || strings.Contains(out, "cleaned") {
		t.Errorf("cleanup nosuchbr: exit %d, out %q, err %q", code, out, e)
	}
}

func TestPRStateSignedOut(t *testing.T) {
	repo, env := sandbox(t)
	bin := filepath.Join(filepath.Dir(repo), "bin")
	gh := "#!/bin/sh\necho 'You are not logged into any GitHub hosts.' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	out, e, code := run(t, repo, env, "pr-state", "1")
	if code != 3 || strings.Count(strings.TrimSpace(e), "\n") != 0 || !strings.Contains(e, "signed out") {
		t.Errorf("pr-state signed out: exit %d, out %q, err %q", code, out, e)
	}
}

func TestRepeatedValueFlagLastWins(t *testing.T) {
	repo, env := sandbox(t)
	if out, e, code := run(t, repo, env, "route", "--speed", "bogus", "--speed=fast", "--text", "x"); code != 0 {
		t.Errorf("route --speed bogus --speed=fast: exit %d, out %q, err %q", code, out, e)
	}
	dir := filepath.Join(repo, "plan")
	os.MkdirAll(dir, 0o755)
	out, e, code := run(t, repo, env, "tasks", "add", dir, "x", "--key", "A-1", "--key", "B-2")
	if code != 0 {
		t.Fatalf("tasks add: exit %d, err %q", code, e)
	}
	b, _ := os.ReadFile(strings.TrimSpace(out))
	if !strings.Contains(string(b), "B-2") || strings.Contains(string(b), "A-1") {
		t.Errorf("tasks add --key A-1 --key B-2 wrote:\n%s", b)
	}
}

func noGHEnv(repo string, env []string) []string {
	out := []string{}
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			e = "PATH=" + filepath.Join(filepath.Dir(repo), "bin")
		}
		out = append(out, e)
	}
	return out
}

func TestNoGitHubAccess(t *testing.T) {
	repo, env := sandbox(t)
	env = noGHEnv(repo, env)
	if out, e, code := run(t, repo, env, "pr-state", "1"); code != 3 || !strings.Contains(e, "needs the GitHub CLI or GH_TOKEN") {
		t.Errorf("pr-state: exit %d, out %q, err %q", code, out, e)
	}
	if out, e, code := run(t, repo, env, "pr", "merge", "1", "--rebase"); code == 0 || !strings.Contains(e, "needs the GitHub CLI or GH_TOKEN") {
		t.Errorf("pr merge: exit %d, out %q, err %q", code, out, e)
	}
	if out, e, code := run(t, repo, env, "pr", "merge"); code != 2 || !strings.Contains(e, "usage: smithy pr merge") {
		t.Errorf("pr merge usage: exit %d, out %q, err %q", code, out, e)
	}
}

func TestPRCreatePrintsCompareURLWithoutGitHub(t *testing.T) {
	repo, env := sandbox(t)
	bare := filepath.Join(filepath.Dir(repo), "origin.git")
	for _, args := range [][]string{
		{"init", "-q", "--bare", bare},
		{"-C", repo, "remote", "add", "origin", "https://github.com/o/r.git"},
		{"-C", repo, "config", "url." + bare + ".pushInsteadOf", "https://github.com/o/r.git"},
		{"-C", repo, "checkout", "-q", "-b", "feat/x"},
	} {
		c := exec.Command("git", args...)
		c.Env = env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	body := filepath.Join(filepath.Dir(repo), "body.md")
	os.WriteFile(body, []byte("what and why"), 0o644)
	out, e, code := run(t, repo, noGHEnv(repo, env), "pr", "create", "--base", "main", "--title", "feat: x", "--body-file", body)
	if code != 0 || strings.TrimSpace(out) != "https://github.com/o/r/compare/main...feat/x?expand=1" || !strings.Contains(e, "open the PR") {
		t.Errorf("pr create: exit %d, out %q, err %q", code, out, e)
	}
}

func TestCleanupRemovesABranchWhoseCommitsAnotherBranchHas(t *testing.T) {
	repo, env := sandbox(t)
	g := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=a@b", "-c", "user.name=a"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g("checkout", "-q", "-b", "work")
	g("commit", "-q", "--allow-empty", "-m", "wip: test agents")
	g("branch", "work-adversary")
	out, e, code := run(t, repo, env, "cleanup", repo, "work-adversary")
	if code != 0 || !strings.Contains(out, "cleaned work-adversary") {
		t.Fatalf("exit %d, out %q, err %q", code, out, e)
	}
}
