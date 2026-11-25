package uninstall

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct{ home, sh, data, bin, kept, other, rc string }

func setup(t *testing.T) fixture {
	h := t.TempDir()
	f := fixture{home: h, sh: filepath.Join(h, ".smithy"), data: filepath.Join(h, ".local", "share", "smithy"), bin: filepath.Join(h, ".local", "bin")}
	t.Setenv("HOME", h)
	t.Setenv("SMITHY_HOME", f.sh)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_BIN_HOME", "")
	w := func(p, s string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w(filepath.Join(f.data, "versions", "0.21.0", "smithy"), "bin")
	os.MkdirAll(f.bin, 0o755)
	os.Symlink(filepath.Join(f.data, "versions", "0.21.0", "smithy"), filepath.Join(f.bin, "smithy"))
	w(filepath.Join(f.bin, "other-tool"), "keep")
	os.MkdirAll(filepath.Join(f.sh, "bin"), 0o755)
	os.Symlink(filepath.Join(f.bin, "smithy"), filepath.Join(f.sh, "bin", "smithy"))
	mine := filepath.Join(h, ".claude", "skills", "smelt")
	f.other = filepath.Join(h, ".claude", "skills", "my-own")
	f.kept = filepath.Join(h, ".claude", "skills", "tend") // installed by someone else: not in the manifest
	w(filepath.Join(mine, "SKILL.md"), "x")
	w(filepath.Join(f.other, "SKILL.md"), "x")
	w(filepath.Join(f.kept, "SKILL.md"), "x")
	w(filepath.Join(f.sh, "installed-skills"), mine+"\n"+f.other+"\n")
	w(filepath.Join(f.sh, "auth.json"), "{}")
	f.rc = filepath.Join(h, ".zshrc")
	w(f.rc, "alias a=b\n\n# smithy: run `smithy` from any terminal\nexport PATH=\""+f.bin+":$PATH\"\nalias c=d\n")
	w(filepath.Join(f.sh, "path-added"), f.rc+"\n")
	return f
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestNonTTYWithoutYesRemovesNothing(t *testing.T) {
	f := setup(t)
	var out bytes.Buffer
	if c := Run(nil, strings.NewReader(""), false, &out, ""); c != 1 {
		t.Fatalf("exit %d\n%s", c, out.String())
	}
	for _, p := range []string{f.data, filepath.Join(f.bin, "smithy"), filepath.Join(f.sh, "bin"), filepath.Join(f.home, ".claude", "skills", "smelt")} {
		if !exists(p) {
			t.Fatalf("%s removed", p)
		}
		if !strings.Contains(out.String(), p) {
			t.Fatalf("%s not listed:\n%s", p, out.String())
		}
	}
}

func TestYesRemovesOnlyListed(t *testing.T) {
	f := setup(t)
	var out bytes.Buffer
	if c := Run([]string{"--yes"}, strings.NewReader(""), false, &out, ""); c != 0 {
		t.Fatalf("exit %d\n%s", c, out.String())
	}
	for _, p := range []string{f.data, filepath.Join(f.bin, "smithy"), filepath.Join(f.sh, "bin"), filepath.Join(f.home, ".claude", "skills", "smelt")} {
		if exists(p) {
			t.Fatalf("%s kept", p)
		}
	}
	for _, p := range []string{f.kept, f.other, filepath.Join(f.bin, "other-tool"), filepath.Join(f.sh, "auth.json")} {
		if !exists(p) {
			t.Fatalf("%s removed", p)
		}
	}
	b, _ := os.ReadFile(f.rc)
	if string(b) != "alias a=b\n\nalias c=d\n" {
		t.Fatalf("rc: %q", b)
	}
}

func TestPurgeRefusesAFolderThatIsNotSmithys(t *testing.T) {
	f := setup(t)
	os.MkdirAll(filepath.Join(f.sh, "my-project", ".git"), 0o755)
	os.WriteFile(filepath.Join(f.sh, "notes.txt"), []byte("mine"), 0o644)
	var out bytes.Buffer
	Run([]string{"--yes", "--purge", "--confirm-purge"}, strings.NewReader(""), false, &out, "")
	if !exists(filepath.Join(f.sh, "notes.txt")) || !exists(filepath.Join(f.sh, "my-project")) {
		t.Fatalf("purged a folder holding other files:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "not smithy's") || !strings.Contains(out.String(), "notes.txt") {
		t.Fatalf("no reason given:\n%s", out.String())
	}
	g := setup(t)
	os.RemoveAll(g.sh)
	os.MkdirAll(g.sh, 0o755)
	os.WriteFile(filepath.Join(g.sh, "readme.md"), []byte("x"), 0o644)
	out.Reset()
	Run([]string{"--yes", "--purge", "--confirm-purge"}, strings.NewReader(""), false, &out, "")
	if !exists(filepath.Join(g.sh, "readme.md")) {
		t.Fatalf("purged a folder with no smithy files:\n%s", out.String())
	}
	h := setup(t)
	os.WriteFile(filepath.Join(h.sh, ".smithy-home"), nil, 0o644)
	os.WriteFile(filepath.Join(h.sh, "something-new"), []byte("x"), 0o644)
	out.Reset()
	Run([]string{"--yes", "--purge", "--confirm-purge"}, strings.NewReader(""), false, &out, "")
	if exists(h.sh) {
		t.Fatalf("marked smithy home not purged:\n%s", out.String())
	}
}

func TestPurge(t *testing.T) {
	f := setup(t)
	var out bytes.Buffer
	Run([]string{"--yes", "--purge"}, strings.NewReader(""), false, &out, "")
	if !exists(filepath.Join(f.sh, "auth.json")) || !strings.Contains(out.String(), "--confirm-purge") {
		t.Fatalf("purged without confirmation:\n%s", out.String())
	}
	f = setup(t)
	out.Reset()
	Run([]string{"--purge"}, strings.NewReader("y\nno\n"), true, &out, "")
	if !exists(filepath.Join(f.sh, "auth.json")) || exists(f.data) {
		t.Fatalf("wrong answer purged, or nothing removed:\n%s", out.String())
	}
	f = setup(t)
	out.Reset()
	Run([]string{"--purge"}, strings.NewReader("y\npurge\n"), true, &out, "")
	if exists(f.sh) || !strings.Contains(out.String(), "auth.json") {
		t.Fatalf("not purged or contents not named:\n%s", out.String())
	}
}

func TestContainerEngine(t *testing.T) {
	setup(t)
	var calls []string
	orig := Engine
	defer func() { Engine = orig }()
	Engine = func(e string, a ...string) (string, error) {
		calls = append(calls, strings.Join(a, " "))
		if a[0] == "image" && a[1] == "ls" {
			return "smithy-sandbox:1\nubuntu:latest", nil
		}
		return "", nil
	}
	var out bytes.Buffer
	Run([]string{"--yes"}, strings.NewReader(""), false, &out, "docker")
	got := strings.Join(calls, ";")
	for _, w := range []string{"image rm smithy-sandbox:1", "network rm smithy-internal", "network rm smithy-egress"} {
		if !strings.Contains(got, w) {
			t.Fatalf("missing %q in %s", w, got)
		}
	}
	if strings.Contains(got, "ubuntu") {
		t.Fatal("removed a non-smithy image")
	}
}
