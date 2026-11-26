package layout

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// UserHome is $HOME, else the OS user home.
func UserHome() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

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

// WardenHome is $WARDEN_HOME, else <smithy home>/warden.
func WardenHome() string {
	if h := os.Getenv("WARDEN_HOME"); h != "" {
		return h
	}
	return filepath.Join(Home(), "warden")
}

// PlansHome is $SMELT_PLANS, else <smithy home>/plans.
func PlansHome() string {
	if p := os.Getenv("SMELT_PLANS"); p != "" {
		return p
	}
	return filepath.Join(Home(), "plans")
}

// Tilde shortens a path under the user home to ~/….
func Tilde(p string) string {
	h := UserHome()
	if h != "" && len(p) > len(h) && p[:len(h)+1] == h+"/" {
		return "~" + p[len(h):]
	}
	return p
}
