package sandbox

import "github.com/Zovaris/smithy/internal/agents/catalog"

// SecretPaths are the files no driven CLI should read: smithy's auth.json and pool accounts,
// ssh, gh, aws, netrc and git credentials.
var SecretPaths = []string{"~/.smithy/auth.json", "~/.smithy/auth.key", "~/.smithy/accounts/**", "~/.ssh/**", "~/.config/gh/**", "~/.aws/**", "~/.netrc", "~/.git-credentials"}

// SecretGuarded reports whether cli's own config can deny reads of the secret paths: claude
// (deny rules) and opencode (permission config). codex, agy, copilot, grok, pi, cursor and dsh have no
// per-path read deny, so a run on them can read those files.
func SecretGuarded(cli string) bool {
	k := catalog.Kind(cli)
	return k == "claude" || k == "opencode"
}

// UnguardedNote is the documented limit for CLIs SecretGuarded rejects.
const UnguardedNote = "has no per-path read deny, so it can read ~/.smithy/auth.json and other secrets (use claude or opencode)"
