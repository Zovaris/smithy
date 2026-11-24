package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func wardenLog(t *testing.T) (string, string) {
	t.Helper()
	wh := t.TempDir()
	t.Setenv("WARDEN_HOME", wh)
	os.MkdirAll(filepath.Join(wh, "b1"), 0o755)
	return filepath.Join(wh, "b1", "01-x.log"), filepath.Join(wh, "b1", "01-x.json")
}

func TestAgentBackgroundWritesTheCardAtLaunch(t *testing.T) {
	p, _ := timeoutPrompt(t)
	log, card := wardenLog(t)
	t.Setenv("AGENT_CMD", "sleep 30")
	old := startCheck
	startCheck = 100 * time.Millisecond
	defer func() { startCheck = old }()
	if rc := AgentMain([]string{"--bg", "--timeout", "0", "--log", log, p}); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	var c map[string]any
	b, _ := os.ReadFile(card)
	if json.Unmarshal(b, &c) != nil || c["state"] != "working" || c["last_beat"] == nil {
		t.Fatalf("card = %s", b)
	}
	pid, ok := c["pid"].(float64)
	if !ok || pid <= 0 {
		t.Fatalf("no pid: %s", b)
	}
	if pr, err := os.FindProcess(int(pid)); err == nil {
		pr.Kill()
	}
}

func TestAgentBackgroundFailsTheCardOnAnImmediateExit(t *testing.T) {
	p, _ := timeoutPrompt(t)
	log, card := wardenLog(t)
	t.Setenv("AGENT_CMD", "sh -c 'echo invalid model; exit 1'")
	if rc := AgentMain([]string{"--bg", "--timeout", "0", "--log", log, p}); rc != 1 {
		t.Fatalf("rc = %d", rc)
	}
	b, _ := os.ReadFile(card)
	if !strings.Contains(string(b), `"state": "failed"`) || !strings.Contains(string(b), "worker exited: invalid model") {
		t.Fatalf("card = %s", b)
	}
}
