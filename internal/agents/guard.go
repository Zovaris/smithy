package agents

import (
	"encoding/json"
	"os"
	"strings"
)

// secretPaths are the files no driven CLI should read: smithy's auth.json and pool accounts,
// ssh, gh, aws, netrc and git credentials.
var secretPaths = []string{"~/.smithy/auth.json", "~/.smithy/auth.key", "~/.smithy/accounts/**", "~/.ssh/**", "~/.config/gh/**", "~/.aws/**", "~/.netrc", "~/.git-credentials"}

// SecretGuarded reports whether cli's own config can deny reads of the secret paths: claude
// (deny rules) and opencode (permission config). codex, gemini, pi, cursor and dsh have no
// per-path read deny, so a run on them can read those files.
func SecretGuarded(cli string) bool {
	k := Kind(cli)
	return k == "claude" || k == "opencode"
}

// UnguardedNote is the documented limit for CLIs SecretGuarded rejects.
const UnguardedNote = "has no per-path read deny, so it can read ~/.smithy/auth.json and other secrets (use claude or opencode)"

// OpenCodePermission is the OPENCODE_PERMISSION JSON for an opencode run under --profile auto
// (the AutoTools allow-list as bash rules, AutoDenied and secret reads denied, no web fetch, no
// directories outside the project) or --readonly (no edits, secret reads denied); "" otherwise.
// opencode applies the last matching rule, so each object lists the catch-all first. Guarded runs
// also lose OPENCODE_CONFIG and OPENCODE_CONFIG_CONTENT from the parent env (Env), which could
// otherwise feed rules into the merge.
func OpenCodePermission(o Options, home string) string {
	if o.Profile != "auto" && !o.ReadOnly {
		return ""
	}
	var paths []string
	for _, p := range secretPaths {
		paths = append(paths, p)
		if home != "" {
			paths = append(paths, home+strings.TrimPrefix(p, "~"))
		}
	}
	secret := rules("*", "allow", paths, "deny")
	if o.ReadOnly {
		return `{"edit":"deny","webfetch":"deny","websearch":"deny","codesearch":"deny","external_directory":"deny","bash":` + rules("*", "deny", nil, "") + `,"read":` + secret + `}`
	}
	// the worker must not widen its own rules: opencode's config files join AutoDenied's Edit list
	edits := []string{"opencode.json", "opencode.jsonc", "**/opencode.json", "**/opencode.jsonc", ".opencode/**", "**/.opencode/**"}
	for _, t := range AutoDenied(o.SmithyBins) {
		if strings.HasPrefix(t, "Edit(") && strings.HasSuffix(t, ")") {
			f := t[5 : len(t)-1]
			edits = append(edits, f)
			if home != "" && strings.HasPrefix(f, "~/") {
				edits = append(edits, home+f[1:])
			}
		}
	}
	var allow, deny []string
	for _, t := range AutoTools(o.SmithyBins) {
		if c, ok := bashRule(t); ok {
			allow = append(allow, c)
		}
	}
	for _, t := range AutoDenied(o.SmithyBins) {
		if c, ok := bashRule(t); ok {
			deny = append(deny, c)
		}
	}
	bash := rules("*", "deny", allow, "allow")
	if len(deny) > 0 {
		bash = bash[:len(bash)-1] + "," + strings.TrimPrefix(rules("", "", deny, "deny"), "{")
	}
	return `{"webfetch":"deny","websearch":"deny","codesearch":"deny","external_directory":"deny","bash":` + bash + `,"edit":` + rules("*", "allow", edits, "deny") + `,"read":` + secret + `}`
}

func bashRule(t string) (string, bool) {
	if !strings.HasPrefix(t, "Bash(") || !strings.HasSuffix(t, ")") {
		return "", false
	}
	return t[5 : len(t)-1], true
}

// rules writes an ordered JSON object: first:firstAct (when first is set), then each key:act.
func rules(first, firstAct string, keys []string, act string) string {
	var b strings.Builder
	b.WriteByte('{')
	n := 0
	add := func(k, v string) {
		if n > 0 {
			b.WriteByte(',')
		}
		kj, _ := json.Marshal(k)
		b.Write(kj)
		b.WriteString(`:"` + v + `"`)
		n++
	}
	if first != "" {
		add(first, firstAct)
	}
	for _, k := range keys {
		add(k, act)
	}
	b.WriteByte('}')
	return b.String()
}

func opencodeEnv(s Spec, o Options) []string {
	if s.Harness != "opencode" {
		return nil
	}
	home, _ := os.UserHomeDir()
	if p := OpenCodePermission(o, home); p != "" {
		return []string{"OPENCODE_PERMISSION=" + p}
	}
	return nil
}
