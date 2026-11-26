package agents

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type entry struct {
	name, body string
	typ        byte
}

func archive(t *testing.T, es []entry) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, e := range es {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: e.typ}
		if e.typ == 0 {
			h.Typeflag = tar.TypeReg
		}
		if strings.Contains(e.name, "/scripts/") {
			h.Mode = 0o755
		}
		if h.Typeflag == tar.TypeDir {
			h.Mode, h.Size = 0o755, 0
		}
		if h.Typeflag == tar.TypeSymlink {
			h.Linkname, h.Size = e.body, 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	gz.Close()
	return b.Bytes()
}

func release(t *testing.T, tgz []byte, sum string) *httptest.Server {
	t.Helper()
	if sum == "" {
		s := sha256.Sum256(tgz)
		sum = hex.EncodeToString(s[:])
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/" + SkillsArchive:
			w.Write(tgz)
		case "/v1/checksums.txt":
			w.Write([]byte("abc  smithy-linux-amd64.gz\n" + sum + "  " + SkillsArchive + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func goodArchive(t *testing.T) []byte {
	return archive(t, []entry{
		{name: "smelt/", typ: tar.TypeDir},
		{name: "smelt/SKILL.md", body: "new smelt"},
		{name: "smelt/scripts/smithy", body: "#!/bin/sh\n"},
		{name: "hush/SKILL.md", body: "hush"},
		{name: "notours/SKILL.md", body: "x"},
	})
}

func opts(t *testing.T, srv *httptest.Server, home string, hs ...string) SkillsInstall {
	var out bytes.Buffer
	return SkillsInstall{BaseURL: srv.URL + "/v1", Client: srv.Client(), Home: home,
		Getenv: func(string) string { return "" }, Harnesses: hs, Out: &out}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSkillsInstallIntoDetectedHarnesses(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude", "skills", "smelt"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "skills", "smelt", "SKILL.md"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(home, ".claude", "skills", "smelt", "stale.md"), []byte("old"), 0o644)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	srv := release(t, goodArchive(t), "")
	if err := InstallSkills(opts(t, srv, home)); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(home, ".claude", "skills", "smelt", "SKILL.md")); got != "new smelt" {
		t.Fatalf("claude smelt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "smelt", "stale.md")); !os.IsNotExist(err) {
		t.Fatal("old skill files must be replaced, not merged")
	}
	if fi, err := os.Stat(filepath.Join(home, ".claude", "skills", "smelt", "scripts", "smithy")); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("launcher must be executable: %v %v", fi, err)
	}
	if read(t, filepath.Join(home, ".agents", "skills", "hush", "SKILL.md")) != "hush" {
		t.Fatal("codex reads ~/.agents/skills")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "notours")); !os.IsNotExist(err) {
		t.Fatal("only this repo's skills are installed")
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini")); !os.IsNotExist(err) {
		t.Fatal("an undetected harness is left alone")
	}
}

func TestSkillsInstallNamedHarnessOnly(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	srv := release(t, goodArchive(t), "")
	if err := InstallSkills(opts(t, srv, home, "gemini")); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(home, ".gemini", "skills", "hush", "SKILL.md")) != "hush" {
		t.Fatal("named harness gets the skills")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatal("unnamed harness is left alone")
	}
}

func TestSkillsInstallRefusesBadChecksum(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	srv := release(t, goodArchive(t), strings.Repeat("0", 64))
	err := InstallSkills(opts(t, srv, home))
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatal("nothing is written on a bad checksum")
	}
}

func TestSkillsInstallRefusesUnsafeArchive(t *testing.T) {
	for name, es := range map[string][]entry{
		"traversal": {{name: "smelt/../../evil", body: "x"}},
		"absolute":  {{name: "/smelt/SKILL.md", body: "x"}},
		"symlink":   {{name: "smelt/link", body: "/etc/passwd", typ: tar.TypeSymlink}},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
			srv := release(t, archive(t, es), "")
			if err := InstallSkills(opts(t, srv, home)); err == nil {
				t.Fatal("unsafe archive must be refused")
			}
			if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(err) {
				t.Fatal("nothing is written from an unsafe archive")
			}
		})
	}
}

func TestSkillsInstallLeavesSymlinkedSkill(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(home, "checkout", "smelt")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("dev"), 0o644)
	os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0o755)
	os.Symlink(src, filepath.Join(home, ".claude", "skills", "smelt"))
	os.Symlink(src, filepath.Join(home, ".claude", "skills", "hush"))
	srv := release(t, goodArchive(t), "")
	o := opts(t, srv, home)
	if err := InstallSkills(o); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(src, "SKILL.md")) != "dev" {
		t.Fatal("a symlinked skill (a dev checkout) must not be overwritten")
	}
	out := o.Out.(*bytes.Buffer).String()
	if strings.Count(out, "symlink") != 1 || !strings.Contains(out, "smelt") || !strings.Contains(out, "hush") {
		t.Fatalf("skipped symlinked skills are reported in one line naming each: %q", out)
	}
}

func TestSkillsInstallErrors(t *testing.T) {
	home := t.TempDir()
	srv := release(t, goodArchive(t), "")
	if err := InstallSkills(opts(t, srv, home, "nope")); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown harness: %v", err)
	}
	if err := InstallSkills(opts(t, srv, home)); err == nil || !strings.Contains(err.Error(), "--harness") {
		t.Fatalf("no harness detected must say to pass --harness: %v", err)
	}
}

func TestSkillsInstallArgs(t *testing.T) {
	hs, err := parseInstallArgs([]string{"--harness", "codex", "--harness=gemini", "claude"})
	if err != nil || strings.Join(hs, ",") != "codex,gemini,claude" {
		t.Fatalf("%v %v", hs, err)
	}
	if _, err := parseInstallArgs([]string{"--bogus"}); err == nil {
		t.Fatal("unknown flag")
	}
}
