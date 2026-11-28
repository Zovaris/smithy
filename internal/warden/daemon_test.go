package warden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/api"
	"github.com/Zovaris/smithy/internal/daemon"
)

func TestDaemonDoctor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMITHY_HOME", home)
	run := func() (string, bool) {
		r := &report{}
		o, _, _ := capture(t, func() int { daemonDoctor(r); return 0 })
		return o, r.failed
	}
	if o, failed := run(); o != "" || failed {
		t.Fatalf("no daemon ever: %q", o)
	}
	daemon.WriteInfo(api.Info{PID: os.Getpid(), Addr: "127.0.0.1:7477", Started: time.Now()})
	os.WriteFile(filepath.Join(home, "daemon.token"), []byte("x\n"), 0o600)
	if o, failed := run(); failed || !strings.Contains(o, "ok    daemon: running") {
		t.Fatalf("running: %q", o)
	}
	daemon.WriteInfo(api.Info{PID: 999999, Addr: "127.0.0.1:7477"})
	if o, _ := run(); !strings.Contains(o, "warn  daemon: stale") {
		t.Fatalf("stale: %q", o)
	}
	daemon.WriteInfo(api.Info{PID: os.Getpid(), Addr: "0.0.0.0:7477"})
	if o, failed := run(); !failed || !strings.Contains(o, "beyond loopback") {
		t.Fatalf("non-loopback: %q", o)
	}
	os.Remove(daemon.InfoFile())
	os.Chmod(filepath.Join(home, "daemon.token"), 0o644)
	if o, failed := run(); !failed || !strings.Contains(o, "readable by others") {
		t.Fatalf("token mode: %q", o)
	}
}
