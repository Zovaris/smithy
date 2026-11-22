package tasks

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sandbox(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("SMITHY_HOME", filepath.Join(root, "smithy"))
	t.Setenv("SMITHY_CONFIG", filepath.Join(root, "config.json"))
	t.Setenv("WARDEN_HOME", filepath.Join(root, "warden"))
	t.Setenv("SMELT_PLANS", filepath.Join(root, "plans"))
	t.Setenv("SMELT_TASKS_DIRS", "")
	return repo
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

func run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	ro, wo, _ := os.Pipe()
	re, we, _ := os.Pipe()
	os.Stdout, os.Stderr = wo, we
	code := Main(args)
	os.Stdout, os.Stderr = oldOut, oldErr
	wo.Close()
	we.Close()
	o, _ := io.ReadAll(ro)
	e, _ := io.ReadAll(re)
	return string(o), string(e), code
}

func taskBody(nn, title, status, blocked, pr string) string {
	return "# " + nn + " · " + title + "\nStatus: " + status + "\nBlocked by: " + blocked + "\nPR: " + pr + "\n\n## What\n"
}

func TestParse(t *testing.T) {
	cases := []struct {
		name, file, body string
		want             Task
	}{
		{"full", "01-login.md", "# 01 · Add login\nStatus: Doing\nBlocked by: none\nPR: https://x/pull/1\n", Task{NN: "01", Title: "Add login", Status: "doing", PR: "https://x/pull/1"}},
		{"no number in title", "02-x.md", "# Plain title\nStatus: done\n", Task{NN: "02", Title: "Plain title", Status: "done", PR: "-"}},
		{"dot without number", "03-x.md", "# Hello · world\n", Task{NN: "03", Title: "Hello · world", Status: "todo", PR: "-"}},
		{"blockers list", "04-x.md", "# 04 · T\nBlocked by: 01, 03\n", Task{NN: "04", Title: "T", Status: "todo", PR: "-", Blockers: []string{"01", "03"}}},
		{"blockers commas", "05-x.md", "# 05 · T\nBlocked by: 01,02\n", Task{NN: "05", Title: "T", Status: "todo", PR: "-", Blockers: []string{"01", "02"}}},
		{"blockers with hash", "06-x.md", "# 06 · T\nBlocked by: #2\n", Task{NN: "06", Title: "T", Status: "todo", PR: "-", Blockers: []string{"2"}}},
		{"none any case", "07-x.md", "# 07 · T\nBlocked by: None\n", Task{NN: "07", Title: "T", Status: "todo", PR: "-"}},
		{"header after section ignored", "08-x.md", "# 08 · T\n## What\nStatus: done\n", Task{NN: "08", Title: "T", Status: "todo", PR: "-"}},
		{"empty file", "09-x.md", "", Task{NN: "09", Status: "todo", PR: "-"}},
		{"extra headers skipped", "10-x.md", "# 10 · T\nOwner: me\nPR: -\nStatus: todo\n", Task{NN: "10", Title: "T", Status: "todo", PR: "-"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), c.file)
			write(t, f, c.body)
			got, err := Parse(f)
			if err != nil {
				t.Fatal(err)
			}
			c.want.File = f
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v want %+v", got, c.want)
			}
		})
	}
	if _, err := Parse(filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Error("missing file: want error")
	}
}

func TestUnblocked(t *testing.T) {
	ts := []Task{
		{NN: "01", Status: "done"},
		{NN: "02", Status: "dropped"},
		{NN: "03", Status: "todo", Blockers: []string{"01", "02"}},
		{NN: "04", Status: "todo", Blockers: []string{"03"}},
		{NN: "05", Status: "doing"},
		{NN: "06", Status: "todo", Blockers: []string{"05"}},
		{NN: "07", Status: "todo"},
		{NN: "08", Status: "todo", Blockers: []string{"99"}},
	}
	var got []string
	for _, u := range Unblocked(ts) {
		got = append(got, u.NN)
	}
	if want := []string{"03", "07"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestSet(t *testing.T) {
	cases := []struct {
		name  string
		pairs []string
		check func(t *testing.T, tk Task, body string)
		err   string
	}{
		{"update status and pr", []string{"status=doing", "pr=https://x/pull/2"}, func(t *testing.T, tk Task, body string) {
			if tk.Status != "doing" || tk.PR != "https://x/pull/2" {
				t.Errorf("got %+v", tk)
			}
			if !strings.Contains(body, "\nPR: https://x/pull/2\n") {
				t.Errorf("PR key respelled:\n%s", body)
			}
		}, ""},
		{"add missing header", []string{"owner=ana"}, func(t *testing.T, tk Task, body string) {
			if !strings.Contains(body, "PR: -\nOwner: ana\n\n## What") {
				t.Errorf("body:\n%s", body)
			}
		}, ""},
		{"invalid status", []string{"status=wip"}, nil, "Status must be one of"},
		{"not key=value", []string{"oops"}, nil, "not key=value"},
		{"blocked by update", []string{"blocked by=02"}, func(t *testing.T, tk Task, body string) {
			if !reflect.DeepEqual(tk.Blockers, []string{"02"}) {
				t.Errorf("blockers %v", tk.Blockers)
			}
		}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "01-a.md")
			write(t, f, taskBody("01", "Title", "todo", "none", "-"))
			tk, err := Set(f, c.pairs)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err %v want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(f)
			c.check(t, tk, string(b))
		})
	}
	if _, err := Set(filepath.Join(t.TempDir(), "nope.md"), []string{"status=done"}); err == nil {
		t.Error("missing file: want error")
	}
	f := filepath.Join(t.TempDir(), "01-a.md")
	write(t, f, "no heading\n")
	if _, err := Set(f, []string{"status=done"}); err == nil || !strings.Contains(err.Error(), "no '# NN") {
		t.Errorf("no heading: %v", err)
	}
}

func TestAdd(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "list")
	f1, err := Add(dir, "Add Login Page!", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(f1) != "01-add-login-page.md" {
		t.Errorf("f1 %s", f1)
	}
	b, _ := os.ReadFile(f1)
	if want := "# 01 · Add Login Page!\nStatus: todo\nBlocked by: none\nPR: -\n\n## What\n\n## Acceptance criteria\n- [ ] \n"; string(b) != want {
		t.Errorf("body:\n%s", b)
	}
	write(t, filepath.Join(dir, "spec.md"), "spec")
	f2, err := Add(dir, "Rate limit", "01", "PROJ-12")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(f2) != "02-rate-limit.md" {
		t.Errorf("f2 %s", f2)
	}
	b, _ = os.ReadFile(f2)
	if !strings.Contains(string(b), "Blocked by: 01\nPR: -\nKey: PROJ-12\nSpec: ./spec.md\n") {
		t.Errorf("body:\n%s", b)
	}
	tk, _ := Parse(f2)
	if !reflect.DeepEqual(tk.Blockers, []string{"01"}) || tk.Title != "Rate limit" {
		t.Errorf("parsed %+v", tk)
	}
	f3, err := Add(dir, "!!!", "", "")
	if err != nil || filepath.Base(f3) != "03-task.md" {
		t.Errorf("empty slug: %s %v", f3, err)
	}
	blocker := filepath.Join(t.TempDir(), "file")
	write(t, blocker, "x")
	if _, err := Add(filepath.Join(blocker, "sub"), "x", "", ""); err == nil {
		t.Error("unwritable dir: want error")
	}
}

func TestDirs(t *testing.T) {
	repo := sandbox(t)
	a := filepath.Join(repo, "docs", "specs", "a")
	b := filepath.Join(repo, ".docs", "specs", "b")
	c := filepath.Join(PlansHome(), filepath.Base(repo), "c")
	d := filepath.Join(repo, "extra", "d")
	empty := filepath.Join(repo, "docs", "specs", "empty")
	for _, dir := range []string{a, b, c, d} {
		write(t, filepath.Join(dir, "01-x.md"), taskBody("01", "x", "todo", "none", "-"))
	}
	write(t, filepath.Join(empty, "notes.md"), "n")
	now := time.Now()
	for i, dir := range []string{a, b, c, d} {
		ts := now.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(dir, ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	if got := Dirs(); len(got) != 3 {
		t.Fatalf("without env: %v", got)
	}
	t.Setenv("SMELT_TASKS_DIRS", "extra:"+filepath.Join(repo, "missing")+":")
	got := Dirs()
	want := []string{d, c, b, a}
	for i := range want {
		want[i], _ = filepath.EvalSymlinks(want[i])
	}
	if len(got) != 4 {
		t.Fatalf("got %v", got)
	}
	for i, g := range got {
		g, _ = filepath.EvalSymlinks(g)
		if g != want[i] {
			t.Errorf("order[%d] = %s want %s", i, g, want[i])
		}
	}
}

func TestResolve(t *testing.T) {
	repo := sandbox(t)
	if _, err := resolve(""); err == nil || !strings.Contains(err.Error(), "no task dir found") {
		t.Errorf("none: %v", err)
	}
	if _, err := resolve(filepath.Join(repo, "nope")); err == nil {
		t.Error("missing dir: want error")
	}
	dir := filepath.Join(repo, "docs", "specs", "a")
	write(t, filepath.Join(dir, "01-x.md"), taskBody("01", "x", "todo", "none", "-"))
	if got, err := resolve(""); err != nil || filepath.Base(got) != "a" {
		t.Errorf("auto: %s %v", got, err)
	}
	if got, err := resolve(dir); err != nil || got != dir {
		t.Errorf("explicit: %s %v", got, err)
	}
}

func TestSummary(t *testing.T) {
	ts := []Task{
		{NN: "01", Status: "done"},
		{NN: "02", Status: "doing"},
		{NN: "03", Status: "todo", Blockers: []string{"01"}},
		{NN: "04", Status: "todo", Blockers: []string{"02"}},
	}
	if got, want := Summary(ts), "1/4 done, 1 doing, 1 ready"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got, want := Summary(nil), "0/0 done, 0 doing, 0 ready"; got != want {
		t.Errorf("empty: %q", got)
	}
}

func TestPlansHome(t *testing.T) {
	t.Setenv("SMELT_PLANS", "")
	t.Setenv("SMITHY_HOME", "/h")
	if got := PlansHome(); got != "/h/plans" {
		t.Errorf("got %s", got)
	}
	t.Setenv("SMELT_PLANS", "/p")
	if got := PlansHome(); got != "/p" {
		t.Errorf("got %s", got)
	}
}

func TestLoadSortsAndRemembers(t *testing.T) {
	repo := sandbox(t)
	dir := filepath.Join(repo, "docs", "specs", "a")
	write(t, filepath.Join(dir, "10-ten.md"), taskBody("10", "Ten", "todo", "none", "-"))
	write(t, filepath.Join(dir, "02-two.md"), taskBody("02", "Two", "done", "none", "-"))
	write(t, filepath.Join(dir, "README.md"), "x")
	ts := Load(dir)
	if len(ts) != 2 || ts[0].NN != "02" || ts[1].NN != "10" {
		t.Fatalf("got %+v", ts)
	}
	if _, err := os.Stat(indexFile()); err != nil {
		t.Errorf("index not written: %v", err)
	}
	before, _ := os.ReadFile(indexFile())
	Load(dir)
	after, _ := os.ReadFile(indexFile())
	if string(before) != string(after) {
		t.Error("index rewritten for a known dir")
	}
}

func TestBoard(t *testing.T) {
	repo := sandbox(t)
	if Board(false) != nil {
		t.Error("empty: want nil")
	}
	dir := filepath.Join(repo, "docs", "specs", "a")
	write(t, filepath.Join(dir, "01-x.md"), taskBody("01", "x", "done", "none", "-"))
	write(t, filepath.Join(dir, "02-y.md"), taskBody("02", "y", "todo", "01", "-"))
	lines := Board(false)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "repo/a") || !strings.Contains(lines[0], "1/2 done, 0 doing, 1 ready") {
		t.Fatalf("board: %q", lines)
	}

	other := filepath.Join(t.TempDir(), "elsewhere", "b")
	write(t, filepath.Join(other, "01-z.md"), taskBody("01", "z", "todo", "none", "-"))
	Load(other)
	gone := filepath.Join(t.TempDir(), "gone", "c")
	write(t, filepath.Join(gone, "01-z.md"), taskBody("01", "z", "todo", "none", "-"))
	Load(gone)
	if got := Board(false); len(got) != 1 {
		t.Errorf("without --all: %q", got)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(Board(true), "\n")
	if !strings.Contains(all, "repo/b") || strings.Contains(all, "/c ") || strings.Contains(all, gone) {
		t.Errorf("--all:\n%s", all)
	}
	if _, ok := readIndex()[gone]; ok {
		t.Error("vanished dir not pruned from the index")
	}
	if _, ok := readIndex()[other]; !ok {
		t.Error("live dir pruned from the index")
	}
}

func TestBoardBatches(t *testing.T) {
	sandbox(t)
	wh := os.Getenv("WARDEN_HOME")
	write(t, filepath.Join(wh, "live", "batch.json"), `{"items":[{"id":"a"},{"id":"b"},{"id":"c"}]}`)
	write(t, filepath.Join(wh, "live", "a.json"), `{"state":"merged"}`)
	write(t, filepath.Join(wh, "done", "batch.json"), `{"items":[{"id":"a"}]}`)
	write(t, filepath.Join(wh, "done", "a.json"), `{"state":"merged"}`)
	write(t, filepath.Join(wh, "empty", "batch.json"), `{"items":[]}`)
	got := strings.Join(Board(false), "\n")
	if !strings.Contains(got, "batch live") || !strings.Contains(got, "1 merged, 2 queued") {
		t.Errorf("live batch:\n%s", got)
	}
	if strings.Contains(got, "batch done") || strings.Contains(got, "batch empty") {
		t.Errorf("final or empty batch shown:\n%s", got)
	}
	if all := strings.Join(Board(true), "\n"); !strings.Contains(all, "batch done") {
		t.Errorf("--all should include final batch:\n%s", all)
	}
}

func TestMainCommands(t *testing.T) {
	repo := sandbox(t)
	dir := filepath.Join(repo, "docs", "specs", "a")

	if _, e, c := run(t); c != 2 || !strings.Contains(e, "usage") {
		t.Errorf("no args: %d %q", c, e)
	}
	if _, _, c := run(t, "bogus"); c != 2 {
		t.Errorf("bogus: %d", c)
	}
	if _, e, c := run(t, "list"); c != 1 || !strings.Contains(e, "no task dir") {
		t.Errorf("list none: %d %q", c, e)
	}
	if _, _, c := run(t, "where"); c != 3 {
		t.Errorf("where none: %d", c)
	}
	if _, e, c := run(t, "board"); c != 3 || !strings.Contains(e, "no task lists") {
		t.Errorf("board none: %d %q", c, e)
	}
	if _, _, c := run(t, "add"); c != 2 {
		t.Errorf("add no title: %d", c)
	}
	if _, _, c := run(t, "set", "x"); c != 2 {
		t.Errorf("set short: %d", c)
	}

	o, _, c := run(t, "add", dir, "First", "--key", "K-1")
	if c != 0 || filepath.Base(strings.TrimSpace(o)) != "01-first.md" {
		t.Fatalf("add: %d %q", c, o)
	}
	o, _, c = run(t, "add", dir, "Second", "--blocked-by", "01")
	if c != 0 || filepath.Base(strings.TrimSpace(o)) != "02-second.md" {
		t.Fatalf("add 2: %d %q", c, o)
	}
	if o, _, c = run(t, "add", "Third"); c != 0 || filepath.Base(strings.TrimSpace(o)) != "03-third.md" {
		t.Errorf("add resolved dir: %d %q", c, o)
	}

	o, _, c = run(t, "list", dir)
	if c != 0 || !strings.Contains(o, "01") || !strings.Contains(o, "First") || !strings.Contains(o, "0/3 done, 0 doing, 2 ready") {
		t.Errorf("list: %d\n%s", c, o)
	}
	o, _, c = run(t, "ready", dir)
	if c != 0 || strings.Count(o, "\n") != 2 {
		t.Errorf("ready: %d %q", c, o)
	}
	o, _, c = run(t, "next", dir)
	if c != 0 || !strings.HasSuffix(strings.TrimSpace(o), "01-first.md") {
		t.Errorf("next: %d %q", c, o)
	}
	if o, _, c = run(t, "where"); c != 0 || !strings.Contains(o, "a") {
		t.Errorf("where: %d %q", c, o)
	}
	if o, _, c = run(t, "board"); c != 0 || !strings.Contains(o, "repo/a") {
		t.Errorf("board: %d %q", c, o)
	}

	f1 := filepath.Join(dir, "01-first.md")
	o, _, c = run(t, "set", f1, "status=doing")
	if c != 0 || !strings.Contains(o, `"status":"doing"`) {
		t.Errorf("set: %d %q", c, o)
	}
	if _, e, c := run(t, "set", f1, "status=nope"); c != 1 || !strings.Contains(e, "Status must be") {
		t.Errorf("set bad: %d %q", c, e)
	}

	_, e, c := run(t, "next", dir)
	if c != 0 {
		t.Fatalf("next with 03 free: %d %q", c, e)
	}
	run(t, "set", filepath.Join(dir, "03-third.md"), "status=doing")
	_, e, c = run(t, "next", dir)
	if c != 3 || !strings.Contains(e, "nothing ready") {
		t.Errorf("next blocked: %d %q", c, e)
	}
	if o, _, c := run(t, "ready", dir); c != 3 || o != "" {
		t.Errorf("ready blocked: %d %q", c, o)
	}
	for _, n := range []string{"01-first", "02-second", "03-third"} {
		run(t, "set", filepath.Join(dir, n+".md"), "status=done")
	}
	_, e, c = run(t, "next", dir)
	if c != 3 || !strings.Contains(e, "all done") {
		t.Errorf("next all done: %d %q", c, e)
	}
	if _, e, c := run(t, "list", filepath.Join(repo, "nope")); c != 1 || !strings.Contains(e, "no task dir") {
		t.Errorf("list missing: %d %q", c, e)
	}
}
