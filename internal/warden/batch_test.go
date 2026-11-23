package warden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchInitAdd(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("HOME", root)
	repo := filepath.Join(root, "repo")
	td := filepath.Join(repo, "docs", "specs", "notes")
	os.MkdirAll(td, 0o755)
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(td, "01-pagination.md"), "# 01 · Add pagination\nStatus: todo\nBlocked by: none\nSpeed: normal\nPR: -\n")
	writeFile(t, filepath.Join(td, "02-reset.md"), "# 02 · Password reset\nStatus: todo\nBlocked by: 01\nSpeed: fast\nPR: -\n")
	writeFile(t, filepath.Join(td, "03-old.md"), "# 03 · Old\nStatus: done\nBlocked by: none\nPR: -\n")
	writeFile(t, filepath.Join(td, "04-list.md"), "# 04 · List view\nStatus: todo\nBlocked by: 03\nPR: -\n")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "tasks")
	git(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo, "--merge", "auto"}) })
	dir := strings.TrimSpace(o)
	if c != 0 || filepath.Base(dir) != "repo-notes" {
		t.Fatalf("%d %s %s", c, o, er)
	}
	var bt briefBatch
	raw, _ := os.ReadFile(filepath.Join(dir, "batch.json"))
	if err := json.Unmarshal(raw, &bt); err != nil || len(bt.Items) != 3 || bt.Merge != "auto" {
		t.Fatalf("%v %s", err, raw)
	}
	a, b := bt.Items[0], bt.Items[1]
	if a.ID != "notes-01" || a.Intent != "docs/specs/notes/01-pagination.md" || a.Risky || a.Base != "main" || !strings.Contains(a.Worktree, "worktrees") {
		t.Errorf("item 1: %+v", a)
	}
	if b.ID != "notes-02" || len(b.Prereqs) != 1 || b.Prereqs[0] != "notes-01" || !b.Risky || !strings.Contains(string(raw), `"speed": "normal"`) {
		t.Errorf("item 2: %+v\n%s", b, raw)
	}
	if len(bt.Items[2].Prereqs) != 0 {
		t.Errorf("a done blocker must not be a prereq: %+v", bt.Items[2])
	}
	if _, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo}) }); c != 2 || !strings.Contains(er, "still runs") {
		t.Errorf("second init over unfinished items must refuse: %d %s", c, er)
	}
	for _, id := range []string{"notes-01", "notes-02", "notes-04"} {
		writeFile(t, filepath.Join(dir, id+".json"), `{"state":"merged"}`)
	}
	if o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo}) }); c != 0 || filepath.Base(strings.TrimSpace(o)) != "repo-notes-2" {
		t.Errorf("init after a finished batch takes -2: %d %s %s", c, o, er)
	}
	if _, er, c := capture(t, func() int {
		return Batch([]string{"add", "repo-notes", "--id", "fix", "--title", "Fix review findings", "--intent", "id reuse after delete"})
	}); c != 0 {
		t.Fatalf("add: %d %s", c, er)
	}
	for _, a := range [][]string{{"--id", "fix", "--title", "x"}, {"--id", "fix2", "--title", "Fix review findings"}} {
		if _, er, c := capture(t, func() int { return Batch(append([]string{"add", "repo-notes", "--intent", "y"}, a...)) }); c != 2 || !strings.Contains(er, "already") {
			t.Errorf("duplicate add %v: %d %s", a, c, er)
		}
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "batch.json"))
	json.Unmarshal(raw, &bt)
	if f := bt.Items[3]; len(bt.Items) != 4 || f.Route != "debug" || f.Base != "main" || f.Intent != "id reuse after delete" || !strings.Contains(string(raw), `"speed": "normal"`) {
		t.Errorf("added: %+v", bt.Items)
	}
	if _, _, c := capture(t, func() int { return Batch([]string{"init", td}) }); c != 2 {
		t.Errorf("init without --repo: %d", c)
	}
}

func TestBatchInitIntentCommitted(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("HOME", root)
	repo := filepath.Join(root, "repo")
	td := filepath.Join(repo, "notes")
	os.MkdirAll(td, 0o755)
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README.md"), "x")
	git(t, repo, "add", "-A")
	git(t, repo, "commit", "-q", "-m", "init")
	git(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	writeFile(t, filepath.Join(td, "01-a.md"), "# 01 · A thing\nStatus: todo\nBlocked by: none\nPR: -\n")
	o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo}) })
	if c != 0 {
		t.Fatalf("%d %s", c, er)
	}
	var bt briefBatch
	raw, _ := os.ReadFile(filepath.Join(strings.TrimSpace(o), "batch.json"))
	json.Unmarshal(raw, &bt)
	if want := filepath.Join(td, "01-a.md"); bt.Items[0].Intent != want {
		t.Errorf("uncommitted task file keeps its absolute path: %q want %q", bt.Items[0].Intent, want)
	}
}

func TestBatchInitRisky(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("HOME", root)
	repo := filepath.Join(root, "repo")
	td := filepath.Join(repo, "notes")
	os.MkdirAll(td, 0o755)
	git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(td, "01-a.md"), "# 01 · Alpha\nStatus: todo\nBlocked by: none\nPR: -\n")
	writeFile(t, filepath.Join(td, "02-b.md"), "# 02 · Beta\nStatus: todo\nBlocked by: none\nPR: -\n")
	writeFile(t, filepath.Join(td, "03-c.md"), "# 03 · Gamma\nStatus: todo\nBlocked by: none\nPR: -\n")
	if _, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo, "--risky", "notes-02,nope"}) }); c == 0 || !strings.Contains(er, "nope") {
		t.Fatalf("unknown id must fail: %d %s", c, er)
	}
	o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo, "--risky", "notes-02,notes-03"}) })
	if c != 0 {
		t.Fatalf("%d %s", c, er)
	}
	var bt briefBatch
	raw, _ := os.ReadFile(filepath.Join(strings.TrimSpace(o), "batch.json"))
	json.Unmarshal(raw, &bt)
	if bt.Items[0].Risky || !bt.Items[1].Risky || !bt.Items[2].Risky {
		t.Errorf("risky flags: %+v", bt.Items)
	}
}
