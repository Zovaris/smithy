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

func TestBareSmithyOpensChat(t *testing.T) {
	if noArgs == nil {
		t.Fatal("bare smithy does not open the chat in the harness build")
	}
	if _, ok := commands["chat"]; !ok {
		t.Fatal("smithy chat is not registered")
	}
}
