package p

import (
	"os"
	"strings"
	"testing"
)

func TestReadmeUsage(t *testing.T) {
	b, _ := os.ReadFile("README.md")
	s := string(b)
	i := strings.Index(s, "\n## Usage")
	if i < 0 {
		t.Fatal("README.md has no ## Usage section")
	}
	if rest := s[i:]; !strings.Contains(rest, "```go") || !strings.Contains(rest, "Add(") {
		t.Fatal("## Usage has no Go example calling Add")
	}
}
