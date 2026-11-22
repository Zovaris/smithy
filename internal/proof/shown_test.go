package proof

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShownLeavesOneCommandLineBare(t *testing.T) {
	for want, argv := range map[string][]string{
		"npm test":         {"npm test"},
		"go test './x y'":  {"go", "test", "./x y"},
		"npx vitest run a": {"npx", "vitest", "run", "a"},
	} {
		if got := Shown(argv); got != want {
			t.Errorf("Shown(%q) = %q, want %q", argv, got, want)
		}
	}
}

func TestShownQuotesAnExistingPathWithASpace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "my tool")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := Shown([]string{p}), "'"+p+"'"; got != want {
		t.Errorf("Shown = %q, want %q", got, want)
	}
}
