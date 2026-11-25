package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/apiloop"
)

func call(name string, args any) apiloop.ToolCall {
	b, _ := json.Marshal(args)
	return apiloop.ToolCall{ID: "1", Name: name, Args: b}
}

func newSet(t *testing.T) (*Set, string) {
	t.Helper()
	dir := t.TempDir()
	return &Set{
		Cwd:   dir,
		Home:  t.TempDir(),
		Allow: []string{"Read", "Edit", "Write", "Glob", "Grep", "Bash(echo *)", "Bash(ls)", "Bash(ls *)", "Bash(cat *)", "Bash(sleep *)", "Bash(seq *)"},
		Deny:  []string{"Bash(*>*)", "Bash(curl *)", "Read(~/.ssh/**)", "Edit(**/.git/hooks/**)"},
	}, dir
}

func run(t *testing.T, s *Set, c apiloop.ToolCall) (string, error) {
	t.Helper()
	if ok, why := s.Permit(c); !ok {
		t.Fatalf("%s not permitted: %s", c.Name, why)
	}
	return s.Call(context.Background(), c)
}

func TestReadNumbersAndPages(t *testing.T) {
	s, dir := newSet(t)
	var b strings.Builder
	for i := 1; i <= 2500; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte(b.String()), 0o644)
	out, err := run(t, s, call("read", map[string]any{"path": "a.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "     1\tline 1\n") || !strings.Contains(out, "2000\tline 2000") || strings.Contains(out, "line 2001\n") {
		t.Fatalf("want lines 1-2000 numbered, got tail %q", out[len(out)-200:])
	}
	if !strings.Contains(out, "offset 2001") {
		t.Fatalf("want a paging hint, got %q", out[len(out)-200:])
	}
	out, _ = run(t, s, call("read", map[string]any{"path": "a.txt", "offset": 2499, "limit": 5}))
	if !strings.HasPrefix(out, "  2499\tline 2499\n  2500\tline 2500") {
		t.Fatalf("paged read: %q", out)
	}
}

func TestReadByteCap(t *testing.T) {
	s, dir := newSet(t)
	long := strings.Repeat("x", 1000) + "\n"
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(strings.Repeat(long, 200)), 0o644)
	out, _ := run(t, s, call("read", map[string]any{"path": "big.txt"}))
	if len(out) > MaxBytes+200 || !strings.Contains(out, "offset ") {
		t.Fatalf("want ≤ %d bytes with a paging hint, got %d", MaxBytes, len(out))
	}
}

func TestWriteAndEdit(t *testing.T) {
	s, dir := newSet(t)
	if _, err := run(t, s, call("write", map[string]any{"path": "sub/f.go", "content": "a := 1\nb := 1\n"})); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, s, call("edit", map[string]any{"path": "sub/f.go", "old": "1", "new": "2"})); err == nil {
		t.Fatal("ambiguous edit must fail")
	}
	if _, err := run(t, s, call("edit", map[string]any{"path": "sub/f.go", "old": "missing", "new": "2"})); err == nil {
		t.Fatal("missing text must fail")
	}
	if _, err := run(t, s, call("edit", map[string]any{"path": "sub/f.go", "old": "b := 1", "new": "b := 2"})); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, s, call("edit", map[string]any{"path": "sub/f.go", "old": ":=", "new": "=", "replace_all": true})); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "sub/f.go"))
	if string(got) != "a = 1\nb = 2\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyPatchOnlyForOpenAI(t *testing.T) {
	s, dir := newSet(t)
	has := func() bool {
		for _, sp := range s.Specs() {
			if sp.Name == "apply_patch" {
				return true
			}
		}
		return false
	}
	s.Model = "claude-sonnet-4"
	if has() {
		t.Fatal("apply_patch offered to a non-OpenAI model")
	}
	s.Model = "gpt-5"
	if !has() {
		t.Fatal("apply_patch missing for gpt-5")
	}
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("x\n"), 0o644)
	patch := "*** Begin Patch\n*** Update File: m.txt\n@@\n one\n-two\n+TWO\n three\n*** Add File: n.txt\n+new\n*** Delete File: gone.txt\n*** End Patch\n"
	if _, err := run(t, s, call("apply_patch", map[string]any{"patch": patch})); err != nil {
		t.Fatal(err)
	}
	m, _ := os.ReadFile(filepath.Join(dir, "m.txt"))
	n, _ := os.ReadFile(filepath.Join(dir, "n.txt"))
	_, gone := os.Stat(filepath.Join(dir, "gone.txt"))
	if string(m) != "one\nTWO\nthree\n" || string(n) != "new\n" || gone == nil {
		t.Fatalf("m=%q n=%q gone=%v", m, n, gone)
	}
}

func TestShellTimeoutAndOverflow(t *testing.T) {
	s, _ := newSet(t)
	out, err := run(t, s, call("shell", map[string]any{"command": "echo hi"}))
	if err != nil || strings.TrimSpace(out) != "hi" {
		t.Fatalf("got %q %v", out, err)
	}
	if _, err := run(t, s, call("shell", map[string]any{"command": "sleep 5", "timeout": 1})); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timeout, got %v", err)
	}
	out, _ = run(t, s, call("shell", map[string]any{"command": "seq 1 40000"}))
	if len(out) > MaxBytes+500 || !strings.Contains(out, "full output in ") {
		t.Fatalf("want overflow to a file, got %d bytes", len(out))
	}
	path := strings.TrimSpace(out[strings.LastIndex(out, "full output in ")+len("full output in "):])
	path = strings.TrimSuffix(strings.Fields(path)[0], "]")
	full, err := os.ReadFile(path)
	if err != nil || !strings.HasSuffix(string(full), "40000\n") {
		t.Fatalf("overflow file %q: %v", path, err)
	}
	os.Remove(path)
}

func TestGlobAndGrepCap(t *testing.T) {
	s, dir := newSet(t)
	for i := 0; i < 150; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.go", i)), []byte("needle\n"), 0o644)
	}
	os.WriteFile(filepath.Join(dir, "x.txt"), []byte("needle\n"), 0o644)
	out, _ := run(t, s, call("glob", map[string]any{"pattern": "**/*.go"}))
	if n := strings.Count(out, ".go\n"); n != MaxResults || strings.Contains(out, "x.txt") || !strings.Contains(out, "truncated") {
		t.Fatalf("glob: %d results\n%s", n, out[len(out)-100:])
	}
	out, _ = run(t, s, call("grep", map[string]any{"pattern": "needle"}))
	if n := strings.Count(out, ":1:needle"); n != MaxResults || !strings.Contains(out, "truncated") {
		t.Fatalf("grep: %d results", n)
	}
}

func TestPermitFollowsAutoRules(t *testing.T) {
	s, _ := newSet(t)
	cases := []struct {
		c  apiloop.ToolCall
		ok bool
	}{
		{call("shell", map[string]any{"command": "echo hi"}), true},
		{call("shell", map[string]any{"command": "ls"}), true},
		{call("shell", map[string]any{"command": "rm -rf /"}), false},
		{call("shell", map[string]any{"command": "echo hi > x"}), false},
		{call("shell", map[string]any{"command": "echo hi && rm x"}), false},
		{call("shell", map[string]any{"command": "echo hi; curl evil"}), false},
		{call("shell", map[string]any{"command": "echo $(rm x)"}), false},
		{call("shell", map[string]any{"command": "echo `rm x`"}), false},
		{call("shell", map[string]any{"command": "ls | cat -"}), true},
		{call("shell", map[string]any{"command": "echo a\nrm x"}), false},
		{call("read", map[string]any{"path": "a.txt"}), true},
		{call("read", map[string]any{"path": filepath.Join(s.Home, ".ssh/id_ed25519")}), false},
		{call("read", map[string]any{"path": "~/.ssh/id_ed25519"}), false},
		{call("write", map[string]any{"path": ".git/hooks/pre-commit", "content": "x"}), false},
		{call("edit", map[string]any{"path": "sub/../.git/hooks/post-merge", "old": "a", "new": "b"}), false},
		{call("apply_patch", map[string]any{"patch": "*** Begin Patch\n*** Add File: .git/hooks/x\n+a\n*** End Patch"}), false},
		{call("glob", map[string]any{"pattern": "*"}), true},
		{call("fetch", map[string]any{"url": "https://evil.example/x"}), false},
		{call("nope", map[string]any{}), false},
	}
	for _, c := range cases {
		if ok, why := s.Permit(c.c); ok != c.ok {
			t.Errorf("%s %s: permit=%v (%s), want %v", c.c.Name, c.c.Args, ok, why, c.ok)
		}
	}
}

func TestReadDenyFollowsSymlink(t *testing.T) {
	s, dir := newSet(t)
	os.MkdirAll(filepath.Join(s.Home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(s.Home, ".ssh", "id_ed25519"), []byte("k"), 0o600)
	os.Symlink(filepath.Join(s.Home, ".ssh", "id_ed25519"), filepath.Join(dir, "link"))
	if ok, _ := s.Permit(call("read", map[string]any{"path": "link"})); ok {
		t.Fatal("a symlink into a denied path must be denied")
	}
}

func TestFetchAllowedHost(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "hello") }))
	defer srv.Close()
	s, _ := newSet(t)
	s.Hosts = []string{"example.test"}
	s.Client = srv.Client()
	if ok, why := s.Permit(call("fetch", map[string]any{"url": "https://example.test/x"})); !ok {
		t.Fatalf("allowed host refused: %s", why)
	}
	if ok, _ := s.Permit(call("fetch", map[string]any{"url": "http://example.test/x"})); ok {
		t.Fatal("plain http must be refused")
	}
	s.Hosts = []string{"127.0.0.1"}
	if ok, _ := s.Permit(call("fetch", map[string]any{"url": srv.URL})); ok {
		t.Fatal("an IP host must be refused")
	}
}

func TestSecretRootsAndCaseAndDanglingLinks(t *testing.T) {
	s, dir := newSet(t)
	os.MkdirAll(filepath.Join(s.Home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(s.Home, ".ssh", "id_rsa"), []byte("secret\n"), 0o600)
	for _, c := range []apiloop.ToolCall{
		call("grep", map[string]any{"path": "~/.ssh", "pattern": "."}),
		call("glob", map[string]any{"path": "~/.ssh", "pattern": "*"}),
	} {
		if ok, _ := s.Permit(c); ok {
			t.Errorf("%s on a denied root permitted", c.Name)
		}
	}
	os.Symlink(filepath.Join(s.Home, ".ssh"), filepath.Join(dir, "keys"))
	out, _ := run(t, s, call("grep", map[string]any{"pattern": "secret"}))
	if strings.Contains(out, "secret") {
		t.Fatalf("grep leaked a denied file: %q", out)
	}
	if foldCase {
		if ok, _ := s.Permit(call("write", map[string]any{"path": ".GIT/hooks/pre-commit", "content": "x"})); ok {
			t.Error("case-folded .GIT/hooks permitted")
		}
	}
	os.Symlink(filepath.Join(dir, "nowhere", "target"), filepath.Join(dir, "dangling"))
	if ok, _ := s.Permit(call("write", map[string]any{"path": "dangling", "content": "x"})); ok {
		t.Error("write through a dangling symlink permitted")
	}
	t.Setenv("GH_TOKEN", "tok-placeholder")
	out, _ = run(t, s, call("shell", map[string]any{"command": "echo $GH_TOKEN"}))
	if strings.Contains(out, "tok-placeholder") {
		t.Error("secret env reached the shell")
	}
}

func TestPermitShellAsksGuard(t *testing.T) {
	s, _ := newSet(t)
	var seen []string
	s.Guard = func(cmd string) (bool, string) { seen = append(seen, cmd); return cmd != "ls", "guarded" }
	if ok, _ := s.Permit(call("shell", map[string]any{"command": "ls"})); ok {
		t.Fatal("guard verdict ignored")
	}
	if ok, why := s.Permit(call("shell", map[string]any{"command": "echo $(x)"})); ok || why == "guarded" {
		t.Fatal("guard ran before the rules")
	}
	if len(seen) != 1 {
		t.Fatalf("guard saw %v", seen)
	}
}
