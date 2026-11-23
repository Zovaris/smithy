package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAutoStatusAndTakeEmpty(t *testing.T) {
	repo, env := sandbox(t)
	out, errOut, code := run(t, repo, env, "auto", "take")
	if code != 0 || strings.TrimSpace(out) != "auto none: empty queue" {
		t.Errorf("take: %d %q %q", code, out, errOut)
	}
	out, errOut, code = run(t, repo, env, "auto", "status", "--json")
	var st struct {
		Alive bool
		Cap   int
		Queue []any
	}
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || st.Alive || st.Cap != 20 || st.Queue == nil {
		t.Errorf("status: %d %q %q", code, out, errOut)
	}
	if _, _, code = run(t, repo, env, "auto", "report", "github:1", "exploded"); code == 0 {
		t.Error("bad state accepted")
	}
	out, _, _ = run(t, repo, env, "config", "list")
	for _, k := range []string{"auto.sources", "auto.max_per_day", "auto.hours", "linear.key_env", "linear.states"} {
		if !strings.Contains(out, k) {
			t.Errorf("config list lacks %s", k)
		}
	}
}

func TestAgentAutoProfileFlags(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	repo, env := sandbox(t)
	bin := filepath.Join(filepath.Dir(repo), "bin")
	rec := filepath.Join(filepath.Dir(repo), "claude.argv")
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+rec+"\ncat > /dev/null\n"), 0o755)
	prompt := filepath.Join(repo, "p.md")
	os.WriteFile(prompt, []byte("hi"), 0o600)
	env = append(env, "AGENT_CLI=claude", "AGENT_PERMISSION_MODE=bypassPermissions")
	if _, errOut, code := run(t, repo, env, "agent", "--profile", "auto", prompt); code != 0 {
		t.Fatalf("agent: %d %s", code, errOut)
	}
	b, _ := os.ReadFile(rec)
	got := string(b)
	if !strings.Contains(got, "--permission-mode\ndontAsk\n--allowedTools\n") || strings.Contains(got, "bypass") || !strings.Contains(got, "Bash(git *)") {
		t.Errorf("claude argv = %q", got)
	}
	if _, _, code := run(t, repo, env, "agent", "--profile", "yolo", prompt); code != 2 {
		t.Errorf("unknown profile exit %d", code)
	}
}

func TestAutoServeSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no SIGTERM")
	}
	repo, env := sandbox(t)
	home := strings.TrimPrefix(env[1], "SMITHY_HOME=")
	c := exec.Command(bin, "auto", "serve", "--agent", "claude")
	c.Dir, c.Env = repo, env
	var o bytes.Buffer
	c.Stdout, c.Stderr = &o, &o
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	hb := filepath.Join(home, "auto", "serve.json")
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(hb); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(hb); err != nil {
		c.Process.Kill()
		t.Fatalf("no heartbeat: %v\n%s", err, o.String())
	}
	time.Sleep(200 * time.Millisecond)
	c.Process.Signal(syscall.SIGTERM)
	if err := c.Wait(); err != nil {
		t.Errorf("exit: %v\n%s", err, o.String())
	}
	if _, err := os.Stat(hb); !os.IsNotExist(err) {
		t.Error("heartbeat left after SIGTERM")
	}
	if !strings.Contains(o.String(), "serve started") || !strings.Contains(o.String(), "serve stopped") {
		t.Errorf("output = %q", o.String())
	}
}
