package agents

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Zovaris/smithy/internal/acp"
	"github.com/Zovaris/smithy/internal/layout"
)

// autoPermit maps one ACP permission request through the --profile auto rules (AutoTools,
// AutoDenied, SecretDenied): commands must match an allowed Bash rule and no denied one, edits
// stay inside cwd and off every denied Edit path, reads stay off the secrets. Anything it can't
// classify (no kind, fetch, delete, move, other, no command or path) is rejected.
func autoPermit(p acp.Permission, cwd string, bins []string) (bool, string) {
	denied := AutoDenied(bins)
	switch p.Kind {
	case "think", "search":
		return true, ""
	case "read":
		for _, path := range p.Paths {
			if matchAny(denied, "Bash", path) || matchAny(denied, "Read", path) {
				return false, "reads a secret path"
			}
		}
		return true, ""
	case "edit":
		if len(p.Paths) == 0 {
			return false, "edit without a path"
		}
		root := filepath.Clean(cwd)
		for _, path := range p.Paths {
			c := filepath.Clean(path)
			if !filepath.IsAbs(c) || !strings.HasPrefix(c, root+string(filepath.Separator)) {
				return false, "edit outside the working folder"
			}
			if matchAny(denied, "Edit", c) {
				return false, "edit of a denied path"
			}
		}
		return true, ""
	case "execute":
		cmd := strings.TrimSpace(p.Command)
		if cmd == "" {
			return false, "command not given"
		}
		if strings.ContainsAny(cmd, ";&|`$<>\n\r") {
			return false, "compound or substituted command"
		}
		if matchAny(denied, "Bash", cmd) {
			return false, "command denied under auto"
		}
		if !matchAny(AutoTools(bins), "Bash", cmd) {
			return false, "command not on the auto allow-list"
		}
		return true, ""
	}
	return false, "tool kind " + strings.TrimSpace(p.Kind+" unclassified")
}

// matchAny reports whether s matches one of rules' Tool(pattern) entries for tool.
func matchAny(rules []string, tool, s string) bool {
	for _, r := range rules {
		pat, ok := strings.CutPrefix(r, tool+"(")
		if !ok || !strings.HasSuffix(pat, ")") {
			continue
		}
		pat = strings.TrimSuffix(pat, ")")
		var re string
		if tool == "Bash" {
			re = "^" + strings.ReplaceAll(regexp.QuoteMeta(pat), `\*`, ".*") + "$"
		} else {
			re = pathRule(pat)
		}
		if m, err := regexp.MatchString(re, filepath.ToSlash(s)); err == nil && m {
			return true
		}
	}
	return false
}

// pathRule turns a Read/Edit rule path (~/x, //abs, **/x, * and **) into an anchored regexp.
func pathRule(pat string) string {
	if strings.HasPrefix(pat, "~/") {
		home := layout.UserHome()
		pat = filepath.ToSlash(home) + pat[1:]
	} else if strings.HasPrefix(pat, "//") {
		pat = pat[1:]
	} else if strings.HasPrefix(pat, "**/") {
		pat = "**" + pat[2:]
	}
	q := regexp.QuoteMeta(pat)
	q = strings.ReplaceAll(q, `\*\*`, "\x00")
	q = strings.ReplaceAll(q, `\*`, "[^/]*")
	q = strings.ReplaceAll(q, "\x00", ".*")
	return "^" + q + "$"
}
