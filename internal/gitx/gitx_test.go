package gitx

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestStyle(t *testing.T) {
	cases := []struct {
		lint bool
		subs []string
		want string
	}{
		{false, nil, "conventional"},
		{true, []string{"Added stuff"}, "conventional"},
		{false, []string{"feat(core): init", "fix(api): handle nulls", "Update"}, "conventional"},
		{false, []string{"✨ add x", ":bug: fix y", "Update"}, "gitmoji"},
		{false, []string{"Add x", "Fix y", "feat: z"}, "plain"},
	}
	for _, c := range cases {
		if got := Style(c.lint, c.subs); got != c.want {
			t.Errorf("Style(%v, %q) = %s, want %s", c.lint, c.subs, got, c.want)
		}
	}
}

func TestCheck(t *testing.T) {
	bad := map[string]string{
		"":                                 "empty message",
		"feat: add x.":                     "subject ends with a period",
		"Added stuff":                      "not conventional",
		"feat: " + strings.Repeat("a", 80): "subject is 86 chars",
	}
	for msg, want := range bad {
		if err := Check(msg, "conventional"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Check(%q) = %v, want %q", msg, err, want)
		}
	}
	for _, msg := range []string{"feat(api): add pagination", "fix!: drop v1\n\nbody."} {
		if err := Check(msg, "conventional"); err != nil {
			t.Errorf("Check(%q) = %v", msg, err)
		}
	}
	if Check("add x", "gitmoji") == nil || Check(":sparkles: add x", "gitmoji") != nil || Check("🐛 fix y", "gitmoji") != nil {
		t.Error("gitmoji checks")
	}
	if Check("Whatever goes", "plain") != nil {
		t.Error("plain accepts any short subject")
	}
}

func TestScopes(t *testing.T) {
	got := Scopes([]string{"feat(api): a", "fix(api): b", "feat(core): c", "docs(cli): d", "plain"})
	if strings.Join(got, " ") != "api core cli" {
		t.Errorf("Scopes = %v", got)
	}
}

func TestTemplatesAndHeadings(t *testing.T) {
	top := t.TempDir()
	if len(Templates(top)) != 0 {
		t.Fatal("no template expected")
	}
	os.MkdirAll(filepath.Join(top, ".github", "PULL_REQUEST_TEMPLATE"), 0o755)
	os.WriteFile(filepath.Join(top, ".github", "pull_request_template.md"), []byte("## Summary\n\n## Testing\n"), 0o644)
	os.WriteFile(filepath.Join(top, ".github", "PULL_REQUEST_TEMPLATE", "b.md"), nil, 0o644)
	os.WriteFile(filepath.Join(top, ".github", "PULL_REQUEST_TEMPLATE", "a.md"), nil, 0o644)
	got := Templates(top)
	if len(got) != 3 || !strings.HasSuffix(got[0], "pull_request_template.md") || !strings.HasSuffix(got[1], "a.md") {
		t.Errorf("Templates = %v", got)
	}
	miss := MissingHeadings("# T\n## Summary\n## Testing\n##### deep\ntext", "## Summary\nfilled\n# T\n")
	if strings.Join(miss, "|") != "## Testing" {
		t.Errorf("MissingHeadings = %v", miss)
	}
}

func TestWorktreeOf(t *testing.T) {
	p := "worktree /r\nHEAD 1\nbranch refs/heads/main\n\nworktree /w/my wt\nHEAD 2\nbranch refs/heads/t1\n"
	if got := WorktreeOf(p, "t1"); got != "/w/my wt" {
		t.Errorf("WorktreeOf t1 = %q", got)
	}
	if got := WorktreeOf(p, "t"); got != "" {
		t.Errorf("WorktreeOf t = %q", got)
	}
}

func TestVerdict(t *testing.T) {
	yes, no := func() bool { return true }, func() bool { return false }
	cases := []struct {
		state, checks, mergeable string
		draft                    bool
		local                    string
		anc                      func() bool
		want                     string
	}{
		{"MERGED", "red", "", false, "x", no, "merged"},
		{"CLOSED", "green", "", false, "", no, "closed"},
		{"OPEN", "green", "MERGEABLE", false, "zzz", no, "unpushed"},
		{"OPEN", "green", "MERGEABLE", false, "old", yes, "green"},
		{"OPEN", "green", "MERGEABLE", true, "", no, "draft"},
		{"OPEN", "pending", "MERGEABLE", true, "", no, "pending"},
		{"OPEN", "red", "MERGEABLE", true, "", no, "red"},
		{"OPEN", "red", "MERGEABLE", false, "", no, "red"},
		{"OPEN", "pending", "MERGEABLE", false, "", no, "pending"},
		{"OPEN", "green", "CONFLICTING", false, "", no, "conflict"},
		{"OPEN", "red", "CONFLICTING", false, "", no, "conflict"},
		{"OPEN", "none", "MERGEABLE", false, "head", no, "green"},
		{"OPEN", "green", "UNKNOWN", false, "", no, "pending"},
		{"OPEN", "green", "UNKNOWN", true, "", no, "pending"},
		{"OPEN", "red", "UNKNOWN", false, "", no, "red"},
	}
	for _, c := range cases {
		if got := Verdict(c.state, c.checks, c.mergeable, c.draft, c.local, "head", c.anc); got != c.want {
			t.Errorf("Verdict(%+v) = %s, want %s", c, got, c.want)
		}
	}
	if Checks([]Rollup{{Conclusion: "success"}, {Conclusion: "", State: ""}}, failRe, pendingRe) != "pending" {
		t.Error("missing outcome counts as pending")
	}
}

func fixture(t *testing.T, name string) []byte {
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "tend", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSummarize(t *testing.T) {
	cases := []struct {
		view, thr, cmp string
		want           map[string]any
	}{
		{"view-red.json", "threads-2open.json", "compare-behind.json", map[string]any{"checks": "red", "mergeable": "CONFLICTING", "review": "CHANGES_REQUESTED", "unresolved": 2.0, "behind": 3.0, "head": "warden/t1"}},
		{"view-green.json", "threads-none.json", "compare-even.json", map[string]any{"checks": "green", "review": "none", "unresolved": 0.0, "behind": 0.0}},
		{"view-pending.json", "threads-none.json", "compare-even.json", map[string]any{"checks": "pending", "review": "none"}},
		{"view-nochecks.json", "threads-none.json", "compare-even.json", map[string]any{"state": "MERGED", "checks": "none"}},
	}
	for _, c := range cases {
		s, err := Summarize(fixture(t, c.view), fixture(t, c.thr), fixture(t, c.cmp))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(s)
		var got map[string]any
		json.Unmarshal(b, &got)
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: %s = %v, want %v", c.view, k, got[k], v)
			}
		}
	}
	s, _ := Summarize([]byte(`{"url":"u","state":"OPEN"}`), []byte(`{}`), []byte(`{}`))
	b, _ := json.Marshal(s)
	if string(b) != `{"url":"u","state":"OPEN","checks":"none","mergeable":"UNKNOWN","review":"none","unresolved":0,"behind":0,"head":null,"base":null,"action":"wait"}` {
		t.Errorf("defaults: %s", b)
	}
}

func TestVerifyUsageNamesALocalPath(t *testing.T) {
	if !strings.Contains(verifyUsage, "local checkout path") {
		t.Fatalf("usage: %q", verifyUsage)
	}
}

func TestPRRepoAndOrigin(t *testing.T) {
	if PRRepo("https://github.com/Zovaris/smithy/pull/5") != "Zovaris/smithy" || PRRepo("5") != "" {
		t.Fatal("PRRepo")
	}
	for url, want := range map[string]string{
		"git@github.com:Zovaris/smithy.git":       "Zovaris/smithy",
		"https://github.com/Zovaris/smithy.git\n": "Zovaris/smithy",
		"https://github.com/o/r":                  "o/r",
		"":                                        "",
	} {
		if got := OriginRepo(url); got != want {
			t.Errorf("OriginRepo(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestVerifyRefusesAPRFromAnotherRepo(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	exec.Command("git", "-C", dir, "remote", "add", "origin", "git@github.com:o/r.git").Run()
	t.Chdir(dir)
	if c := VerifyMain([]string{dir, "b", "https://github.com/other/repo/pull/1"}); c != 2 {
		t.Fatalf("exit %d, want 2", c)
	}
}

func TestPRHelpAndMissingTemplate(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "-C", dir, "init", "-q").Run()
	t.Chdir(dir)
	if c := PRMain([]string{"--help"}); c != 0 {
		t.Fatalf("--help exit %d", c)
	}
	if c := PRMain([]string{"create", "--help"}); c != 0 {
		t.Fatalf("create --help exit %d", c)
	}
	if c := PRMain([]string{"template"}); c != 0 {
		t.Fatalf("template without one exit %d", c)
	}
}

func TestCommitStylePrintsSubjectLimit(t *testing.T) {
	dir := t.TempDir()
	exec.Command("git", "-C", dir, "init", "-q").Run()
	t.Chdir(dir)
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })
	code := CommitMain([]string{"style"})
	os.Stdout = old
	w.Close()
	b, _ := io.ReadAll(r)
	if code != 0 || !strings.Contains(string(b), "subject limit: 72 chars") {
		t.Fatalf("exit %d, output %q", code, b)
	}
}

func TestCommitFailurePrintsErrorLast(t *testing.T) {
	dir := t.TempDir()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		exec.Command("git", append([]string{"-C", dir}, a...)...).Run()
	}
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	os.WriteFile(hook, []byte("#!/bin/sh\necho hook says no >&2\nexit 1\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	t.Chdir(dir)
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	code := CommitMain([]string{"fix: add a", "a.txt"})
	os.Stderr = old
	w.Close()
	b, _ := io.ReadAll(r)
	out := strings.Split(strings.TrimSpace(string(b)), "\n")
	if code == 0 || !strings.HasPrefix(out[len(out)-1], "smithy commit: git commit failed") {
		t.Fatalf("exit %d, stderr %q", code, b)
	}
}

func TestCIPending(t *testing.T) {
	dir := t.TempDir()
	if CIPending(dir) {
		t.Error("no workflows dir: want false")
	}
	wf := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wf, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if CIPending(dir) {
		t.Error("no workflow file: want false")
	}
	if err := os.WriteFile(filepath.Join(wf, "ci.yml"), []byte("on: push"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !CIPending(dir) {
		t.Error("ci.yml: want true")
	}
}

func TestVerifyConflictExit4(t *testing.T) {
	for _, checks := range []string{"PENDING", "FAILURE", "SUCCESS"} {
		dir := t.TempDir()
		exec.Command("git", "-C", dir, "init", "-q").Run()
		t.Chdir(dir)
		bin := t.TempDir()
		js := `{"url":"u","state":"OPEN","headRefOid":"abc","mergeable":"CONFLICTING","isDraft":false,"statusCheckRollup":[{"conclusion":"","state":"` + checks + `"}]}`
		if checks != "PENDING" {
			js = strings.Replace(js, `"conclusion":""`, `"conclusion":"`+checks+`"`, 1)
		}
		os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\necho '"+js+"'\n"), 0o755)
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		if c := VerifyMain([]string{dir, "b", "5"}); c != 4 {
			t.Errorf("%s + CONFLICTING: exit %d, want 4", checks, c)
		}
	}
}

func TestWithoutPRHint(t *testing.T) {
	in := "remote: \nremote: Create a pull request for 'feat/x' on GitHub by visiting:\nremote:      https://github.com/o/r/pull/new/feat/x\nremote: \nhook: lint ok\n"
	if got := withoutPRHint(in); got != "hook: lint ok\n" {
		t.Fatalf("got %q", got)
	}
	if got := withoutPRHint("error: failed to push\n"); got != "error: failed to push\n" {
		t.Fatalf("got %q", got)
	}
}

func TestChecksAllSkippedIsNotGreen(t *testing.T) {
	fail, pend := regexp.MustCompile(`FAILURE|ERROR|CANCELLED|TIMED_OUT`), regexp.MustCompile(`PENDING|QUEUED|IN_PROGRESS`)
	if g := Checks([]Rollup{{Conclusion: "SKIPPED"}, {Conclusion: "NEUTRAL"}}, fail, pend); g != "none" {
		t.Fatalf("all skipped = %q", g)
	}
	if g := Checks([]Rollup{{Conclusion: "SKIPPED"}, {Conclusion: "SUCCESS"}}, fail, pend); g != "green" {
		t.Fatalf("skipped + success = %q", g)
	}
	if g := Checks([]Rollup{{State: "SUCCESS"}}, fail, pend); g != "green" {
		t.Fatalf("status success = %q", g)
	}
}
