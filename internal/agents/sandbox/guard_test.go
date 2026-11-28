package sandbox

import (
	"testing"
)

func TestSecretGuarded(t *testing.T) {
	for cli, want := range map[string]bool{"claude": true, "opencode": true, "codex": false, "agy": false, "copilot": false, "grok": false, "pi": false, "agent": false, "dsh": false} {
		if got := SecretGuarded(cli); got != want {
			t.Errorf("SecretGuarded(%s) = %v", cli, got)
		}
	}
}
