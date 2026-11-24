// Package ghx runs git and the GitHub CLI and holds the paths every smithy command shares.
package ghx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Home is the smithy state dir: $SMITHY_HOME or ~/.smithy.
// Under `go test` it never returns the user's real ~/.smithy: a test that set no temp home gets a
// per-process temp dir (exported so child processes share it), so tests cannot leak events or alerts.
func Home() string {
	home := os.Getenv("SMITHY_HOME")
	if home == "" {
		d, _ := os.UserHomeDir()
		home = filepath.Join(d, ".smithy")
	}
	if testing.Testing() && filepath.Clean(home) == startHome {
		testHomeOnce.Do(func() {
			testHome, _ = os.MkdirTemp("", "smithy-test-home-")
		})
		os.Setenv("SMITHY_HOME", testHome)
		return testHome
	}
	return home
}

var (
	testHomeOnce sync.Once
	testHome     string
)

// In a test binary SMITHY_HOME points at a temp dir from the start, so the smithy binaries tests
// run as child processes inherit it too.
func init() {
	if testing.Testing() {
		Home()
	}
}

// startHome is the user's real ~/.smithy, taken before any test can change $HOME.
var startHome = func() string {
	d, _ := os.UserHomeDir()
	return filepath.Join(d, ".smithy")
}()

// SkillDir is the skill's scripts folder the launcher ran from (empty when run directly).
func SkillDir() string { return os.Getenv("SMITHY_SKILL_DIR") }

// RepoRoot is the top of the git checkout at dir, or "" outside one.
func RepoRoot(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Run runs a command and returns trimmed stdout; the error carries stderr's first line.
func Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// GH runs gh and decodes its JSON output into v.
func GH(v any, args ...string) error {
	out, err := Run("gh", args...)
	if err != nil {
		return err
	}
	if out == "" {
		out = "null"
	}
	return json.Unmarshal([]byte(out), v)
}
