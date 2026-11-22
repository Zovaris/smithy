//go:build !windows

package agents

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestAdversaryTimeoutKillsGrandchildAndExits124(t *testing.T) {
	d, p, log := advPrompt(t)
	pidf := filepath.Join(d, "gc.pid")
	t.Setenv("AGENT_CMD", "sleep 60 & echo $! > "+pidf+"; wait")
	rc := AgentMain([]string{"--timeout", "0.02", "--log", log, p})
	if rc != 124 {
		t.Fatalf("rc = %d, want 124", rc)
	}
	b, _ := os.ReadFile(pidf)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid == 0 {
		t.Fatalf("no grandchild pid: %q", b)
	}
	time.Sleep(300 * time.Millisecond)
	if alive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("grandchild %d survived the timeout", pid)
	}
}

func TestAdversaryFastExitKeepsCodeAndNoTimeoutNote(t *testing.T) {
	_, p, log := advPrompt(t)
	t.Setenv("AGENT_CMD", "echo hi; exit 3")
	if rc := AgentMain([]string{"--timeout", "5", "--log", log, p}); rc != 3 {
		t.Fatalf("rc = %d, want 3", rc)
	}
	if b, _ := os.ReadFile(log); strings.Contains(string(b), "timed out") {
		t.Fatalf("spurious timeout note: %q", b)
	}
}

func TestAdversaryBgRelativePromptAndCwd(t *testing.T) {
	d, _, _ := advPrompt(t)
	sub := filepath.Join(d, "sub")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(d, "rel.md"), []byte("x"), 0o644)
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(d)
	t.Setenv("SMITHY_TEST_AGENT_MAIN", "1")
	t.Setenv("AGENT_CMD", "sleep 30")
	if rc := AgentMain([]string{"--bg", "--timeout", "0.02", "--cwd", sub, "--log", "rel.log", "rel.md"}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	for i := 0; i < 60; i++ {
		if b, _ := os.ReadFile(filepath.Join(sub, "rel.log")); strings.Contains(string(b), "timed out after") {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	b, _ := os.ReadFile(filepath.Join(sub, "rel.log"))
	t.Fatalf("log = %q", b)
}

func TestAdversaryConfigTimeoutZeroMeansNone(t *testing.T) {
	if ParseTimeout("0") != 0 || ParseTimeout(" 90 ") != 90*time.Minute || ParseTimeout("1e-9") < 0 {
		t.Fatal("ParseTimeout edge")
	}
	if ParseTimeout("NaN") != 90*time.Minute || ParseTimeout("Inf") != 90*time.Minute {
		t.Fatalf("NaN/Inf must fall back to the default: %v %v", ParseTimeout("NaN"), ParseTimeout("Inf"))
	}
}
