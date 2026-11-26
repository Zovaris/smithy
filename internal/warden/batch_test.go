package warden

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/testkit"
)

func TestBatchInitAdd(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("HOME", root)
	repo := filepath.Join(root, "repo")
	td := filepath.Join(repo, "docs", "specs", "notes")
	os.MkdirAll(td, 0o755)
	testkit.Git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(td, "01-pagination.md"), "# 01 · Add pagination\nStatus: todo\nBlocked by: none\nSpeed: normal\nPR: -\n")
	writeFile(t, filepath.Join(td, "02-reset.md"), "# 02 · Password reset\nStatus: todo\nBlocked by: 01\nSpeed: fast\nPR: -\n")
	writeFile(t, filepath.Join(td, "03-old.md"), "# 03 · Old\nStatus: done\nBlocked by: none\nPR: -\n")
	writeFile(t, filepath.Join(td, "04-list.md"), "# 04 · List view\nStatus: todo\nBlocked by: 03\nPR: -\n")
	testkit.Git(t, repo, "add", "-A")
	testkit.Git(t, repo, "commit", "-q", "-m", "tasks")
	testkit.Git(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
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
	testkit.Git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README.md"), "x")
	testkit.Git(t, repo, "add", "-A")
	testkit.Git(t, repo, "commit", "-q", "-m", "init")
	testkit.Git(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
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
	testkit.Git(t, repo, "init", "-q", "-b", "main")
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

func TestBatchInitIntentRepoSubdir(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("HOME", root)
	top := filepath.Join(root, "repo")
	sub := filepath.Join(top, "pkg")
	td := filepath.Join(sub, "notes")
	os.MkdirAll(td, 0o755)
	testkit.Git(t, top, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(td, "01-a.md"), "# 01 · A thing\nStatus: todo\nBlocked by: none\nPR: -\n")
	testkit.Git(t, top, "add", "-A")
	testkit.Git(t, top, "commit", "-q", "-m", "init")
	testkit.Git(t, top, "update-ref", "refs/remotes/origin/main", "HEAD")
	o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", sub}) })
	if c != 0 {
		t.Fatalf("%d %s", c, er)
	}
	var bt briefBatch
	raw, _ := os.ReadFile(filepath.Join(strings.TrimSpace(o), "batch.json"))
	json.Unmarshal(raw, &bt)
	if want := "pkg/notes/01-a.md"; bt.Items[0].Intent != want {
		t.Errorf("intent %q want %q", bt.Items[0].Intent, want)
	}
}

func TestBatchInitRiskyEmpty(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("HOME", root)
	repo := filepath.Join(root, "repo")
	td := filepath.Join(repo, "notes")
	os.MkdirAll(td, 0o755)
	testkit.Git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(td, "01-a.md"), "# 01 · Alpha\nStatus: todo\nBlocked by: none\nPR: -\n")
	for _, a := range [][]string{{"--risky="}, {"--risky", ""}} {
		args := append([]string{"init", td, "--repo", repo}, a...)
		if _, er, c := capture(t, func() int { return Batch(args) }); c == 0 || !strings.Contains(er, "--risky") {
			t.Errorf("%v: empty --risky must fail: %d %s", a, c, er)
		}
	}
}

func TestBatchInitSettingsDefaults(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("HOME", root)
	repo := filepath.Join(root, "repo")
	td := filepath.Join(repo, "notes")
	os.MkdirAll(td, 0o755)
	testkit.Git(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(td, "01-a.md"), "# 01 · Add a\nStatus: todo\nBlocked by: none\nPR: -\n")
	read := func(dir string) map[string]any {
		var m map[string]any
		raw, _ := os.ReadFile(filepath.Join(strings.TrimSpace(dir), "batch.json"))
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%v %s", err, raw)
		}
		return m
	}
	init := func(extra ...string) (map[string]any, string, int) {
		o, er, c := capture(t, func() int { return Batch(append([]string{"init", td, "--repo", repo}, extra...)) })
		if c != 0 {
			return nil, er, c
		}
		return read(o), er, c
	}
	t.Setenv("SMITHY_CONFIG", filepath.Join(root, "none.json"))
	m, er, c := init()
	if c != 0 || m["mode"] != "smelt normal" || m["cleanup"] != "auto" || m["merge"] != "" || m["max"] != float64(5) {
		t.Fatalf("defaults: %d %s %v", c, er, m)
	}
	cfg := filepath.Join(root, "user-config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	os.RemoveAll(filepath.Join(root, "warden"))
	writeFile(t, cfg, `{"warden":{"max":3,"cleanup":"ask","mode":"smelt full","merge":"manual"}}`)
	os.RemoveAll(filepath.Join(root, "warden"))
	m, er, c = init()
	if c != 0 || m["mode"] != "smelt full" || m["cleanup"] != "ask" || m["merge"] != "manual" || m["max"] != float64(3) {
		t.Fatalf("from config: %d %s %v", c, er, m)
	}
	os.RemoveAll(filepath.Join(root, "warden"))
	m, er, c = init("--max", "2", "--merge", "auto", "--cleanup", "auto", "--mode", "plain")
	if c != 0 || m["mode"] != "plain" || m["cleanup"] != "auto" || m["merge"] != "auto" || m["max"] != float64(2) {
		t.Fatalf("flags win: %d %s %v", c, er, m)
	}
	for _, bad := range [][]string{{"--cleanup", "never"}, {"--mode", "turbo"}} {
		if _, _, c := init(bad...); c != 2 {
			t.Errorf("%v must be refused: %d", bad, c)
		}
	}
}

func autoRepo(t *testing.T) (root, repo, td string) {
	root, _ = filepath.EvalSymlinks(t.TempDir())
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("HOME", root)
	repo = filepath.Join(root, "repo")
	td = filepath.Join(root, "plans", "repo", "auto-2026-01-01-1")
	os.MkdirAll(td, 0o755)
	os.MkdirAll(repo, 0o755)
	testkit.Git(t, repo, "init", "-q", "-b", "main")
	testkit.Git(t, repo, "commit", "-q", "--allow-empty", "-m", "x")
	testkit.Git(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	writeFile(t, filepath.Join(td, "01-tes-1-add-pagination.md"), "# 01 · Add pagination\nStatus: todo\nBlocked by: none\nSpeed: full\nModel: cheap\nPR: -\nSource: https://linear.app/x/TES-1\nKey: TES-1\nRef: linear:uuid-1\n")
	writeFile(t, filepath.Join(td, "02-tes-2-list-view.md"), "# 02 · List view\nStatus: todo\nBlocked by: none\nSpeed: route\nModel: route\nPR: -\nKey: TES-2\nRef: linear:uuid-2\n")
	return root, repo, td
}

func TestBatchInitAutoTaskDir(t *testing.T) {
	root, repo, td := autoRepo(t)
	cfg := filepath.Join(root, "config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	writeFile(t, cfg, `{"auto":{"merge":"auto"}}`)
	testkit.Git(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	testkit.Git(t, repo, "update-ref", "refs/remotes/origin/release/x", "HEAD")
	o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo, "--base", "release/x"}) })
	if c != 0 {
		t.Fatalf("%d %s", c, er)
	}
	raw, _ := os.ReadFile(filepath.Join(strings.TrimSpace(o), "batch.json"))
	var m struct {
		Merge      string `json:"merge"`
		Production any    `json:"production"`
		Items      []struct{ Speed, Model, Branch string }
	}
	if err := json.Unmarshal(raw, &m); err != nil || m.Merge != "auto" || m.Production != false || len(m.Items) != 2 {
		t.Fatalf("%v %s", err, raw)
	}
	a, b := m.Items[0], m.Items[1]
	if a.Speed != "full" || a.Model != "cheap" || !strings.Contains(a.Branch, "TES-1") {
		t.Errorf("item 1: %+v", a)
	}
	if b.Speed != "normal" || b.Model != "" || strings.Contains(string(raw), `"model": "route"`) {
		t.Errorf("item 2: %+v\n%s", b, raw)
	}
}

func TestBatchInitAutoOnProductionBase(t *testing.T) {
	root, repo, td := autoRepo(t)
	t.Setenv("SMITHY_CONFIG", filepath.Join(root, "none.json"))
	testkit.Git(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo}) })
	if c != 0 {
		t.Fatalf("%d %s", c, er)
	}
	raw, _ := os.ReadFile(filepath.Join(strings.TrimSpace(o), "batch.json"))
	if !strings.Contains(string(raw), `"production": "main"`) || !strings.Contains(string(raw), `"merge": "manual"`) {
		t.Errorf("%s", raw)
	}
}

func TestBatchInitAutoUnknownDefaultIsProduction(t *testing.T) {
	root, repo, td := autoRepo(t)
	cfg := filepath.Join(root, "config.json")
	t.Setenv("SMITHY_CONFIG", cfg)
	writeFile(t, cfg, `{"auto":{"merge":"auto"}}`)
	o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo}) })
	if c != 0 {
		t.Fatalf("%d %s", c, er)
	}
	raw, _ := os.ReadFile(filepath.Join(strings.TrimSpace(o), "batch.json"))
	if !strings.Contains(string(raw), `"production": "main"`) {
		t.Errorf("unknown default branch must fail safe to production=base: %s", raw)
	}
}

func TestKnownDefaultTimesOut(t *testing.T) {
	_, repo, _ := autoRepo(t)
	bin := t.TempDir()
	writeFile(t, filepath.Join(bin, "gh"), "#!/bin/sh\nexec sleep 5\n")
	os.Chmod(filepath.Join(bin, "gh"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	defer func(d time.Duration) { lookupTimeout = d }(lookupTimeout)
	lookupTimeout = 200 * time.Millisecond
	start := time.Now()
	if d := knownDefault(repo); d != "" {
		t.Errorf("knownDefault = %q, want \"\" (fail safe)", d)
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("lookup took %v; the timeout did not stop gh", el)
	}
}

func TestBatchSet(t *testing.T) {
	root, repo, td := autoRepo(t)
	t.Setenv("SMITHY_CONFIG", filepath.Join(root, "none.json"))
	o, er, c := capture(t, func() int { return Batch([]string{"init", td, "--repo", repo}) })
	if c != 0 {
		t.Fatalf("%d %s", c, er)
	}
	name := filepath.Base(strings.TrimSpace(o))
	if _, er, c := capture(t, func() int {
		return Batch([]string{"set", name, "merge=auto", "production=main", "cleanup=ask", "mode=smelt full", "base_sha=main:abc123"})
	}); c != 0 {
		t.Fatalf("set: %d %s", c, er)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "warden", name, "batch.json"))
	var m map[string]any
	json.Unmarshal(raw, &m)
	if m["merge"] != "auto" || m["production"] != "main" || m["cleanup"] != "ask" || m["mode"] != "smelt full" {
		t.Errorf("%s", raw)
	}
	if bs, _ := m["base_sha"].(map[string]any); bs["main"] != "abc123" {
		t.Errorf("base_sha: %s", raw)
	}
	capture(t, func() int { return Batch([]string{"set", name, "production=false"}) })
	raw, _ = os.ReadFile(filepath.Join(root, "warden", name, "batch.json"))
	if !strings.Contains(string(raw), `"production": false`) {
		t.Errorf("production=false: %s", raw)
	}
	for _, bad := range [][]string{{"merge=yes"}, {"cleanup=never"}, {"mode=turbo"}, {"base_sha=main"}, {"base_sha=other:abc"}, {"production=no-such-branch"}, {"items=[]"}, {"merge"}, {}} {
		if _, _, c := capture(t, func() int { return Batch(append([]string{"set", name}, bad...)) }); c != 2 {
			t.Errorf("set %v must be refused: %d", bad, c)
		}
	}
	if _, _, c := capture(t, func() int { return Batch([]string{"set", "nope", "merge=auto"}) }); c != 2 {
		t.Errorf("unknown batch: %d", c)
	}
}
