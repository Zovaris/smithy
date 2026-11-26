package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenCodePermissionAuto(t *testing.T) {
	p := OpenCodePermission(Options{Profile: "auto"}, "/home/dev")
	var m map[string]any
	if err := json.Unmarshal([]byte(p), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, p)
	}
	if m["webfetch"] != "deny" || m["external_directory"] != "deny" {
		t.Errorf("webfetch/external_directory not denied: %s", p)
	}
	bash := m["bash"].(map[string]any)
	if bash["*"] != "deny" || bash["git status *"] != "allow" || bash["curl *"] != "deny" || bash["*auth.json*"] != "deny" {
		t.Errorf("bash rules: %v", bash)
	}
	if strings.Index(p, `"*":"deny"`) > strings.Index(p, `"git status *":"allow"`) || strings.Index(p, `"git status *":"allow"`) > strings.Index(p, `"*auth.json*":"deny"`) {
		t.Errorf("order must be catch-all deny, allows, then denies (last match wins): %s", p)
	}
	read := m["read"].(map[string]any)
	if read["*"] != "allow" || read["/home/dev/.smithy/auth.json"] != "deny" || read["~/.smithy/auth.json"] != "deny" {
		t.Errorf("read rules: %v", read)
	}
	edit := m["edit"].(map[string]any)
	if edit["/home/dev/.smithy/auth.json"] != "deny" || edit["opencode.json"] != "deny" || edit[".opencode/**"] != "deny" || edit["**/.git/hooks/**"] != "deny" || m["websearch"] != "deny" {
		t.Errorf("edit rules: %v", m["edit"])
	}
}

func TestOpenCodePermissionReadOnly(t *testing.T) {
	p := OpenCodePermission(Options{ReadOnly: true}, "/home/dev")
	var m map[string]any
	if err := json.Unmarshal([]byte(p), &m); err != nil {
		t.Fatal(err)
	}
	if m["read"].(map[string]any)["/home/dev/.ssh/**"] != "deny" || m["edit"] != "deny" {
		t.Errorf("readonly rules: %s", p)
	}
	if OpenCodePermission(Options{}, "/home/dev") != "" {
		t.Error("an interactive run gets no permission override")
	}
}

func TestEnvOpenCodePermission(t *testing.T) {
	env := Env(nil, Spec{Harness: "opencode"}, Options{Profile: "auto"})
	if !hasPrefix(env, "OPENCODE_PERMISSION={") {
		t.Errorf("opencode auto run has no OPENCODE_PERMISSION: %v", env)
	}
	env = Env([]string{"OPENCODE_CONFIG_CONTENT={}", "OPENCODE_CONFIG=/x"}, Spec{Harness: "opencode"}, Options{ReadOnly: true})
	if hasPrefix(env, "OPENCODE_CONFIG_CONTENT=") || hasPrefix(env, "OPENCODE_CONFIG=") {
		t.Errorf("parent opencode config env kept: %v", env)
	}
	if hasPrefix(Env(nil, Spec{Harness: "codex"}, Options{Profile: "auto"}), "OPENCODE_PERMISSION=") {
		t.Error("codex run got OPENCODE_PERMISSION")
	}
}

func TestSecretGuarded(t *testing.T) {
	for cli, want := range map[string]bool{"claude": true, "opencode": true, "codex": false, "agy": false, "copilot": false, "grok": false, "pi": false, "agent": false, "dsh": false} {
		if got := SecretGuarded(cli); got != want {
			t.Errorf("SecretGuarded(%s) = %v", cli, got)
		}
	}
}

func hasPrefix(env []string, p string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, p) {
			return true
		}
	}
	return false
}
