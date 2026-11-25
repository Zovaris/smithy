//go:build harness

package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/auto"
)

func TestStateReportsVersion(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	sv.Version = "v9.9.9"
	res := do(t, "GET", hs.URL+"/v1/state", "", auth)
	defer res.Body.Close()
	var st State
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Version != "v9.9.9" {
		t.Fatalf("state version = %q, want v9.9.9", st.Version)
	}
}

func TestIdleCountsOpenStreams(t *testing.T) {
	sv, hs := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	if n := sv.Clients(); n != 0 {
		t.Fatalf("clients before any stream = %d", n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r, _ := http.NewRequestWithContext(ctx, "GET", hs.URL+"/v1/events", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return sv.Clients() == 1 })
	cancel()
	res.Body.Close()
	waitFor(t, func() bool { return sv.Clients() == 0 })
}

func TestWatchIdleStopsOnlyWhenIdle(t *testing.T) {
	sv, _ := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	var busy atomic.Bool
	busy.Store(true)
	stopped := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sv.WatchIdle(ctx, 60*time.Millisecond, 10*time.Millisecond, busy.Load, func() { close(stopped) })
	select {
	case <-stopped:
		t.Fatal("stopped while work was running")
	case <-time.After(200 * time.Millisecond):
	}
	busy.Store(false)
	sv.clients.Add(1)
	select {
	case <-stopped:
		t.Fatal("stopped while a client was connected")
	case <-time.After(200 * time.Millisecond):
	}
	sv.clients.Add(-1)
	sv.touch()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("did not stop after the idle time with no clients and no work")
	}
}

func TestWatchIdleZeroNeverStops(t *testing.T) {
	sv, _ := newTest(t, filepath.Join(t.TempDir(), "s.db"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sv.WatchIdle(ctx, 0, time.Millisecond, func() bool { return false }, func() { t.Fatal("idle_exit 0 stopped the daemon") })
}

func TestParseIdle(t *testing.T) {
	for in, want := range map[string]time.Duration{"30m": 30 * time.Minute, "0": 0, "off": 0, "90s": 90 * time.Second} {
		got, err := ParseIdle(in)
		if err != nil || got != want {
			t.Errorf("ParseIdle(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"soon", "-5m"} {
		if _, err := ParseIdle(bad); err == nil {
			t.Errorf("ParseIdle(%q) accepted", bad)
		}
	}
}

func TestStopEndsTheDaemonProcess(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("no sleep:", err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	pid := cmd.Process.Pid
	waitFor(t, func() bool { return auto.ProcStart(pid) != "" })
	if err := WriteInfo(Info{PID: pid, PIDStart: auto.ProcStart(pid), Addr: "127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Stop(&out, 5*time.Second); code != 0 {
		t.Fatalf("Stop = %d: %s", code, out.String())
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("process still running after Stop")
	}
	if !strings.Contains(out.String(), "stopped") {
		t.Fatalf("Stop output %q does not say it stopped", out.String())
	}
	if i, _ := ReadInfo(); i != nil {
		t.Fatal("daemon.json left behind after stop")
	}
}

func TestStopWithNoDaemon(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	var out bytes.Buffer
	if code := Stop(&out, time.Second); code != 0 {
		t.Fatalf("Stop with nothing running = %d", code)
	}
	if !strings.Contains(out.String(), "no daemon running") {
		t.Fatalf("output %q", out.String())
	}
}

func TestEnsureReusesOrStarts(t *testing.T) {
	live := &Info{PID: 1, Addr: "127.0.0.1:7477"}
	started := 0
	start := func() (*Info, error) { started++; return &Info{PID: 2, Addr: "127.0.0.1:7478"}, nil }
	i, fresh, err := Ensure(func() *Info { return live }, start)
	if err != nil || i != live || fresh || started != 0 {
		t.Fatalf("with a live daemon: %v %v %v, started %d", i, fresh, err, started)
	}
	i, fresh, err = Ensure(func() *Info { return nil }, start)
	if err != nil || i == nil || i.PID != 2 || !fresh || started != 1 {
		t.Fatalf("with none: %v %v %v, started %d", i, fresh, err, started)
	}
	_, _, err = Ensure(func() *Info { return nil }, func() (*Info, error) { return nil, errors.New("boom") })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("start error not returned: %v", err)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached in 3s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
