package warden

import (
	"strings"
	"testing"
)

func TestDoctorDesktopSkipsAgentCLI(t *testing.T) {
	newDoctor(t, "git", "gh", "claude")
	t.Setenv("AGENT_CMD", "")
	t.Setenv("WARDEN_HARNESS", "claude-desktop")
	o, _ := doctor(t)
	if strings.Contains(o, "agent CLI") {
		t.Errorf("desktop adapter prints an agent CLI line:\n%s", o)
	}
}

func TestDoctorSubagentFallback(t *testing.T) {
	newDoctor(t, "git", "gh")
	t.Setenv("AGENT_CMD", "")
	t.Setenv("WARDEN_HARNESS", "claude-desktop")
	if o, _ := doctor(t); !strings.Contains(o, "  warn  workers: subagent fallback; they end with this session") {
		t.Errorf("desktop without claude CLI:\n%s", o)
	}
	newDoctor(t, "git", "gh", "claude")
	t.Setenv("AGENT_CMD", "")
	t.Setenv("WARDEN_HARNESS", "claude-cli")
	if o, _ := doctor(t); strings.Contains(o, "subagent fallback") {
		t.Errorf("claude CLI present:\n%s", o)
	}
}
