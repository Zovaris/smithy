package p

import (
	"os"
	"strings"
	"testing"
)

func TestChangelog(t *testing.T) {
	b, _ := os.ReadFile("CHANGELOG.md")
	s := string(b)
	u, r := strings.Index(s, "## Unreleased"), strings.Index(s, "## v1.0.0")
	if u < 0 || r < u {
		t.Fatal("CHANGELOG.md lost its sections")
	}
	if part := s[u:r]; !strings.Contains(part, "- ") || !strings.Contains(part, "Clamp") {
		t.Fatal("no bullet about Clamp under Unreleased")
	}
}
