package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func home(t *testing.T) string {
	t.Helper()
	h := filepath.Join(t.TempDir(), "home")
	t.Setenv("SMITHY_HOME", h)
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("LINEAR_API_KEY", "")
	t.Chdir(t.TempDir())
	return h
}

func TestSetGetPermsAndList(t *testing.T) {
	h := home(t)
	if err := Set("linear", "lin_api_secretabcd"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(File())
	if err != nil || File() != filepath.Join(h, "auth.json") {
		t.Fatalf("file %s: %v", File(), err)
	}
	if runtime.GOOS != "windows" {
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v", fi.Mode().Perm())
		}
		if di, _ := os.Stat(h); di.Mode().Perm() != 0o700 {
			t.Errorf("dir mode %v", di.Mode().Perm())
		}
	}
	if k, src, err := Get("linear"); err != nil || k != "lin_api_secretabcd" || src != File() {
		t.Errorf("Get = %q %q %v", k, src, err)
	}
	l, _ := List()
	if len(l) != 1 || l[0] != "linear ****abcd" {
		t.Errorf("List = %q", l)
	}
	if ok, err := Remove("linear"); !ok || err != nil {
		t.Errorf("Remove = %v %v", ok, err)
	}
	if k, _, _ := Get("linear"); k != "" {
		t.Errorf("after remove = %q", k)
	}
}

func TestStoredIgnoresEnv(t *testing.T) {
	home(t)
	t.Setenv("CLAUDE_API_KEY", "fromenv")
	if k := Stored("claude"); k != "" {
		t.Errorf("Stored read the env: %q", k)
	}
	Set("claude", "fromfile")
	if k := Stored("claude"); k != "fromfile" {
		t.Errorf("Stored = %q", k)
	}
}

func TestEnvWins(t *testing.T) {
	home(t)
	Set("linear", "fromfile")
	t.Setenv("LINEAR_API_KEY", "fromenv")
	if k, src, _ := Get("linear"); k != "fromenv" || src != "LINEAR_API_KEY" {
		t.Errorf("Get = %q %q", k, src)
	}
}

func TestLooseFileRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	home(t)
	Set("linear", "secret")
	os.Chmod(File(), 0o644)
	k, _, err := Get("linear")
	if k != "" || err == nil || !strings.Contains(err.Error(), "chmod 600") || strings.Contains(err.Error(), "secret") {
		t.Errorf("Get = %q %v", k, err)
	}
}

func TestMissing(t *testing.T) {
	home(t)
	if k, _, err := Get("linear"); k != "" || err != nil {
		t.Errorf("Get = %q %v", k, err)
	}
}

func TestRejectsInnerWhitespaceAndTightensDir(t *testing.T) {
	h := home(t)
	if err := Set("linear", "abc def"); err == nil {
		t.Error("key with a space accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	os.MkdirAll(h, 0o755)
	os.Chmod(h, 0o755)
	if err := Set("linear", "lin_api_key_0000"); err != nil {
		t.Fatal(err)
	}
	if di, _ := os.Stat(h); di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", di.Mode().Perm())
	}
}

func TestMask(t *testing.T) {
	for in, want := range map[string]string{"abcdefgh": "****", "abcdefghijklmnop": "****mnop", "abc": "****", "": "****"} {
		if got := Mask(in); got != want {
			t.Errorf("Mask(%q) = %q", in, got)
		}
	}
}

func TestMainSetReadsStdinOnly(t *testing.T) {
	home(t)
	var out, errb bytes.Buffer
	if code := Main([]string{"set", "linear", "argvkey"}, strings.NewReader("x"), &out, &errb); code != 2 {
		t.Errorf("argv key accepted: %d", code)
	}
	if code := Main([]string{"set", "linear"}, strings.NewReader("  \n"), &out, &errb); code != 2 {
		t.Errorf("empty key accepted: %d", code)
	}
	if code := Main([]string{"set", "linear"}, strings.NewReader("  lin_key_0000wxyz\n"), &out, &errb); code != 0 {
		t.Fatalf("set: %d %s", code, errb.String())
	}
	if out.String() != "saved linear ****wxyz\n" || strings.Contains(errb.String(), "paste") {
		t.Errorf("non-tty set: out %q err %q", out.String(), errb.String())
	}
	if k, _, _ := Get("linear"); k != "lin_key_0000wxyz" {
		t.Errorf("stored %q", k)
	}
	out.Reset()
	Main([]string{"list"}, nil, &out, &errb)
	if out.String() != "linear ****wxyz\n" {
		t.Errorf("list = %q", out.String())
	}
	if strings.Contains(out.String()+errb.String(), "lin_key_0000wxyz") {
		t.Error("full key printed")
	}
	if code := Main([]string{"remove", "linear"}, nil, &out, &errb); code != 0 {
		t.Errorf("remove: %d", code)
	}
}
