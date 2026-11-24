//go:build harness

package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestHarnessBuildLinksStore(t *testing.T) {
	out, err := exec.Command("go", "list", "-tags", "harness", "-deps", ".").Output()
	if err != nil {
		t.Skip("go list unavailable:", err)
	}
	if !strings.Contains(string(out), "modernc.org/sqlite") {
		t.Fatal("the harness build does not link the SQLite store")
	}
}
