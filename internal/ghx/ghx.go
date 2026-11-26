// Package ghx runs git and the GitHub CLI.
package ghx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SkillDir is the skill's scripts folder the launcher ran from (empty when run directly).
func SkillDir() string { return os.Getenv("SMITHY_SKILL_DIR") }

// RepoRoot is the top of the git checkout at dir, or "" outside one.
func RepoRoot(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Run runs a command and returns trimmed stdout; the error carries stderr's first line.
func Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// GH runs gh and decodes its JSON output into v.
func GH(v any, args ...string) error {
	out, err := Run("gh", args...)
	if err != nil {
		return err
	}
	if out == "" {
		out = "null"
	}
	return json.Unmarshal([]byte(out), v)
}
