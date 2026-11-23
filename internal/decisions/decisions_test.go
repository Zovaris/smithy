package decisions

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func capture(t *testing.T, f func() int) (string, int) {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	code := f()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b), code
}

func setup(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	old := repoSlug
	repoSlug = func() string { return "demo" }
	t.Cleanup(func() { repoSlug = old })
}

func TestAddPendingConfirmChange(t *testing.T) {
	setup(t)
	if _, c := capture(t, func() int {
		return Main([]string{"add", "Token storage?", "httpOnly cookie", "--why", "safest", "--ref", "PR 12"})
	}); c != 0 {
		t.Fatalf("add exit %d", c)
	}
	capture(t, func() int {
		return Main([]string{"add", "Rename API?", "keep name", "--why", "public API", "--by", "timeout"})
	})
	out, _ := capture(t, func() int { return Main([]string{"pending"}) })
	if !strings.Contains(out, "1. ") || !strings.Contains(out, "Token storage? → httpOnly cookie · why: safest · ref: PR 12") || !strings.Contains(out, "2. ") || !strings.Contains(out, "by timeout · Rename API?") {
		t.Fatalf("pending:\n%s", out)
	}
	capture(t, func() int { return Main([]string{"confirm", "1"}) })
	out, _ = capture(t, func() int { return Main([]string{"pending"}) })
	if strings.Contains(out, "Token") || !strings.Contains(out, "1. ") || !strings.Contains(out, "Rename API?") {
		t.Fatalf("after confirm:\n%s", out)
	}
	if _, c := capture(t, func() int { return Main([]string{"change", "1", "rename to v2"}) }); c != 0 {
		t.Fatalf("change exit %d", c)
	}
	out, _ = capture(t, func() int { return Main([]string{"pending"}) })
	if !strings.Contains(out, "none pending") {
		t.Fatalf("after change:\n%s", out)
	}
	b, _ := os.ReadFile(File())
	if !strings.Contains(string(b), "- [x] ") || !strings.Contains(string(b), "Rename API? → rename to v2 (changed from: keep name) · why: public API") {
		t.Fatalf("file:\n%s", b)
	}
}

func TestConfirmAll(t *testing.T) {
	setup(t)
	for _, q := range []string{"a", "b"} {
		capture(t, func() int { return Main([]string{"add", q, "x", "--why", "y"}) })
	}
	capture(t, func() int { return Main([]string{"confirm", "--all"}) })
	out, _ := capture(t, func() int { return Main([]string{"pending"}) })
	if !strings.Contains(out, "none pending") {
		t.Fatal(out)
	}
}

func TestBadUsage(t *testing.T) {
	setup(t)
	for _, a := range [][]string{
		{"add", "q", "c"},
		{"add", "q", "c", "--why", "w", "--by", "me"},
		{"change", "1"},
		{"nope"},
		{},
	} {
		if _, c := capture(t, func() int { return Main(a) }); c != 2 {
			t.Errorf("%v: exit %d, want 2", a, c)
		}
	}
}

func TestNumbersArePendingPositions(t *testing.T) {
	setup(t)
	for _, q := range []string{"a", "b", "c"} {
		capture(t, func() int { return Main([]string{"add", q, "x", "--why", "y"}) })
	}
	capture(t, func() int { return Main([]string{"confirm", "1"}) })
	capture(t, func() int { return Main([]string{"confirm", "2"}) })
	out, _ := capture(t, func() int { return Main([]string{"pending"}) })
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 || !strings.HasPrefix(lines[0], "1. ") || !strings.Contains(lines[0], "b → x") {
		t.Fatalf("want only b pending as 1:\n%s", out)
	}
	if _, c := capture(t, func() int { return Main([]string{"confirm", "2"}) }); c != 1 {
		t.Fatalf("confirm of a missing pending number: exit %d, want 1", c)
	}
}

func TestSeparatorsRejected(t *testing.T) {
	setup(t)
	for _, a := range [][]string{
		{"add", "a → b?", "x", "--why", "y"},
		{"add", "q", "x · y", "--why", "y"},
		{"add", "q", "x", "--why", "y → z"},
	} {
		if _, c := capture(t, func() int { return Main(a) }); c != 2 {
			t.Errorf("%v: exit %d, want 2", a, c)
		}
	}
	capture(t, func() int { return Main([]string{"add", "q", "x", "--why", "y"}) })
	if _, c := capture(t, func() int { return Main([]string{"change", "1", "a → b"}) }); c != 2 {
		t.Errorf("change with separator: exit %d", c)
	}
}

func TestConcurrentAddAndConfirm(t *testing.T) {
	setup(t)
	devnull, _ := os.Open(os.DevNull)
	old := os.Stdout
	os.Stdout = devnull
	defer func() { os.Stdout = old }()
	Main([]string{"add", "seed", "x", "--why", "y"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) { defer wg.Done(); Main([]string{"add", fmt.Sprintf("q%d", i), "x", "--why", "y"}) }(i)
		go func() { defer wg.Done(); Main([]string{"confirm", "--all"}) }()
	}
	wg.Wait()
	b, _ := os.ReadFile(File())
	if n := strings.Count(string(b), "\n- ["); n+1 != 21 {
		t.Fatalf("want 21 entries, got %d:\n%s", n+1, b)
	}
}

func TestSlug(t *testing.T) {
	for _, c := range [][3]string{
		{"git@github.com:Zovaris/smithy.git", "x", "Zovaris-smithy"},
		{"https://github.com/Zovaris/smithy/", "x", "Zovaris-smithy"},
		{"", "b", "b"},
	} {
		if got := slug(c[0], c[1]); got != c[2] {
			t.Errorf("slug(%q) = %q", c[0], got)
		}
	}
}

func TestAddBySelf(t *testing.T) {
	setup(t)
	if _, c := capture(t, func() int {
		return Main([]string{"add", "Default retries?", "3", "--why", "issue says so", "--by", "self"})
	}); c != 0 {
		t.Fatalf("add --by self exit %d", c)
	}
	if out, _ := capture(t, func() int { return Main([]string{"pending"}) }); !strings.Contains(out, "by self · Default retries?") {
		t.Fatalf("pending:\n%s", out)
	}
}

func TestListRefPrefix(t *testing.T) {
	setup(t)
	for _, a := range [][]string{{"A?", "x", "--ref", "PR 12"}, {"B?", "y", "--ref", "PR 13"}, {"C?", "z"}} {
		capture(t, func() int { return Main(append([]string{"add", a[0], a[1], "--why", "w"}, a[2:]...)) })
	}
	out, c := capture(t, func() int { return Main([]string{"list", "--ref", "PR 1"}) })
	if c != 0 || !strings.Contains(out, "A?") || !strings.Contains(out, "B?") || strings.Contains(out, "C?") {
		t.Errorf("prefix PR 1: %d %q", c, out)
	}
	out, _ = capture(t, func() int { return Main([]string{"list", "--ref", "PR 12"}) })
	if !strings.Contains(out, "A?") || strings.Contains(out, "B?") {
		t.Errorf("prefix PR 12: %q", out)
	}
	out, _ = capture(t, func() int { return Main([]string{"list"}) })
	if !strings.Contains(out, "C?") {
		t.Errorf("unfiltered list: %q", out)
	}
}
