package proof

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Zovaris/smithy/internal/testkit"
)

func TestScanCatchesFileRenamedToSecretName(t *testing.T) {
	dir := newRepo(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hello there, a plain readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testkit.Git(t, dir, "add", "README")
	testkit.Git(t, dir, "commit", "-q", "-m", "readme")
	testkit.Git(t, dir, "mv", "README", ".env")
	testkit.Git(t, dir, "commit", "-q", "-m", "rename")
	if c := ScanMain([]string{"--base", "HEAD~1"}); c != 1 {
		t.Fatalf("renamed .env passed: exit %d", c)
	}
}
