package agents

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseArchiveListsEveryKnownSkill(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "smithy-skills.tar.gz") && strings.Contains(l, "tar -czf") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("release.yml has no smithy-skills.tar.gz tar line")
	}
	fields := strings.Fields(line)
	for _, k := range Known {
		if !containsField(fields, k) {
			t.Errorf("release archive misses skill %q", k)
		}
	}
}

func containsField(fs []string, s string) bool {
	for _, f := range fs {
		if f == s {
			return true
		}
	}
	return false
}
