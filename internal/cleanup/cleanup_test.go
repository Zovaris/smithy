package cleanup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mkbin(t *testing.T, dir, ver string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ver), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ver, "smithy"), make([]byte, size), 0o755); err != nil {
		t.Fatal(err)
	}
}

func paths(items []Item) []string {
	var out []string
	for _, i := range items {
		out = append(out, filepath.Base(i.Target))
	}
	return out
}

func TestBuildsKeepsCurrentAndOnePrevious(t *testing.T) {
	bin := t.TempDir()
	for _, v := range []string{"0.9.0", "0.17.0", "0.18.0", "0.18.1", "0.19.0-rc1", "dev/old", "dev/new", "tools"} {
		mkbin(t, bin, v, 10)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(filepath.Join(bin, "dev", "old", "smithy"), old, old)
	os.Chtimes(filepath.Join(bin, "dev", "old"), old, old)
	if err := os.WriteFile(filepath.Join(bin, "smithy-linux-arm64"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(bin, "0.18.0", "smithy"), filepath.Join(bin, "smithy")); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(paths(Builds(bin)), ",")
	if got != "0.9.0,0.17.0,0.18.1,old" {
		t.Fatalf("removable builds = %q, want 0.9.0,0.17.0,0.18.1,old (keep current 0.18.0, newest other 0.19.0-rc1, recent dev builds, unknown folders)", got)
	}
	for _, i := range Builds(bin) {
		if i.Bytes != 10 {
			t.Fatalf("%s size %d, want 10", i.Target, i.Bytes)
		}
	}
}

func TestBuildsNoSymlinkKeepsNewestTwo(t *testing.T) {
	bin := t.TempDir()
	for _, v := range []string{"0.1.0", "0.2.0", "0.10.0"} {
		mkbin(t, bin, v, 1)
	}
	if got := strings.Join(paths(Builds(bin)), ","); got != "0.1.0" {
		t.Fatalf("got %q, want 0.1.0", got)
	}
	if got := Builds(filepath.Join(bin, "missing")); len(got) != 0 {
		t.Fatalf("missing bin dir: %v", got)
	}
}

func TestImagesStaleOnlySmithyNotConfigured(t *testing.T) {
	ls := "smithy-sandbox\tlatest\taaa\t1.2GB\nsmithy-sandbox\talias\taaa\t1.2GB\nsmithy-sandbox\told\tbbb\t900MB\nsmithy-sandbox\told2\tbbb\t900MB\nubuntu\tlatest\tccc\t70MB\n<none>\t<none>\tddd\t5MB\nsmithy-sandbox-gemini\tv1\teee\t1GB\n"
	got := Images(ls, "smithy-sandbox:latest")
	var ids []string
	for _, i := range got {
		ids = append(ids, i.Target+"="+i.Size)
	}
	if s := strings.Join(ids, ","); s != "smithy-sandbox:old=900MB,smithy-sandbox:old2=900MB,smithy-sandbox-gemini:v1=1GB" {
		t.Fatalf("stale images = %q", s)
	}
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreesStaleOnlyCleanAndLanded(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	os.MkdirAll(repo, 0o755)
	run(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a"), []byte("a"), 0o644)
	run(t, repo, "add", "a")
	run(t, repo, "commit", "-qm", "a")
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".env\n"), 0o644)
	run(t, repo, "add", ".gitignore")
	run(t, repo, "commit", "-qm", "ignore")
	for _, b := range []string{"merged", "dirty", "ahead", "here", "ignored"} {
		run(t, repo, "worktree", "add", "-q", "-b", b, filepath.Join(root, b))
	}
	os.MkdirAll(filepath.Join(root, "here", "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "dirty", "x"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "ignored", ".env"), []byte("k"), 0o644)
	os.WriteFile(filepath.Join(root, "ahead", "b"), []byte("b"), 0o644)
	run(t, filepath.Join(root, "ahead"), "add", "b")
	run(t, filepath.Join(root, "ahead"), "commit", "-qm", "b")
	got := Worktrees(repo, "main", filepath.Join(root, "here", "sub"))
	if s := strings.Join(paths(got), ","); s != "merged" {
		t.Fatalf("stale worktrees = %q, want merged (dirty has changes, ahead has work, here is the cwd)", s)
	}
}

func TestMainDryRunDeletesNothingYesDeletes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMITHY_HOME", home)
	bin := filepath.Join(home, "bin")
	for _, v := range []string{"0.1.0", "0.2.0", "0.3.0"} {
		mkbin(t, bin, v, 2048)
	}
	engine = func(args ...string) (string, error) { return "", os.ErrNotExist }
	t.Chdir(t.TempDir())
	if code := Main(nil); code != 0 {
		t.Fatalf("dry run exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(bin, "0.1.0")); err != nil {
		t.Fatal("dry run deleted 0.1.0")
	}
	if code := Main([]string{"--yes"}); code != 0 {
		t.Fatalf("--yes exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(bin, "0.1.0")); !os.IsNotExist(err) {
		t.Fatal("--yes kept 0.1.0")
	}
	if _, err := os.Stat(filepath.Join(bin, "0.3.0")); err != nil {
		t.Fatal("--yes removed the newest build")
	}
	if code := Main([]string{"--bogus"}); code != 2 {
		t.Fatalf("bad flag exit %d", code)
	}
}

func TestBuildsDevAgeFromBinaryNotFolder(t *testing.T) {
	bin := t.TempDir()
	old := time.Now().Add(-30 * 24 * time.Hour)
	for _, k := range []string{"stale", "inuse"} {
		d := filepath.Join(bin, "dev", k)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(d, "smithy")
		if err := os.WriteFile(f, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		if k == "stale" {
			_ = os.Chtimes(f, old, old)
		}
		_ = os.Chtimes(d, old, old)
	}
	got := strings.Join(paths(Builds(bin)), ",")
	if !strings.Contains(got, "stale") || strings.Contains(got, "inuse") {
		t.Fatalf("got %q, want only the stale dev build", got)
	}
}

func TestBuildsInStandardLayout(t *testing.T) {
	data := t.TempDir()
	vers, dev, link := filepath.Join(data, "versions"), filepath.Join(data, "dev"), filepath.Join(t.TempDir(), "smithy")
	for _, v := range []string{"0.19.0", "0.20.0", "0.21.0"} {
		mkbin(t, vers, v, 1)
	}
	mkbin(t, dev, "old", 1)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(filepath.Join(dev, "old", "smithy"), old, old)
	os.Chtimes(filepath.Join(dev, "old"), old, old)
	if err := os.Symlink(filepath.Join(vers, "0.19.0", "smithy"), link); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(BuildsIn(vers, dev, link)), ","); got != "0.20.0,old" {
		t.Fatalf("got %q, want 0.20.0,old (keep the linked 0.19.0 and newest 0.21.0)", got)
	}
}
