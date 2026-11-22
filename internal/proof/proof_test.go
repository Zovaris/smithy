package proof

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "init.defaultBranch=main"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func TestRunRecordsExitDigestAndCommit(t *testing.T) {
	dir := newRepo(t)
	var out bytes.Buffer
	r, err := Run(dir, "suite passes", []string{"sh", "-c", "echo hi; exit 3"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if r.Exit != 3 || out.String() != "hi\n" || r.Claim != "suite passes" || r.Dirty || len(r.Digest) != 16 || len(r.Commit) != 40 {
		t.Fatalf("bad receipt %+v out=%q", r, out.String())
	}
	r2, _ := Run(dir, "", []string{"sh", "-c", "echo hi; exit 3"}, &bytes.Buffer{})
	if r2.Digest != r.Digest || r2.Claim != "sh -c echo hi; exit 3" {
		t.Fatalf("digest not stable or default claim wrong: %+v", r2)
	}
}

func TestRunMarksDirtyTreeAndStale(t *testing.T) {
	dir := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := Run(dir, "x", []string{"true"}, &bytes.Buffer{})
	if !r.Dirty || r.State(r.Commit, false) != "dirty" {
		t.Fatalf("dirty tree should give a stale receipt: %+v", r)
	}
}

func TestAppendLoadMarkdown(t *testing.T) {
	dir := newRepo(t)
	a := Receipt{Claim: "a|b", Command: []string{"go", "test"}, Exit: 0, Commit: "1111111111", Digest: "d"}
	b := Receipt{Claim: "lint", Command: []string{"lint"}, Exit: 1, Commit: "2222222222", Digest: "e"}
	for _, r := range []Receipt{a, b} {
		if err := Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	rs, err := Load(dir)
	if err != nil || len(rs) != 2 {
		t.Fatalf("load: %v %v", rs, err)
	}
	md := Markdown(rs, "1111111111", false)
	if !strings.Contains(md, `| a\|b | `+"`go test`"+` | 0 | 1111111 |`) || !strings.Contains(md, "2222222 (stale)") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	rs, err := Load(t.TempDir())
	if err != nil || rs != nil {
		t.Fatalf("got %v %v", rs, err)
	}
}

func TestScanDiffFindsSecretsOnAddedLinesOnly(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/app/config.go b/app/config.go",
		"+++ b/app/config.go",
		"@@ -1,0 +10,4 @@",
		`+key := "AKIAABCDEFGHIJKLMNOP"`, // smithy:allow (fixture)
		`+password = "hunter2hunter2"`,   // smithy:allow (fixture)
		`+password = os.Getenv("PASSWORD")`,
		`+token := "ghp_` + strings.Repeat("a", 36) + `" // smithy:allow`,
		"-old := \"AKIAABCDEFGHIJKLMNOP\"", // smithy:allow (fixture)
		"diff --git a/README.md b/README.md",
		"+++ b/README.md",
		"@@ -5 +7 @@",
		"+postgres://admin:supersecret@db:5432/x", // smithy:allow (fixture)
	}, "\n")
	fs := ScanDiff(diff)
	var got []string
	for _, f := range fs {
		got = append(got, f.String())
	}
	want := []string{
		"app/config.go:10\taws access key\tAKIA************MNOP",
		"app/config.go:11\thardcoded secret\tpass*******************er2\"",
		"README.md:7\tcredentials in url\tpost*********************ret@",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestScanFiles(t *testing.T) {
	fs := ScanFiles([]string{".env", "deploy/.env.production", ".env.example", "certs/server.pem", "src/key.go", "id_ed25519", ""})
	var got []string
	for _, f := range fs {
		got = append(got, f.File)
	}
	if strings.Join(got, ",") != ".env,deploy/.env.production,certs/server.pem,id_ed25519" {
		t.Fatalf("got %v", got)
	}
}

func TestMask(t *testing.T) {
	if mask("abc") != "***" || mask("abcdefghij") != "abcd**ghij" {
		t.Fatal(mask("abcdefghij"))
	}
}

func TestMainsEndToEnd(t *testing.T) {
	dir := newRepo(t)
	t.Chdir(dir)
	if c := ReceiptMain([]string{"run", "--claim", "ok", "--", "true"}); c != 0 {
		t.Fatalf("run exit %d", c)
	}
	if c := ReceiptMain([]string{"run", "--", "false"}); c != 1 {
		t.Fatalf("run should pass the exit code through, got %d", c)
	}
	for _, a := range [][]string{{"list"}, {"list", "--md", "--all"}} {
		if c := ReceiptMain(a); c != 0 {
			t.Fatalf("%v exit %d", a, c)
		}
	}
	if c := ReceiptMain([]string{"list", "--bogus"}); c != 2 {
		t.Fatalf("bad flag exit %d", c)
	}
	if c := ReceiptMain([]string{"run", "--"}); c != 2 {
		t.Fatalf("no command exit %d", c)
	}
	if c := ReceiptMain([]string{"clear"}); c != 0 {
		t.Fatalf("clear exit %d", c)
	}
	if rs, _ := Load(dir); len(rs) != 0 {
		t.Fatalf("clear left %v", rs)
	}
	if c := ScanMain(nil); c != 0 {
		t.Fatalf("clean scan exit %d", c)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("-----BEGIN RSA PRIVATE KEY-----\n"), 0o644); err != nil { // smithy:allow
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("X=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := ScanMain([]string{"--base", "HEAD"}); c != 1 {
		t.Fatalf("dirty scan exit %d", c)
	}
	if c := ScanMain([]string{"--nope"}); c != 2 {
		t.Fatalf("bad flag exit %d", c)
	}
}

func TestReceiptStateAndQuote(t *testing.T) {
	r := Receipt{Commit: "abc", Command: []string{"sh", "-c", "bun run test"}}
	if r.State("abc", false) != "fresh" || r.State("def", false) != "stale" {
		t.Fatal("fresh/stale")
	}
	r.Dirty = true
	if r.State("abc", false) != "dirty" || r.State("def", false) != "stale" {
		t.Fatal("dirty")
	}
	if got := Quote(r.Command); got != "sh -c 'bun run test'" {
		t.Fatalf("quote: %q", got)
	}
	if got := Quote([]string{"echo", "it's", ""}); got != `echo 'it'\''s' ''` {
		t.Fatalf("quote: %q", got)
	}
	if md := Markdown([]Receipt{r}, "abc", false); !strings.Contains(md, "abc (dirty)") || !strings.Contains(md, "`sh -c 'bun run test'`") {
		t.Fatalf("markdown: %s", md)
	}
}

func TestAppendIgnoresItsOwnFolder(t *testing.T) {
	dir := newRepo(t)
	if err := Append(dir, Receipt{Claim: "x"}); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil || strings.TrimSpace(string(out)) != "" {
		t.Fatalf("receipts show up in git status: %q %v", out, err)
	}
}

func TestRedactKeepsTokensOutOfPRBodies(t *testing.T) {
	tok := "ghp_" + strings.Repeat("a", 36)
	md := Markdown([]Receipt{{Commit: "c", Command: []string{"curl", "-H", "Authorization: token " + tok}}}, "c", false)
	if strings.Contains(md, tok) {
		t.Fatalf("token printed: %s", md)
	}
}

func TestScanRejectsOptionBaseAndCoversHistoryWithoutOrigin(t *testing.T) {
	dir := newRepo(t)
	t.Chdir(dir)
	if c := ScanMain([]string{"--base", "--output=x"}); c != 2 {
		t.Fatalf("option base exit %d", c)
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Fatal("git wrote the --output file")
	}
	if err := os.WriteFile(filepath.Join(dir, "k.txt"), []byte("-----BEGIN RSA PRIVATE KEY-----\n"), 0o644); err != nil { // smithy:allow
		t.Fatal(err)
	}
	git(t, dir, "add", "k.txt")
	git(t, dir, "commit", "-q", "-m", "key")
	if c := ScanMain(nil); c != 1 {
		t.Fatalf("committed key without origin passed: exit %d", c)
	}
}

func TestListSaysHowManyItHid(t *testing.T) {
	dir := newRepo(t)
	t.Chdir(dir)
	if err := Append(dir, Receipt{Claim: "old", Commit: "0000000"}); err != nil {
		t.Fatal(err)
	}
	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	c := ReceiptMain([]string{"list"})
	os.Stderr = old
	w.Close()
	b, _ := io.ReadAll(r)
	if c != 0 || !strings.Contains(string(b), "1 receipt(s) from other commits hidden") {
		t.Fatalf("exit %d stderr %q", c, b)
	}
}

func TestHostileFindings(t *testing.T) {
	key := "AKIA" + "Z7QW3RTY5UIO2PLK"
	diff := "diff --git a/d.txt b/d.txt\nnew file mode 100644\n--- /dev/null\n+++ b/d.txt\n@@ -0,0 +1,3 @@\n+a\n+++ b/x\n+k=" + key + "\n"
	fs := ScanDiff(diff)
	if len(fs) != 1 || fs[0].File != "d.txt" || fs[0].Line != 3 {
		t.Fatalf("a '++' line hid or moved the finding: %v", fs)
	}

	dir := newRepo(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "s.txt"), []byte("k = \""+key+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := ScanMain(nil); c != 1 {
		t.Fatalf("untracked file content not scanned: exit %d", c)
	}
	os.Remove(filepath.Join(dir, "s.txt"))
	if err := os.WriteFile(filepath.Join(dir, "we ird é.pem"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fs := ScanFiles([]string{"we ird é.pem"}); len(fs) != 1 {
		t.Fatalf("non-ascii .pem not flagged: %v", fs)
	}
	if c := ScanMain(nil); c != 1 {
		t.Fatalf("non-ascii .pem passed: exit %d", c)
	}
	os.Remove(filepath.Join(dir, "we ird é.pem"))

	if err := Append(dir, Receipt{Claim: "a"}); err != nil {
		t.Fatal(err)
	}
	f, _ := os.OpenFile(receiptsPath(dir), os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"claim":"cut`)
	f.Close()
	if err := Append(dir, Receipt{Claim: "after"}); err != nil {
		t.Fatal(err)
	}
	rs, _ := Load(dir)
	if len(rs) != 2 || rs[1].Claim != "after" {
		t.Fatalf("receipt after a cut line lost: %+v", rs)
	}

	r := Receipt{Commit: "abc"}
	if r.State("abc", true) != "dirty" {
		t.Fatal("tree edited after the run should read dirty")
	}

	os.RemoveAll(filepath.Join(dir, ".smelt"))
	os.WriteFile(filepath.Join(dir, ".smelt"), nil, 0o644)
	if c := ReceiptMain([]string{"run", "--", "true"}); c != 1 {
		t.Fatalf("unrecorded success must fail: %d", c)
	}
}

func TestRedactMasksSecretFlagsAndBearer(t *testing.T) {
	got := Redact(Quote([]string{"tool", "--token", "sup3rs3cretvalue123", "--api-key=abcdefghij12", "Authorization: Bearer abcdef1234567890xyz"}))
	for _, leak := range []string{"sup3rs3cretvalue123", "abcdefghij12", "abcdef1234567890xyz"} {
		if strings.Contains(got, leak) {
			t.Fatalf("leaked %q in %q", leak, got)
		}
	}
}

func TestUntrackedFileMakesReceiptDirtyButIgnoredAndSmeltDoNot(t *testing.T) {
	dir := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".gitignore")
	git(t, dir, "commit", "-q", "-m", "ignore")
	os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".smelt"), 0o755)
	os.WriteFile(filepath.Join(dir, ".smelt", "notes.md"), []byte("x\n"), 0o644)
	if r, _ := Run(dir, "x", []string{"true"}, &bytes.Buffer{}); r.Dirty {
		t.Fatalf("ignored files and .smelt/ must not make a receipt dirty: %+v", r)
	}
	os.WriteFile(filepath.Join(dir, "new.go"), []byte("package x\n"), 0o644)
	if r, _ := Run(dir, "x", []string{"true"}, &bytes.Buffer{}); !r.Dirty {
		t.Fatalf("an untracked file must make the receipt dirty: %+v", r)
	}
}

func TestScanBinariesFlagsBinaryAndRiskyExtensions(t *testing.T) {
	fs := ScanBinaries([]string{"a.txt", "x.PYC", "lib/y.so", "doc.docx", "ok.go", "blob.bin", ""}, map[string]bool{"blob.bin": true})
	got := []string{}
	for _, f := range fs {
		got = append(got, f.File+"|"+f.Rule)
	}
	want := "x.PYC|needs a human look: binary or archive file,lib/y.so|needs a human look: binary or archive file,doc.docx|needs a human look: binary or archive file,blob.bin|needs a human look: binary or archive file"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v", got)
	}
}

func TestNumstatBinaries(t *testing.T) {
	got := numstatBinaries("1\t2\ta.txt\x00-\t-\tb.bin\x00-\t-\t\x00old.bin\x00new.bin\x00")
	if !got["b.bin"] || !got["new.bin"] || got["a.txt"] || got["old.bin"] {
		t.Fatalf("%v", got)
	}
}

func TestScanFailsOnConflictMarkersOutsideDocFences(t *testing.T) {
	diff := "diff --git a/CHANGELOG.md b/CHANGELOG.md\n+++ b/CHANGELOG.md\n@@ -0,0 +1,8 @@\n" +
		"+<<<<<<< HEAD\n+text\n+=======\n+other\n+>>>>>>> origin/x\n" +
		"+```\n+<<<<<<< shown in docs\n+```\n"
	fs := ScanDiff(diff)
	if len(fs) != 3 {
		t.Fatalf("want 3 markers outside the fence, got %v", fs)
	}
	for _, f := range fs {
		if f.Rule != "conflict marker" {
			t.Fatalf("%v", f)
		}
	}
	code := "diff --git a/a.go b/a.go\n+++ b/a.go\n@@ -0,0 +1,2 @@\n+||||||| base\n+// ======= not alone\n"
	if fs := ScanDiff(code); len(fs) != 1 {
		t.Fatalf("code files have no fences: %v", fs)
	}
	if fs := ScanDiff("diff --git a/a.go b/a.go\n+++ b/a.go\n@@ -0,0 +2 @@\n+<<<<<<< HEAD\r\n+=======\r\n"); len(fs) != 2 {
		t.Fatalf("a CRLF marker must count: %v", fs)
	}
	if fs := ScanDiff("diff --git a/a.md b/a.md\n+++ b/a.md\n@@ -0,0 +1 @@\n+a ======= b\n"); len(fs) != 0 {
		t.Fatalf("%v", fs)
	}
}

func TestScanMainReportsBinaryFile(t *testing.T) {
	dir := newRepo(t)
	os.WriteFile(filepath.Join(dir, "p.pyc"), []byte("text\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "z.dat"), []byte{0, 1, 2}, 0o644)
	git(t, dir, "add", "p.pyc", "z.dat")
	git(t, dir, "commit", "-q", "-m", "bin")
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(dir)
	if code := ScanMain([]string{"--base", "HEAD~1"}); code != 0 {
		t.Fatalf("binaries are warnings for a human look, not failures; exit %d", code)
	}
}

func TestBareSeparatorNeedsAConflictStart(t *testing.T) {
	banner := "diff --git a/x.txt b/x.txt\n+++ b/x.txt\n@@ -0,0 +1,2 @@\n+=======\n+title\n"
	if fs, _ := scanDiff(banner); len(fs) != 0 {
		t.Fatalf("a bare ======= with no <<<<<<< is not a conflict: %v", fs)
	}
	conflict := "diff --git a/x.txt b/x.txt\n+++ b/x.txt\n@@ -0,0 +1,3 @@\n+<<<<<<< HEAD\n+=======\n+>>>>>>> other\n"
	if fs, _ := scanDiff(conflict); len(fs) != 3 {
		t.Fatalf("all three markers of a real conflict: %v", fs)
	}
}

func TestRunSplitsOneQuotedCommandLine(t *testing.T) {
	dir := newRepo(t)
	var out bytes.Buffer
	r, err := Run(dir, "", []string{"echo one two"}, &out)
	if err != nil || r.Exit != 0 || out.String() != "one two\n" {
		t.Fatalf("err=%v r=%+v out=%q", err, r, out.String())
	}
}

func TestMarkdownEditsNeitherDirtyNorStale(t *testing.T) {
	dir := newRepo(t)
	os.MkdirAll(filepath.Join(dir, "docs", "specs"), 0o755)
	md := filepath.Join(dir, "docs", "specs", "01-task.md")
	if err := os.WriteFile(md, []byte("Status: doing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := Run(dir, "x", []string{"true"}, &bytes.Buffer{})
	if r.Dirty {
		t.Fatalf("a Markdown edit made the receipt dirty: %+v", r)
	}
	git(t, dir, "add", "docs/specs/01-task.md")
	git(t, dir, "commit", "-q", "-m", "done")
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	if !docsOnlySince(dir, r.Commit, head) {
		t.Fatal("docs-only commit should keep the receipt current")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "commit", "-qam", "code")
	head = strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	if docsOnlySince(dir, r.Commit, head) {
		t.Fatal("a code commit must make the receipt stale")
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
