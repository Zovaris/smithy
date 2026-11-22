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
	o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo, "--merge", "auto"}) })
	dir := strings.TrimSpace(o)
	if c != 0 || filepath.Base(dir) != "notes" {
		t.Fatalf("%d %s %s", c, o, er)
	}
	var bt briefBatch
	raw, _ := os.ReadFile(filepath.Join(dir, "batch.json"))
	if err := json.Unmarshal(raw, &bt); err != nil || len(bt.Items) != 2 || bt.Merge != "auto" {
		t.Fatalf("%v %s", err, raw)
	}
	a, b := bt.Items[0], bt.Items[1]
	if a.ID != "notes-01" || a.Intent != "docs/specs/notes/01-pagination.md" || a.Risky || a.Base != "main" || !strings.Contains(a.Worktree, "worktrees") {
		t.Errorf("item 1: %+v", a)
	}
	if b.ID != "notes-02" || len(b.Prereqs) != 1 || b.Prereqs[0] != "notes-01" || !b.Risky || !strings.Contains(string(raw), `"speed": "normal"`) {
		t.Errorf("item 2: %+v\n%s", b, raw)
	}
	if o, _, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo}) }); c != 0 || filepath.Base(strings.TrimSpace(o)) != "notes-2" {
		t.Errorf("second init must not reuse the dir: %d %s", c, o)
	}
	if _, er, c := capture(t, func() int {
		return Batch([]string{"add", "notes", "--id", "fix", "--title", "Fix review findings", "--intent", "id reuse after delete"})
	}); c != 0 {
		t.Fatalf("add: %d %s", c, er)
	}
	if _, er, c := capture(t, func() int { return Batch([]string{"add", "notes", "--id", "fix", "--title", "x", "--intent", "y"}) }); c != 2 || !strings.Contains(er, "already exists") {
		t.Errorf("duplicate add: %d %s", c, er)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "batch.json"))
	json.Unmarshal(raw, &bt)
	if len(bt.Items) != 3 || bt.Items[2].Route != "debug" || bt.Items[2].Base != "main" || bt.Items[2].Intent != "id reuse after delete" {
		t.Errorf("added: %+v", bt.Items)
	}
	if _, _, c := capture(t, func() int { return Batch([]string{"init", td}) }); c != 2 {
		t.Errorf("init without --repo: %d", c)
	}
}
