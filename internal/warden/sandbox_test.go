package warden

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/config"
)

func sandboxReport(t *testing.T, mode, goos string, have ...string) (string, bool) {
	t.Helper()
	return sandboxReportWith(t, mode, goos, "bridge", "tok", have...)
}

func sandboxReportWith(t *testing.T, mode, goos, network, token string, have ...string) (string, bool) {
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
	if network != "" {
		if err := config.Set(config.UserFile(), "auto.sandbox_network", network); err != nil {
			t.Fatal(err)
		}
	}
	p := agents.Probe{Getenv: func(k string) string {
		if k == "CLAUDE_CODE_OAUTH_TOKEN" {
			return token
		}
		return ""
	}, GOOS: goos, Look: func(b string) (string, error) {
		if slices.Contains(have, b) {
			return b, nil
		}
		return "", errors.New("no")
	}, Version: func(string) (string, error) { return version, nil }, Exists: func(f string) bool { _, err := os.Stat(f); return err == nil }}
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
		{"container", "linux", []string{"podman"}, "ok    sandbox: container (podman, socket engine default, image smithy-sandbox:latest, network bridge)", false},
		{"container", "linux", []string{"docker"}, "open egress with no domain allow-list", false},
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

func TestSandboxDoctorContainerRefusals(t *testing.T) {
	o, failed := sandboxReportWith(t, "container", "linux", "none", "tok", "docker")
	if !failed || !strings.Contains(o, agents.ContainerNoNetwork) {
		t.Errorf("network none must FAIL:\n%s", o)
	}
	o, failed = sandboxReportWith(t, "container", "linux", "", "tok", "docker")
	if !failed || !strings.Contains(o, agents.ContainerNoNetwork) {
		t.Errorf("network unset must FAIL:\n%s", o)
	}
	t.Setenv("SMITHY_HOME", t.TempDir())
	o, failed = sandboxReportWith(t, "container", "linux", "bridge", "", "docker")
	if !failed || !strings.Contains(o, "claude setup-token, then smithy auth set claude") {
		t.Errorf("no credential must FAIL:\n%s", o)
	}
}

func TestSandboxDoctorProxy(t *testing.T) {
	t.Setenv("SMITHY_HOME", t.TempDir())
	o, failed := sandboxReportWith(t, "container", "darwin", "proxy", "tok", "docker")
	if !failed || !strings.Contains(o, "no linux smithy for the proxy sidecar") {
		t.Errorf("no binary: failed=%v\n%s", failed, o)
	}
	o, failed = sandboxReportWith(t, "container", "linux", "proxy", "tok", "docker")
	if failed || !strings.Contains(o, "egress proxy smithy-proxy on smithy-internal, hosts github.com,") || strings.Contains(o, "open egress") {
		t.Errorf("binary: failed=%v\n%s", failed, o)
	}
}
