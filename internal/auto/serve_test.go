package auto

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Zovaris/smithy/internal/config"
)

// fakeSmithy is a stand-in for `smithy agent --bg`: it records its argv and the prompt, then
// prints "<pid> <log>" with the pid from $FAKE_PID.
func fakeSmithy(t *testing.T) (dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	dir = t.TempDir()
	script := filepath.Join(dir, "smithy")
	body := "#!/bin/sh\necho \"$@\" >> " + filepath.Join(dir, "argv") + "\n" +
		"for a; do last=$a; done\ncat \"$last\" >> " + filepath.Join(dir, "prompts") + "\n" +
		"echo \"$FAKE_PID $4/x.log\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	selfExe = func() (string, error) { return script, nil }
	t.Cleanup(func() { selfExe = os.Executable })
	return dir
}

func serveSetup(t *testing.T) (*fakeSource, string) {
	t.Helper()
	home(t)
	src := &fakeSource{name: "fake", issues: []Issue{{ID: "7", Title: "fix it", URL: "https://x/7", Labels: []string{"auto"}}}}
	Sources["fake"] = func(Settings) (Source, error) { return src, nil }
	t.Cleanup(func() { delete(Sources, "fake") })
	if err := config.Set(config.UserFile(), "auto.sources", "fake"); err != nil {
		t.Fatal(err)
	}
	return src, fakeSmithy(t)
}

func newTestServer(t *testing.T, logs *[]string) *Server {
	t.Helper()
	plans := t.TempDir()
	sv, err := NewServer(ServeOptions{Agent: "claude", Once: true}, func(l string) { *logs = append(*logs, l) })
	if err != nil {
		t.Fatal(err)
	}
	sv.mkdr = func(slug string) (string, error) {
		d := filepath.Join(plans, slug)
		return d, os.MkdirAll(d, 0o755)
	}
	return sv
}

func TestServeOnceQueuesTakesAndLaunchesOnce(t *testing.T) {
	_, fake := serveSetup(t)
	t.Setenv("FAKE_PID", strconv.Itoa(os.Getpid()))
	var logs []string
	sv := newTestServer(t, &logs)
	if err := sv.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	argv, _ := os.ReadFile(filepath.Join(fake, "argv"))
	if n := strings.Count(string(argv), "\n"); n != 1 {
		t.Fatalf("launched %d times: %s\nlogs: %q", n, argv, logs)
	}
	for _, want := range []string{"agent --bg --profile auto --timeout 180 ", "--log " + SessionsDir()} {
		if !strings.Contains(string(argv), want) {
			t.Errorf("argv %q lacks %q", argv, want)
		}
	}
	prompt, _ := os.ReadFile(filepath.Join(fake, "prompts"))
	if !strings.Contains(string(prompt), "tend auto once ") || !strings.Contains(string(prompt), "smithy agent") {
		t.Errorf("prompt = %q", prompt)
	}
	if _, err := os.Stat(ServeFile()); !os.IsNotExist(err) {
		t.Errorf("heartbeat left after exit: %v", err)
	}
	if l := ReadLock(); l == nil || l.PID != os.Getpid() {
		t.Errorf("lock = %+v", l)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "auto fake 7 queued") || !strings.Contains(strings.Join(logs, "\n"), "orchestrator started") {
		t.Errorf("logs = %q", logs)
	}

	// A live orchestrator holds the lock: the next issue stays queued.
	src2 := &fakeSource{name: "fake", issues: []Issue{{ID: "8", Title: "next", URL: "https://x/8", Labels: []string{"auto"}}}}
	Sources["fake"] = func(Settings) (Source, error) { return src2, nil }
	logs = nil
	sv = newTestServer(t, &logs)
	if sv.Heartbeat().SessionPID != os.Getpid() {
		t.Errorf("restarted serve forgot the orchestrator: %+v", sv.Heartbeat())
	}
	sv.Cycle()
	argv, _ = os.ReadFile(filepath.Join(fake, "argv"))
	if n := strings.Count(string(argv), "\n"); n != 1 {
		t.Errorf("second orchestrator started while one runs: %s", argv)
	}
	if st, _ := Load(); len(st.Queue) != 1 || st.Queue[0].ID != "8" {
		t.Errorf("queue = %+v", st.Queue)
	}
	if hb, _ := ReadServe(); hb == nil || hb.State != "orchestrating" || hb.Health(time.Now()) != "running" {
		t.Errorf("heartbeat = %+v", hb)
	}

	// Once it ends, the lock clears and the queue launches.
	if err := os.WriteFile(LockFile(), []byte(`{"pid":999999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_PID", "999998")
	sv.Cycle()
	argv, _ = os.ReadFile(filepath.Join(fake, "argv"))
	if n := strings.Count(string(argv), "\n"); n != 2 {
		t.Errorf("queued issue not launched after the lock cleared: %s\nlogs %q", argv, logs)
	}
}

func TestServePausedLaunchesNothing(t *testing.T) {
	_, fake := serveSetup(t)
	config.Set(config.UserFile(), "auto.paused", "true")
	var logs []string
	sv := newTestServer(t, &logs)
	sv.Cycle()
	if _, err := os.Stat(filepath.Join(fake, "argv")); err == nil {
		t.Error("launched while paused")
	}
}

func TestServeStopsOnCancelAndRefusesSecond(t *testing.T) {
	serveSetup(t)
	var logs []string
	sv, err := NewServer(ServeOptions{Agent: "claude"}, func(l string) { logs = append(logs, l) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewServer(ServeOptions{Agent: "claude"}, nil); err != nil {
		t.Errorf("same pid refused: %v", err)
	}
	os.WriteFile(ServeFile(), []byte(`{"pid": 1, "pid_start": "`+procStart(1)+`"}`), 0o600)
	if _, err := NewServer(ServeOptions{Agent: "claude"}, nil); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("second serve: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- sv.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
	if _, err := os.Stat(ServeFile()); !os.IsNotExist(err) {
		t.Error("heartbeat left after stop")
	}
}

func TestHealth(t *testing.T) {
	n := time.Now()
	var none *Heartbeat
	if none.Health(n) != "not running" {
		t.Error("nil heartbeat")
	}
	hb := &Heartbeat{PID: os.Getpid(), Every: 10, LastCycle: n.Add(-20 * time.Second)}
	if hb.Health(n) != "running" {
		t.Error("fresh heartbeat")
	}
	hb.LastCycle = n.Add(-31 * time.Second)
	if hb.Health(n) != "stale" {
		t.Error("old heartbeat")
	}
	hb.LastCycle, hb.PID = n, 999999
	if hb.Health(n) != "stale" {
		t.Error("dead pid")
	}
	if runtime.GOOS != "windows" {
		hb.PID, hb.PIDStart = os.Getpid(), "Mon Jan  1 00:00:00 2001"
		if hb.Health(n) != "stale" {
			t.Error("reused pid read as alive")
		}
		hb.PIDStart = procStart(os.Getpid())
		if hb.Health(n) != "running" {
			t.Error("same process read as dead")
		}
	}
}

func TestServeFailedLaunchRequeues(t *testing.T) {
	serveSetup(t)
	selfExe = func() (string, error) { return "/nonexistent/smithy", nil }
	var logs []string
	sv := newTestServer(t, &logs)
	sv.Cycle()
	if st, _ := Load(); len(st.Queue) != 1 || st.Queue[0].ID != "7" || len(st.Launched) != 0 && len(st.Launched[day(now())]) != 0 {
		t.Errorf("issue not requeued: %+v\nlogs %q", st, logs)
	}
	if ReadLock() != nil {
		t.Error("lock left after a failed launch")
	}
	if !strings.Contains(strings.Join(logs, "\n"), "back in the queue") {
		t.Errorf("logs = %q", logs)
	}
}

// An orchestrator whose agent exits without starting any task puts its issues back, at most
// twice per issue; the third time serve gives up and leaves them launched.
func TestServeRequeuesWhenOrchestratorStartedNothing(t *testing.T) {
	_, fake := serveSetup(t)
	t.Setenv("FAKE_PID", "999998")
	var logs []string
	sv := newTestServer(t, &logs)
	for i := 0; i < 4; i++ {
		sv.Cycle()
	}
	argv, _ := os.ReadFile(filepath.Join(fake, "argv"))
	if n := strings.Count(string(argv), "\n"); n != 3 {
		t.Errorf("launched %d times, want 3\nlogs %q", n, logs)
	}
	all := strings.Join(logs, "\n")
	if strings.Count(all, "requeued 1 issue(s): orchestrator started nothing (log ") != 2 || !strings.Contains(all, "giving up on 7") {
		t.Errorf("logs = %q", logs)
	}
	st, _ := Load()
	if len(st.Queue) != 0 || len(st.Launched[day(now())]) != 1 || st.Requeued["fake:7"] != 2 {
		t.Errorf("state = %+v", st)
	}
	if q := st.Launched[day(now())]; len(q) == 1 {
		if _, err := os.Stat(q[0].Dir); err != nil {
			t.Errorf("given-up task dir removed: %v", err)
		}
	}
}

func TestServeKeepsStartedWork(t *testing.T) {
	serveSetup(t)
	t.Setenv("FAKE_PID", "999998")
	var logs []string
	sv := newTestServer(t, &logs)
	sv.Cycle()
	files, _ := filepath.Glob(filepath.Join(sv.Heartbeat().SessionDir, "01-*.md"))
	if len(files) != 1 {
		t.Fatalf("no task file: %q", logs)
	}
	b, _ := os.ReadFile(files[0])
	if err := os.WriteFile(files[0], []byte(strings.Replace(string(b), "Status: todo", "Status: doing", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	sv.Cycle()
	if strings.Contains(strings.Join(logs, "\n"), "requeued") {
		t.Errorf("started work requeued: %q", logs)
	}
}
