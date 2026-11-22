package decisions

import (
	"io"
	"os"
	"strings"
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
	if !strings.Contains(out, "1. ") || !strings.Contains(out, "Token storage? → httpOnly cookie · why: safest · ref: PR 12") || !strings.Contains(out, "2. ") || !strings.Contains(out, "by timeout") {
		t.Fatalf("pending:\n%s", out)
	}
	capture(t, func() int { return Main([]string{"confirm", "1"}) })
	out, _ = capture(t, func() int { return Main([]string{"pending"}) })
	if strings.Contains(out, "Token") || !strings.Contains(out, "2. ") {
		t.Fatalf("after confirm:\n%s", out)
	}
	if _, c := capture(t, func() int { return Main([]string{"change", "2", "rename to v2"}) }); c != 0 {
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
		{"confirm", "9"},
		{"change", "1"},
		{"nope"},
		{},
	} {
		if _, c := capture(t, func() int { return Main(a) }); c != 2 {
			t.Errorf("%v: exit %d, want 2", a, c)
		}
	}
}
