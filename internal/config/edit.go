package config

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

var enums = map[string][]string{
	"tasks.location": {"repo", "local", "board"},
	"tasks.commit":   {"true", "false"},
	"tasks.ignore":   {"gitignore", "exclude"},
	"warden.merge":   {"ask", "auto", "manual"},
	"warden.cleanup": {"auto", "ask"},
	"warden.mode":    {"smelt fast", "smelt normal", "smelt full", "plain"},
	"agent.effort":   {"low", "medium"},
	"auto.mode":      {"labeled", "all"},
	"auto.sandbox":   {"off", "native"},
	"ui.theme":       {"steel", "frost", "plum"},
}

var bools = map[string]bool{"auto.paused": true, "auto.retry_stronger": true, "auto.sandbox_weaker_net": true, "decide.enabled": true, "guard.enabled": true, "agent.code_mode": true, "tui.reduce_motion": true, "board.linear_sync": true}

var numbers = map[string]bool{"warden.max": true, "warden.stuck_minutes": true, "ask.timeout": true, "agent.timeout": true,
	"watch.every": true, "watch.max": true, "decide.timeout": true, "auto.max_per_day": true, "auto.every": true, "auto.session_hours": true,
	"auto.max_cost_per_day": true}

// decimals are read with ParseFloat; the other numbers with Atoi.
var decimals = map[string]bool{"auto.max_cost_per_day": true, "watch.every": true, "watch.max": true, "auto.session_hours": true,
	"warden.stuck_minutes": true, "agent.timeout": true, "ask.timeout": true}

// atLeastOne are whole numbers the code rejects below 1.
var atLeastOne = map[string]bool{"warden.max": true, "auto.every": true}

var lists = map[string]bool{"route.domain": true, "watch.events": true, "tend.watch": true, "auto.sources": true, "auto.sandbox_domains": true, "guard.fetch_block": true, "guard.fetch_allow": true, "notify.kinds": true,
	"linear.team": true, "linear.project": true, "linear.states": true}

// KindOf is the input a key takes: bool, enum (with its options), number, list (comma) or text.
func KindOf(key string) (string, []string) {
	switch {
	case bools[key]:
		return "bool", []string{"true", "false"}
	case enums[key] != nil:
		return "enum", enums[key]
	case numbers[key]:
		return "number", nil
	case lists[key]:
		return "list", nil
	}
	return "text", nil
}

// Secret reports a key whose value is a secret itself; *.key_env keys hold a variable's name, not a secret.
func Secret(key string) bool {
	last := key[strings.LastIndex(key, ".")+1:]
	for _, w := range []string{"token", "secret", "password", "api_key", "apikey"} {
		if strings.Contains(strings.ToLower(last), w) {
			return true
		}
	}
	return false
}

// typed checks value against the key's kind and returns its saved form.
func typed(key, value string) (string, error) {
	v := strings.TrimSpace(value)
	kind, opts := KindOf(key)
	switch kind {
	case "bool":
		switch strings.ToLower(v) {
		case "true", "false":
			return strings.ToLower(v), nil
		}
		return "", fmt.Errorf("%s must be true or false, got %q", key, value)
	case "enum":
		for _, o := range opts {
			if strings.EqualFold(v, o) {
				return o, nil
			}
		}
		return "", fmt.Errorf("%s must be one of %s, got %q", key, strings.Join(opts, ", "), value)
	case "number":
		if decimals[key] {
			if f, err := strconv.ParseFloat(v, 64); err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
				return "", fmt.Errorf("%s must be a non-negative number, got %q", key, value)
			}
			return v, nil
		}
		if n, err := strconv.Atoi(v); err != nil || n < 0 || (atLeastOne[key] && n < 1) {
			if atLeastOne[key] {
				return "", fmt.Errorf("%s must be a whole number of at least 1, got %q", key, value)
			}
			return "", fmt.Errorf("%s must be a non-negative whole number, got %q", key, value)
		}
		return v, nil
	case "list":
		var parts []string
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, ","), nil
	}
	return value, nil
}

// scopeFile is the file `smithy config set` writes: the user's, or with repo this repo's .smithy/config.json.
func scopeFile(repo bool) (string, error) {
	if !repo {
		return UserFile(), nil
	}
	file := RepoFile()
	if file == "" {
		return "", fmt.Errorf("not in a git repo")
	}
	if strings.HasSuffix(file, filepath.Join(".agents", "config.json")) {
		file = filepath.Join(filepath.Dir(filepath.Dir(file)), ".smithy", "config.json")
	}
	return file, nil
}

// Apply validates and saves key = value in the user's or the repo's file, the path `smithy config set`
// takes; it returns the file, the saved value and a note for the user.
func Apply(repo bool, key, value string) (file, saved, note string, err error) {
	if err = ValidKey(key); err != nil {
		return
	}
	if file, err = scopeFile(repo); err != nil {
		return
	}
	if saved, note, err = Normalize(key, value); err != nil {
		return
	}
	err = Set(file, key, saved)
	return
}

// Remove unsets key in the user's or the repo's file, the path `smithy config unset` takes.
func Remove(repo bool, key string) (string, bool, error) {
	if err := ValidKey(key); err != nil {
		return "", false, err
	}
	file, err := scopeFile(repo)
	if err != nil {
		return "", false, err
	}
	found, err := Unset(file, key)
	return file, found, err
}
