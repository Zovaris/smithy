package auto

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDefaultAgentTakesAnAPIAgentFromUserSettingsOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMITHY_HOME", home)
	t.Setenv("SMITHY_CONFIG", filepath.Join(home, "config.json"))
	t.Setenv("AGENT_CLI", "claude")
	repo := t.TempDir()
	if err := exec.Command("git", "-C", repo, "init", "-q").Run(); err != nil {
		t.Skip("git unavailable")
	}
	os.MkdirAll(filepath.Join(repo, ".smithy"), 0o755)
	os.WriteFile(filepath.Join(repo, ".smithy", "config.json"), []byte(`{"auto":{"agent":"api:openrouter/x/y"}}`), 0o644)
	t.Chdir(repo)
	if a := DefaultAgent(); a != "claude" {
		t.Fatalf("a repo picked the API agent: %q", a)
	}
	os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"auto":{"agent":"api:openrouter/x/y"}}`), 0o600)
	if a := DefaultAgent(); a != "api:openrouter/x/y" {
		t.Fatalf("user setting ignored: %q", a)
	}
}
