package selfupdate

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const script = "#!/bin/sh\necho \"args: $*\"\nexit 7\n"

func serve(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/install.sh":
			fmt.Fprint(w, script)
		case "/repos/Zovaris/smithy/releases/latest":
			if r.Header.Get("Authorization") != "Bearer tok" {
				http.Error(w, "no token", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldI, oldA := InstallURL, APIBase
	InstallURL, APIBase = srv.URL+"/install.sh", srv.URL
	t.Cleanup(func() { InstallURL, APIBase = oldI, oldA })
}

// installed puts a binary at ~/.local/bin/smithy in a temp home and returns its path.
func installed(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	bin := filepath.Join(home, ".local", "bin", "smithy")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return bin
}

func run(t *testing.T, o Options, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	o.Stdout, o.Stderr = &out, &errOut
	code := Run(args, o)
	return out.String(), errOut.String(), code
}

func TestUpdatesBinaryAndSkills(t *testing.T) {
	serve(t)
	bin := installed(t)
	out, _, code := run(t, Options{Version: "1.0.0", Exe: bin})
	if code != 7 || !strings.Contains(out, "args: --update\n") {
		t.Errorf("code %d out %q", code, out)
	}
}

func TestPinnedVersion(t *testing.T) {
	serve(t)
	bin := installed(t)
	out, _, _ := run(t, Options{Version: "1.0.0", Exe: bin}, "v0.20.0")
	if !strings.Contains(out, "args: --update 0.20.0\n") {
		t.Errorf("out %q", out)
	}
}

func TestCheck(t *testing.T) {
	serve(t)
	t.Setenv("GH_TOKEN", "tok")
	out, errOut, code := run(t, Options{Version: "1.0.0", Exe: "/nowhere/smithy"}, "--check")
	if code != 0 || out != "current v1.0.0\nlatest  v9.9.9\n" {
		t.Errorf("code %d out %q err %q", code, out, errOut)
	}
}

func TestRefusesNonInstalledBinary(t *testing.T) {
	serve(t)
	installed(t)
	other := filepath.Join(t.TempDir(), "smithy")
	_, errOut, code := run(t, Options{Version: "dev", Exe: other})
	if code != 2 || !strings.Contains(errOut, "--force") {
		t.Errorf("code %d err %q", code, errOut)
	}
	out, _, code := run(t, Options{Version: "dev", Exe: other}, "--force")
	if code != 7 || !strings.Contains(out, "args: --update\n") {
		t.Errorf("force: code %d out %q", code, out)
	}
}

func TestBadArgs(t *testing.T) {
	if _, _, code := run(t, Options{}, "--nope"); code != 2 {
		t.Errorf("code %d", code)
	}
	if _, _, code := run(t, Options{}, "1.0.0", "2.0.0"); code != 2 {
		t.Errorf("code %d", code)
	}
}
