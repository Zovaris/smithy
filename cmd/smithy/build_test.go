//go:build !harness

package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSkillsBuildHasNoSQLite(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skip("go list unavailable:", err)
	}
	if strings.Contains(string(out), "modernc.org/sqlite") || strings.Contains(string(out), "internal/store") {
		t.Fatal("the skills build links the SQLite store")
	}
}

func TestHarnessCommandsInSkillsBuild(t *testing.T) {
	for _, name := range harnessOnly {
		c, ok := commands[name]
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if c.help != "" {
			t.Errorf("%s is listed in --help of the skills build", name)
		}
		var b strings.Builder
		if code := needsHarness(&b, name); code != 2 || !strings.Contains(b.String(), "smithy-harness-") || !strings.Contains(b.String(), "-tags harness") {
			t.Fatalf("%s: code %d, %q", name, code, b.String())
		}
	}
}
