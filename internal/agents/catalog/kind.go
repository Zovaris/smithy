package catalog

import (
	"path/filepath"
	"strings"
)

// Kind is the CLI kind of an agent name or path: its base name, with agent meaning cursor.
func Kind(cli string) string {
	if cli == "" || cli == "custom" {
		return cli
	}
	b := strings.TrimSuffix(filepath.Base(cli), ".exe")
	if b == "agent" {
		return "cursor"
	}
	return b
}
