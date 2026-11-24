package warden

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/config"
)

func sandboxReport(t *testing.T, mode, goos string, have ...string) (string, bool) {
	t.Helper()
	version := "2.1.331 (Claude Code)"
	if goos == "old" {
		goos, version = "darwin", "2.0.1 (Claude Code)"
	}
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	t.Setenv("AGENT_CMD", "")
	t.Setenv("AGENT_CLI", "")
	if err := config.Set(config.UserFile(), "auto.sandbox", mode); err != nil {
		t.Fatal(err)
	}
	if err := config.Set(config.UserFile(), "auto.agent", "claude"); err != nil {
		t.Fatal(err)
	}
	p := agents.Probe{GOOS: goos, Look: func(b string) (string, error) {
		if slices.Contains(have, b) {
			return b, nil
		}
		return "", errors.New("no")
	}, Version: func(string) (string, error) { return version, nil }}
	r := &report{}
	o, _, _ := capture(t, func() int { sandboxDoctor(r, p); return 0 })
	return o, r.failed
}

func TestSandboxDoctor(t *testing.T) {
	cases := []struct {
		mode, goos string
		have       []string
		want       string
		failed     bool
	}{
		{"off", "darwin", nil, "ok    sandbox: off", false},
		{"native", "darwin", []string{"/usr/bin/sandbox-exec"}, "ok    sandbox: native (claude, hosts github.com,", false},
		{"native", "darwin", nil, "FAIL  sandbox native: sandbox-exec missing", true},
		{"native", "linux", []string{"bwrap"}, "socat missing", true},
		{"container", "linux", []string{"podman"}, "not live-tested yet", false},
		{"container", "linux", nil, "no docker or podman", true},
		{"native", "old", []string{"/usr/bin/sandbox-exec"}, "older than 2.1.331", true},
	}
	for _, c := range cases {
		o, failed := sandboxReport(t, c.mode, c.goos, c.have...)
		if !strings.Contains(o, c.want) || failed != c.failed {
			t.Errorf("%s/%s: failed=%v want %v\n%s", c.mode, c.goos, failed, c.failed, o)
		}
	}
}

func TestSandboxDoctorPoolContainer(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	t.Setenv("SMITHY_CONFIG", filepath.Join(t.TempDir(), "c.json"))
	for k, v := range map[string]string{"auto.sandbox": "container", "auto.agent": "pool"} {
		if err := config.Set(config.UserFile(), k, v); err != nil {
			t.Fatal(err)
		}
	}
	p := agents.Probe{GOOS: "linux", Look: func(b string) (string, error) { return b, nil }}
	r := &report{}
	o, _, _ := capture(t, func() int { sandboxDoctor(r, p); return 0 })
	if !r.failed || !strings.Contains(o, "pool and container don't combine yet") {
		t.Errorf("want a FAIL line for pool + container:\n%s", o)
	}
}
