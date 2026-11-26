//go:build harness

package daemon

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func taskProject(t *testing.T) (proj, file string) {
	t.Helper()
	proj = t.TempDir()
	dir := filepath.Join(proj, "docs", "specs", "v1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(dir, "01-login.md")
	os.WriteFile(file, []byte("# 01 · Login page\nStatus: todo\nBlocked by: none\n\n## What\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "02-logout.md"), []byte("# 02 · Logout\nStatus: doing\nBlocked by: 01\n"), 0o644)
	return proj, file
}

func getTasks(t *testing.T, u, proj string) TaskBoard {
	t.Helper()
	res := do(t, "GET", u+"/v1/tasks?project="+url.QueryEscape(proj), "", auth)
	defer res.Body.Close()
	var b TaskBoard
	if err := json.NewDecoder(res.Body).Decode(&b); err != nil || res.StatusCode != 200 {
		t.Fatalf("tasks: %d %v", res.StatusCode, err)
	}
	return b
}

func TestTasksListAndMove(t *testing.T) {
	proj, file := taskProject(t)
	_, u, _ := sessionTest(t, okRun)
	t.Setenv("SMELT_PLANS", t.TempDir())
	if _, err := AddProject(proj, true); err != nil {
		t.Fatal(err)
	}
	b := getTasks(t, u, proj)
	if len(b.Lists) != 1 || len(b.Lists[0].Tasks) != 2 {
		t.Fatalf("board: %+v", b)
	}
	tk := b.Lists[0].Tasks[0]
	if tk.Title != "Login page" || tk.Status != "todo" || tk.NN != "01" || !strings.HasSuffix(tk.File, "01-login.md") {
		t.Fatalf("task: %+v", tk)
	}
	if got := b.Lists[0].Tasks[1].Blockers; len(got) != 1 || got[0] != "01" {
		t.Errorf("blockers: %v", got)
	}
	if def := getTasks(t, u, ""); len(def.Lists) != 1 {
		t.Errorf("default project: %+v", def)
	}

	hdr := map[string]string{"Authorization": auth["Authorization"]}
	res := do(t, "PUT", u+"/v1/tasks/status", `{"file":"`+tk.File+`","status":"done"}`, hdr)
	if res.StatusCode != 200 {
		t.Fatalf("move: %d", res.StatusCode)
	}
	raw, _ := os.ReadFile(file)
	if !strings.Contains(string(raw), "Status: done\n") || !strings.Contains(string(raw), "## What") {
		t.Errorf("file after move:\n%s", raw)
	}
	if res := do(t, "PUT", u+"/v1/tasks/status", `{"file":"`+tk.File+`","status":"bogus"}`, hdr); res.StatusCode != 400 {
		t.Errorf("bad status: %d, want 400", res.StatusCode)
	}
	outside := filepath.Join(t.TempDir(), "01-x.md")
	os.WriteFile(outside, []byte("# 01 · X\nStatus: todo\n"), 0o644)
	if res := do(t, "PUT", u+"/v1/tasks/status", `{"file":"`+outside+`","status":"done"}`, hdr); res.StatusCode != 400 {
		t.Errorf("file outside a project's task dirs: %d, want 400", res.StatusCode)
	}
	if raw, _ := os.ReadFile(outside); !strings.Contains(string(raw), "Status: todo") {
		t.Errorf("outside file was changed")
	}
}
