package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Zovaris/smithy/internal/config"
)

const routeUsage = `Model router: picks the cheapest model tier that can do a task well, from the task itself.
  smithy route [--file <task.md>] [--text "<task words>"] [--kind ship|scout|review|fix|tend|test]
               [--agent <agent file or name>] [--speed fast|normal|full] [--json]
--agent picks the kind from a smelt agent: spec-check, standards, security and builder are review;
adversary, regression, property, mutation, e2e and hostile are test; implementer is ship (give
the task with --file or --text); tend and scout (or their SKILL.md) are their own kind.
Prints ` + "`<tier> <model>  # reasons`" + `, or JSON with --json. <model> is the user's model for that
tier in this harness (smithy config model <tier>); ` + "`-`" + ` when unset (use the session default, and
ask the user to name the tier once and save it with ` + "`smithy config set-model`" + `). The caller may move one tier up or down with a stated reason;
a user's explicit model always wins.
Tiers: cheap (mechanical, read-only, small), mid (most feature work), strong (risky, ambiguous,
cross-cutting, or the last fix round).`

var (
	risky   = []string{"auth", "login", "password", "secret", "access token", "auth token", "api token", "api key", "permission", "security", "payment", "billing", "money", "invoice", "migration", "schema", "database", "concurren", "race condition", "lock contention", "deadlock", "mutex", "crypto", "encrypt", "data loss", "delete user", "production", "public api"}
	hard    = []string{"architecture", "redesign", "refactor", "performance", "investigate", "root cause", "why does", "ambiguous", "trade-off", "cross-cutting", "rewrite", "remake", "protocol", "parser", "compiler", "algorithm"}
	prose   = []string{"doc", "docs", "documentation", "readme", "contributing", "changelog", "guide", "adr", "wording"}
	trivial = []string{"typo", "copy change", "wording", "rename", "bump", "version", "color", "colour", "style tweak", "readme", "comment", "lint", "format"}

	criteriaRe  = regexp.MustCompile(`(?m)^\s*- \[[ x]\]`)
	filesRe     = regexp.MustCompile(`## Files((?s:.*?))(\n## |$)`)
	interfaceRe = regexp.MustCompile(`## Interfaces((?s:.*?))(\n## |$)`)
	pathRe      = regexp.MustCompile(`[\w./-]+\.\w+`)
	bulletRe    = regexp.MustCompile(`(?m)^\s*- `)
)

// Tiers are the model tiers, cheapest first.
var Tiers = []string{"cheap", "mid", "strong"}

func suffix(w string) string {
	for _, p := range prose {
		if p == w {
			return `s?\b`
		}
	}
	return ""
}

var negated = regexp.MustCompile(`(?i)\b(no|without|never|zero)\s+(\w+\s+){0,1}$`)

// matches returns the words of the list found in low as they appear there, skipping any that a
// "no", "without", "never" or "zero" just before negates ("no secrets needed").
func matches(low string, words []string) []string {
	var out []string
	for _, w := range words {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + suffix(w) + `\w*`)
		for _, loc := range re.FindAllStringIndex(low, -1) {
			if negated.MatchString(low[:loc[0]]) {
				continue
			}
			out = append(out, low[loc[0]:loc[1]])
			break
		}
	}
	return out
}

func section(re *regexp.Regexp, text string, count *regexp.Regexp) int {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	return len(count.FindAllString(m[1], -1))
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// Route picks the tier for a task text, kind (ship, scout, review, fix, tend) and speed (fast,
// normal, full), with the reasons.
func Route(text, kind, speed string) (string, []string) {
	low := strings.ToLower(text)
	criteria := len(criteriaRe.FindAllString(text, -1))
	files := section(filesRe, text, pathRe)
	interfaces := section(interfaceRe, text, bulletRe)
	score, reasons := 1, []string{}
	title := strings.ToLower(strings.SplitN(strings.TrimSpace(text), "\n", 2)[0])
	risk, hrd, triv := matches(low, risky), matches(low, hard), matches(title, trivial)
	if len(matches(title, prose)) > 0 && len(matches(title, risky)) == 0 {
		return Tiers[0], []string{"prose only: " + strings.Join(first(matches(title, prose), 2), ", ")}
	}
	if kind == "scout" || kind == "review" || kind == "tend" {
		score = 0
		reasons = append(reasons, kind+" work")
	}
	if len(risk) > 0 {
		score = 2
		reasons = append(reasons, "risky: "+strings.Join(first(risk, 3), ", "))
	}
	if len(hrd) > 0 && score < 2 {
		score++
		reasons = append(reasons, "hard: "+strings.Join(first(hrd, 3), ", "))
	}
	if criteria > 6 || files > 8 || interfaces > 4 {
		score++
		reasons = append(reasons, fmt.Sprintf("large: %d criteria, %d files, %d interfaces", criteria, files, interfaces))
	}
	if len(risk) == 0 && len(hrd) == 0 && len(triv) > 0 && criteria <= 2 && files <= 2 {
		score--
		reasons = append(reasons, "small: "+strings.Join(first(triv, 2), ", "))
	}
	if speed == "full" {
		score = max(score, 2)
		reasons = append(reasons, "speed full")
	}
	if speed == "fast" && len(risk) == 0 {
		score = min(score, 1)
		reasons = append(reasons, "speed fast")
	}
	if kind == "test" {
		reasons = append(reasons, "test agent")
	}
	if kind == "fix" {
		score = max(score, 1)
		reasons = append(reasons, "code fix")
	}
	score = max(0, min(2, score))
	if len(reasons) == 0 {
		reasons = append(reasons, "ordinary feature work")
	}
	return Tiers[score], reasons
}

// AgentKind maps a smelt agent file or name to its routing kind.
func AgentKind(agent string) (string, bool) {
	name := strings.TrimSuffix(agent[strings.LastIndex(agent, "/")+1:], ".md")
	if name == "SKILL" {
		dir := strings.TrimSuffix(agent, "/SKILL.md")
		name = dir[strings.LastIndex(dir, "/")+1:]
	}
	switch name {
	case "spec-check", "standards", "security", "builder":
		return "review", true
	case "adversary", "regression", "property", "mutation", "e2e", "hostile":
		return "test", true
	case "implementer":
		return "ship", true
	case "tend", "scout":
		return name, true
	}
	return "", false
}

func oneOf(s string, opts ...string) bool {
	for _, o := range opts {
		if s == o {
			return true
		}
	}
	return false
}

// RouteMain runs `smithy route` and returns the exit code.
func RouteMain(args []string) int {
	opt := func(k string) (string, bool) {
		for i, a := range args {
			if a == k {
				if i+1 < len(args) {
					return args[i+1], true
				}
				return "", false
			}
		}
		return "", false
	}
	has := func(k string) bool {
		for _, a := range args {
			if a == k {
				return true
			}
		}
		return false
	}
	if has("-h") || has("--help") {
		fmt.Fprintln(os.Stderr, routeUsage)
		return 2
	}
	for i, a := range args {
		switch a {
		case "--file", "--text", "--kind", "--agent", "--speed":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				fmt.Fprintf(os.Stderr, "smithy route: %s needs a value\n", a)
				return 2
			}
		}
	}
	if k, ok := opt("--kind"); ok && !oneOf(k, "ship", "scout", "review", "fix", "tend", "test") {
		fmt.Fprintf(os.Stderr, "smithy route: unknown --kind %q (ship, scout, review, fix, tend, test)\n", k)
		return 2
	}
	if s, ok := opt("--speed"); ok && !oneOf(s, "fast", "normal", "full") {
		fmt.Fprintf(os.Stderr, "smithy route: unknown --speed %q (fast, normal, full)\n", s)
		return 2
	}
	text, _ := opt("--text")
	if f, ok := opt("--file"); ok {
		b, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy route:", err)
			return 1
		}
		text += "\n" + string(b)
	}
	kind, _ := opt("--kind")
	if a, ok := opt("--agent"); ok {
		k, known := AgentKind(a)
		if !known {
			fmt.Fprintf(os.Stderr, "smithy route: unknown agent %q\n", a)
			return 2
		}
		kind = k
	}
	if kind == "" {
		kind = "ship"
	}
	speed, _ := opt("--speed")
	if speed == "" {
		speed = "normal"
	}
	if strings.TrimSpace(text) == "" && kind == "ship" {
		fmt.Fprintln(os.Stderr, "smithy route: give --file or --text")
		return 2
	}
	tier, reasons := Route(text, kind, speed)
	model, _ := config.Model(tier)
	if has("--json") {
		var m *string
		if model != "" {
			m = &model
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(struct {
			Tier    string   `json:"tier"`
			Model   *string  `json:"model"`
			Reasons []string `json:"reasons"`
		}{tier, m, reasons})
		return 0
	}
	if model == "" {
		model = "-"
	}
	fmt.Printf("%s %s  # %s\n", tier, model, strings.Join(reasons, "; "))
	return 0
}
