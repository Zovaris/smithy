package docs

import (
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/testkit"
)

func TestDriftBadBase(t *testing.T) {
	t.Chdir(repoWithOddNames(t))
	for _, base := range []string{"nosuch", "-x"} {
		if _, code := capture(t, func() int { return Drift([]string{base}) }); code != 2 {
			t.Errorf("base %q: code %d, want 2", base, code)
		}
	}
}

func TestDriftOddNames(t *testing.T) {
	dir := repoWithOddNames(t)
	testkit.CommitFile(t, dir, "README.md", "see with space.go and ñandú.go\n")
	t.Chdir(dir)
	out, code := capture(t, func() int { return Drift([]string{"HEAD~3"}) })
	if code != 0 || !strings.Contains(out, "- with space.go") || !strings.Contains(out, "- ñandú.go") {
		t.Fatalf("odd names missing (code %d):\n%s", code, out)
	}
}
