//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func okRun(ctx context.Context, msg string, h []Exchange, e Emit) (string, error) { return "ok", nil }

func projects(t *testing.T, u string) ProjectList {
	t.Helper()
	res := do(t, "GET", u+"/v1/projects", "", auth)
	var l ProjectList
	if err := json.NewDecoder(res.Body).Decode(&l); err != nil || res.StatusCode != 200 {
		t.Fatalf("list: %d %v", res.StatusCode, err)
	}
	return l
}

func TestProjectsAddDefaultRemove(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	_, u, _ := sessionTest(t, okRun)
	if l := projects(t, u); len(l.Projects) != 0 || l.Default != "" {
		t.Fatalf("fresh: %+v", l)
	}
	if code, m := post(t, u+"/v1/projects", `{"dir":"`+a+`"}`); code != 200 {
		t.Fatalf("add a: %d %v", code, m)
	}
	if code, m := post(t, u+"/v1/projects", `{"dir":"`+b+`","default":true}`); code != 200 {
		t.Fatalf("add b: %d %v", code, m)
	}
	post(t, u+"/v1/projects", `{"dir":"`+a+`"}`)
	l := projects(t, u)
	if len(l.Projects) != 2 || l.Projects[0].Dir != ra || l.Projects[1].Dir != rb || l.Default != rb {
		t.Fatalf("after adds: %+v", l)
	}
	if l.Projects[0].Name != filepath.Base(ra) {
		t.Errorf("name: %q", l.Projects[0].Name)
	}
	if code, _ := post(t, u+"/v1/projects", `{"dir":"relative/x"}`); code != 400 {
		t.Errorf("relative dir: %d, want 400", code)
	}
	hdr := map[string]string{"Authorization": auth["Authorization"], "Content-Type": "application/json"}
	res := do(t, "DELETE", u+"/v1/projects", `{"dir":"`+b+`"}`, hdr)
	if res.StatusCode != 200 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if l := projects(t, u); len(l.Projects) != 1 || l.Default != "" {
		t.Errorf("after delete: %+v", l)
	}
}

func TestProjectsSurviveRestart(t *testing.T) {
	a := t.TempDir()
	ra, _ := filepath.EvalSymlinks(a)
	_, u, _ := sessionTest(t, okRun)
	h := os.Getenv("SMITHY_HOME")
	post(t, u+"/v1/projects", `{"dir":"`+a+`","default":true}`)
	_, u2, _ := sessionTest(t, okRun)
	t.Setenv("SMITHY_HOME", h)
	if l := projects(t, u2); len(l.Projects) != 1 || l.Default != ra {
		t.Errorf("reloaded: %+v", l)
	}
}

func TestSessionWithoutDirStartsInDefaultProject(t *testing.T) {
	a := t.TempDir()
	ra, _ := filepath.EvalSymlinks(a)
	_, u, _ := sessionTest(t, okRun)
	if code, _ := post(t, u+"/v1/sessions", `{"agent":"claude"}`); code != 400 {
		t.Errorf("no dir, no default: %d, want 400", code)
	}
	post(t, u+"/v1/projects", `{"dir":"`+a+`","default":true}`)
	code, m := post(t, u+"/v1/sessions", `{"agent":"claude"}`)
	if code != 201 && code != 200 {
		t.Fatalf("start: %d %v", code, m)
	}
	if m["dir"] != ra {
		t.Errorf("dir: %v, want %s", m["dir"], ra)
	}
}

func TestBrowseFolders(t *testing.T) {
	root := t.TempDir()
	rr, _ := filepath.EvalSymlinks(root)
	for _, d := range []string{"b", "a", ".hidden"} {
		os.Mkdir(filepath.Join(root, d), 0o755)
	}
	os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644)
	_, u, _ := sessionTest(t, okRun)
	res := do(t, "GET", u+"/v1/fs?dir="+url.QueryEscape(root), "", auth)
	var l DirList
	json.NewDecoder(res.Body).Decode(&l)
	if res.StatusCode != 200 || l.Dir != rr || l.Parent != filepath.Dir(rr) || len(l.Dirs) != 2 || l.Dirs[0] != "a" || l.Dirs[1] != "b" {
		t.Errorf("browse: %d %+v", res.StatusCode, l)
	}
	if res := do(t, "GET", u+"/v1/fs?dir="+url.QueryEscape(filepath.Join(root, "file.txt")), "", auth); res.StatusCode != 400 {
		t.Errorf("file: %d, want 400", res.StatusCode)
	}
	res = do(t, "GET", u+"/v1/fs", "", auth)
	json.NewDecoder(res.Body).Decode(&l)
	if res.StatusCode != 200 || l.Dir == "" {
		t.Errorf("home: %d %+v", res.StatusCode, l)
	}
}

func TestClientProjects(t *testing.T) {
	a := t.TempDir()
	ra, _ := filepath.EvalSymlinks(a)
	_, u, _ := sessionTest(t, okRun)
	c := &Client{Base: u, Token: testToken}
	ctx := context.Background()
	if l, err := c.AddProject(ctx, a, true); err != nil || l.Default != ra {
		t.Fatalf("add: %+v %v", l, err)
	}
	if l, err := c.Projects(ctx); err != nil || len(l.Projects) != 1 {
		t.Fatalf("list: %+v %v", l, err)
	}
	if l, err := c.ListDirs(ctx, filepath.Dir(ra)); err != nil || !slices.Contains(l.Dirs, filepath.Base(ra)) {
		t.Errorf("dirs: %+v %v", l, err)
	}
	if l, err := c.RemoveProject(ctx, a); err != nil || len(l.Projects) != 0 {
		t.Errorf("remove: %+v %v", l, err)
	}
}
