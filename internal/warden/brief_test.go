package warden

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	tmpl := "a {{x}}[if y] Y[end].\n[if !y]\nnot y\n[end]\n[if y]\n[if z]\nz\n[end]\ny\n[end]\nend\n"
	got, err := render(tmpl, map[string]string{"x": "1"}, map[string]bool{"y": true, "z": false})
	if err != nil || got != "a 1 Y.\ny\nend\n" {
		t.Errorf("got %q %v", got, err)
	}
	for _, bad := range []string{"{{nope}}", "[if nope]x[end]", "[end]", "[if y]x"} {
		if _, err := render(bad, map[string]string{}, map[string]bool{"y": true}); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestChannelLine(t *testing.T) {
	md, err := os.ReadFile(filepath.Join("..", "..", "skills", "warden", "references", "harness.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := channelLine(string(md), "claude-cli"); got != "Send it to the warden with SendMessage, to: <name>." {
		t.Errorf("claude-cli: %q", got)
	}
	for _, h := range []string{"codex", "claude-desktop", "unknown"} {
		if got := channelLine(string(md), h); got != "Put it in the card's note." {
			t.Errorf("%s: %q", h, got)
		}
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

type briefEnv struct{ repo, wt, dir string }

func newBrief(t *testing.T, batchJSON string) briefEnv {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	e := briefEnv{filepath.Join(root, "repo"), filepath.Join(root, "wt"), filepath.Join(root, "warden", "b1")}
	skills, _ := filepath.Abs(filepath.Join("..", "..", "skills"))
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("WARDEN_TEST_SKILL_ROOT", skills)
	os.MkdirAll(filepath.Join(e.repo, "docs", "specs"), 0o755)
	os.MkdirAll(e.dir, 0o755)
	git(t, e.repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(e.repo, "docs", "specs", "01-a.md"), "spec")
	git(t, e.repo, "add", ".")
	git(t, e.repo, "commit", "-qm", "init")
	git(t, e.repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	git(t, e.repo, "worktree", "add", "-q", "-b", "feat/a", e.wt)
	r := strings.NewReplacer("REPO", e.repo, "WT", e.wt)
	writeFile(t, filepath.Join(e.dir, "batch.json"), r.Replace(batchJSON))
	return e
}

func brief(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	o, e, c := capture(t, func() int { return Brief(args) })
	return strings.TrimSpace(o), e, c
}

const oneItem = `{"batch":"b1","repo":"REPO","max":1,"stack":true,"mode":"smelt normal","merge":"auto","tasks_dir":"docs/specs",
"items":[{"id":"a","title":"Do A","kind":"ship","intent":"make A work","source":"docs/specs/01-a.md","base":"main","branch":"feat/a","worktree":"WT","prereqs":[],"launches":0},
{"id":"b","title":"Do B","kind":"ship","intent":"make B","base":"main","branch":"feat/b","worktree":"WT2","prereqs":["a"],"launches":0}]}`

func TestBriefRenders(t *testing.T) {
	e := newBrief(t, oneItem)
	writeFile(t, filepath.Join(e.repo, "docs", "specs", "rulings.md"), "- a: keep the API\n- b: other\n")
	writeFile(t, filepath.Join(e.dir, "decisions.md"), "- ab: not this one\n- a uses JSON\n")
	t.Setenv("WARDEN_HARNESS", "codex")
	o, er, c := brief(t, "b1", "a")
	if c != 0 {
		t.Fatalf("%d %s", c, er)
	}
	if o != filepath.Join(e.dir, "a.brief.md") {
		t.Errorf("path %q", o)
	}
	b, _ := os.ReadFile(o)
	got := string(b)
	for _, want := range []string{
		"You are worker a in warden batch b1: Do A. Kind: ship.",
		"Worktree (already created; work only here): " + e.wt + ", branch feat/a.",
		"PR target: main. Merge mode: auto.\n",
		"make A work",
		filepath.Join(e.repo, "docs", "specs", "01-a.md") + " (read it first",
		"## Rulings (settled; follow them)\n- a: keep the API\n- a uses JSON\n",
		"skills/smelt/SKILL.md and follow it",
		"tasks set docs/specs/01-a.md status=done",
		"`cd " + e.wt + " &&`",
		"skills/warden/scripts/smithy cards set b1 a key=value",
		"Put it in the card's note.",
		`"done: a PR <url>"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{"{{", "[if", "[end]", "wait-turn", "relaunch", "Stacked", "- b: other", "ab: not"} {
		if strings.Contains(got, bad) {
			t.Errorf("unexpected %q in:\n%s", bad, got)
		}
	}
}

func TestBriefWaitRelaunchStack(t *testing.T) {
	e := newBrief(t, oneItem)
	git(t, e.repo, "worktree", "add", "-q", "-b", "feat/b", e.wt+"2")
	t.Setenv("WARDEN_HARNESS", "claude-cli")
	if _, er, c := brief(t, "b1", "b"); c != 2 || !strings.Contains(er, "--to") {
		t.Errorf("claude-cli without --to: %d %s", c, er)
	}
	o, er, c := brief(t, "b1", "b", "--to", "boss", "--subagent")
	if c != 0 {
		t.Fatalf("%d %s", c, er)
	}
	b, _ := os.ReadFile(o)
	got := string(b)
	for _, want := range []string{
		"Merge mode: auto. Stacked on a: merge only after it has merged.",
		"wait-turn b1 b 9` in the\n  foreground",
		"git merge origin/feat/a`",
		"Read requirements from the intent and the code.",
		"to: boss.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "## Rulings") || strings.Contains(got, "in the background") {
		t.Errorf("unexpected:\n%s", got)
	}
}

func TestBriefErrors(t *testing.T) {
	t.Setenv("WARDEN_HARNESS", "codex")
	cases := []struct{ name, json, id, want string }{
		{"missing worktree field", strings.Replace(oneItem, `"worktree":"WT",`, "", 1), "a", "missing worktree"},
		{"wrong branch", strings.Replace(oneItem, `"branch":"feat/a"`, `"branch":"feat/x"`, 1), "a", `on branch "feat/a", not feat/x`},
		{"worktree not created", oneItem, "b", "not a git worktree root"},
		{"bad merge", strings.Replace(oneItem, `"merge":"auto"`, `"merge":"ask"`, 1), "a", "want auto or manual"},
		{"no item", oneItem, "zz", "no item zz"},
		{"worktree is the repo", strings.Replace(oneItem, `"worktree":"WT"`, `"worktree":"REPO"`, 1), "a", "on branch \"main\""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newBrief(t, c.json)
			if _, er, code := brief(t, "b1", c.id); code != 2 || !strings.Contains(er, c.want) {
				t.Errorf("%d %q", code, er)
			}
		})
	}
	if _, _, c := brief(t, "b1"); c != 2 {
		t.Error("usage: want 2")
	}
}

func TestBriefRelaunch(t *testing.T) {
	newBrief(t, strings.Replace(oneItem, `"launches":0`, `"launches":1`, 1))
	t.Setenv("WARDEN_HARNESS", "codex")
	o, er, c := brief(t, "b1", "a")
	b, _ := os.ReadFile(o)
	if c != 0 || !strings.Contains(string(b), "- This is a relaunch. Read ") || strings.Contains(string(b), "wait-turn") {
		t.Errorf("%d %s\n%s", c, er, b)
	}
}
