package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeNeverRealUnderTest(t *testing.T) {
	t.Setenv("SMITHY_HOME", "")
	h := Home()
	d, _ := os.UserHomeDir()
	if real := filepath.Join(d, ".smithy"); h == real {
		t.Fatalf("Home() is the real %s during tests", real)
	}
	if !strings.Contains(h, "smithy-test-home") {
		t.Fatalf("Home() = %s, want the test temp dir", h)
	}
	if Home() != h {
		t.Fatal("Home() changes between calls")
	}
}

func TestHomeRefusesRealUnderTest(t *testing.T) {
	d, _ := os.UserHomeDir()
	t.Setenv("SMITHY_HOME", filepath.Join(d, ".smithy"))
	if Home() == filepath.Join(d, ".smithy") {
		t.Fatal("Home() honours the real ~/.smithy during tests")
	}
}

func TestHome(t *testing.T) {
	t.Run("env", func(t *testing.T) {
		t.Setenv("SMITHY_HOME", "/custom/home")
		if got := Home(); got != "/custom/home" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("default", func(t *testing.T) {
		h := t.TempDir()
		t.Setenv("SMITHY_HOME", "")
		t.Setenv("HOME", h)
		if got, want := Home(), filepath.Join(h, ".smithy"); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
}
