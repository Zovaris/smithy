package guard

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Zovaris/smithy/internal/config"
)

// Suggested is the allowlist the settings UI offers to add in one action (smithy guard suggest
// --add): GitHub, model APIs, package registries and docs sites.
var Suggested = []string{
	"github.com", "*.github.com", "*.githubusercontent.com",
	"api.anthropic.com", "api.openai.com", "generativelanguage.googleapis.com", "openrouter.ai",
	"registry.npmjs.org", "pypi.org", "files.pythonhosted.org", "proxy.golang.org", "sum.golang.org",
	"crates.io", "static.crates.io", "rubygems.org",
	"pkg.go.dev", "developer.mozilla.org", "docs.python.org", "docs.rs", "nodejs.org", "go.dev",
}

// FetchRules are the fetch guard's domain globs ("*.example.com" matches subdomains only).
type FetchRules struct {
	Enabled      bool
	Allow, Block []string
}

// LoadFetch reads guard.fetch_block (a repo may add to it) and guard.fetch_allow (the user's
// config only), each a comma list or a JSON list of domain globs.
func LoadFetch() (FetchRules, error) {
	f := FetchRules{Enabled: Enabled()}
	var err error
	if f.Block, err = hosts(config.Get("guard.fetch_block", "")); err != nil {
		return f, fmt.Errorf("guard.fetch_block: %w", err)
	}
	allow, err := config.UserGet("guard.fetch_allow", "")
	if err == nil {
		f.Allow, err = hosts(allow)
	}
	if err != nil {
		return f, fmt.Errorf("guard.fetch_allow: %w", err)
	}
	return f, nil
}

func hosts(v string) ([]string, error) {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "[") {
		return List(v)
	}
	var out []string
	for _, h := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		out = append(out, strings.ToLower(h))
	}
	return out, nil
}

// HostMatch reports whether host matches glob: an exact name, "*.example.com" for its
// subdomains, or "*" for every host.
func HostMatch(glob, host string) bool {
	glob = strings.ToLower(strings.TrimSpace(glob))
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if glob == "*" {
		return true
	}
	if s, ok := strings.CutPrefix(glob, "*."); ok {
		return strings.HasSuffix(host, "."+s)
	}
	return host == glob
}

// Match returns the block glob host matches unless an allow glob matches it too; "" lets it go.
func (f FetchRules) Match(host string) string {
	if !f.Enabled || host == "" {
		return ""
	}
	for _, a := range f.Allow {
		if HostMatch(a, host) {
			return ""
		}
	}
	for _, b := range f.Block {
		if HostMatch(b, host) {
			return b
		}
	}
	return ""
}

// RememberHost appends host to the user's guard.fetch_allow.
func RememberHost(host string) error { return appendList("guard.fetch_allow", host, hosts) }

// AddSuggested merges Suggested into the user's guard.fetch_allow and returns how many it added.
func AddSuggested() (int, error) {
	n := 0
	for _, h := range Suggested {
		cur, err := config.UserGet("guard.fetch_allow", "")
		if err != nil {
			return n, err
		}
		have, _ := hosts(cur)
		if !contains(have, h) {
			if err := RememberHost(h); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

// SuggestedJSON is Suggested with whether each is already allowed, for the settings UI.
func SuggestedJSON() string {
	cur, _ := config.UserGet("guard.fetch_allow", "")
	have, _ := hosts(cur)
	type row struct {
		Host  string `json:"host"`
		Added bool   `json:"added"`
	}
	rows := make([]row, 0, len(Suggested))
	for _, h := range Suggested {
		rows = append(rows, row{h, contains(have, h)})
	}
	b, _ := json.Marshal(rows)
	return string(b)
}
