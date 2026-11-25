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
	for _, sdk := range []string{"anthropic-sdk-go", "openai-go", "google.golang.org/genai", "internal/apiloop/provider", "fastschema/qjs", "tetratelabs/wazero", "internal/codemode"} {
		if strings.Contains(string(out), sdk) {
			t.Fatalf("the skills build links %s (harness build only, docs/adr/0012)", sdk)
		}
	}
}

func TestHarnessCommandsInSkillsBuild(t *testing.T) {
	for _, name := range harnessOnly {
		c, ok := commands[name]
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		var top strings.Builder
		topHelp(&top, true)
		if c.help == "" || !strings.Contains(top.String(), name) || !strings.Contains(strings.Join(strings.Fields(top.String()), " "), c.help+" (harness build)") {
			t.Errorf("%s is not listed as (harness build) in the skills build's help", name)
		}
		var b strings.Builder
		if code := needsHarness(&b, name); code != 2 || !strings.Contains(b.String(), "smithy-harness-") || !strings.Contains(b.String(), "-tags harness") {
			t.Fatalf("%s: code %d, %q", name, code, b.String())
		}
	}
}
