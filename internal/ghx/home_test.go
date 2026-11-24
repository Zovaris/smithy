package ghx

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
