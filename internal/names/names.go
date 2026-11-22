// Package names is the one place that names branches and worktrees for smelt, warden and tend.
package names

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
)

var accents = strings.NewReplacer(
	"á", "a", "à", "a", "ä", "a", "â", "a", "ã", "a", "å", "a", "é", "e", "è", "e", "ë", "e", "ê", "e",
	"í", "i", "ì", "i", "ï", "i", "î", "i", "ó", "o", "ò", "o", "ö", "o", "ô", "o", "õ", "o", "ø", "o",
	"ú", "u", "ù", "u", "ü", "u", "û", "u", "ñ", "n", "ç", "c", "ß", "ss",
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a title into lowercase-hyphen form, at most 40 characters, cut at a word boundary.
func Slug(title string) string {
	s := nonAlnum.ReplaceAllString(accents.Replace(strings.ToLower(title)), "-")
	s = strings.Trim(s, "-")
	for len(s) > 40 && strings.Contains(s, "-") {
		s = s[:strings.LastIndex(s, "-")]
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// Type guesses the commit type of a task from its title.
func Type(title string) string {
	t := strings.ToLower(title)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(t, w) {
				return true
			}
		}
		return false
	}
	pre := func(words ...string) bool {
		for _, w := range words {
			if strings.HasPrefix(t, w) {
				return true
			}
		}
		return false
	}
	word := func(words ...string) bool {
		for _, f := range strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) }) {
			for _, w := range words {
				if f == w {
					return true
				}
			}
		}
		return false
	}
	switch {
	case pre("fix") || has("bug", "crash", "broken", "error", "regression"):
		return "fix"
	case pre("doc ", "docs") || word("readme", "adr", "contributing", "changelog", "documentation", "docs", "doc", "guide", "report"):
		return "docs"
	case pre("refactor", "rename", "extract", "move"):
		return "refactor"
	case pre("test") || has("coverage"):
		return "test"
	case pre("chore", "bump", "upgrade", "ci") || has("dependenc"):
		return "chore"
	}
	return "feat"
}

var badKey = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
var cleanup = []struct {
	re  *regexp.Regexp
	rep string
}{{regexp.MustCompile(`/-`), "/"}, {regexp.MustCompile(`--+`), "-"}, {regexp.MustCompile(`-+$`), ""}}

var filler = map[string]bool{"a": true, "an": true, "the": true, "and": true, "or": true, "of": true, "for": true, "from": true, "to": true, "with": true, "in": true, "on": true, "by": true, "into": true}

// branchSlug keeps a branch short: filler words dropped, words already in key dropped, at most
// four words, none a single letter.
func branchSlug(s, key string) string {
	inKey := map[string]bool{}
	for _, w := range strings.Split(strings.ToLower(key), "-") {
		inKey[w] = true
	}
	var kept []string
	for _, w := range strings.Split(s, "-") {
		if len(w) < 2 || filler[w] || inKey[w] {
			continue
		}
		kept = append(kept, w)
		if len(kept) == 4 {
			break
		}
	}
	return strings.Join(kept, "-")
}

// Branch builds a branch from the git.branch template. key is a ticket or task id ("-" or "" for none).
func Branch(key, title, typ string) string {
	if key == "-" {
		key = ""
	}
	key = badKey.ReplaceAllString(strings.TrimPrefix(key, "#"), "-")
	if typ == "" {
		typ = Type(title)
	}
	s := branchSlug(strings.TrimPrefix(Slug(title), typ+"-"), key)
	b := config.Get("git.branch", "{type}/{key}-{slug}")
	b = strings.NewReplacer("{type}", typ, "{key}", key, "{slug}", s).Replace(b)
	for _, c := range cleanup {
		b = c.re.ReplaceAllString(b, c.rep)
	}
	return b
}

// Worktree builds the worktree path for a branch of the repo at dir, from git.worktree.
func Worktree(branch, dir string) (string, error) {
	top := ghx.RepoRoot(dir)
	if top == "" {
		return "", fmt.Errorf("%s is not in a git repo", dir)
	}
	if c, err := ghx.Run("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && filepath.Base(c) == ".git" {
		top = filepath.Dir(c)
	}
	w := config.Get("git.worktree", "{parent}/worktrees/{repo}-{branch}")
	return strings.NewReplacer("{parent}", filepath.Dir(top), "{repo}", filepath.Base(top), "{branch}", strings.ReplaceAll(branch, "/", "-")).Replace(w), nil
}

// typeWords are the branch types; a key equal to one is a misplaced type.
var typeWords = map[string]bool{"feat": true, "fix": true, "docs": true, "refactor": true, "test": true, "chore": true}

const usage = `usage: smithy names <command>
  branch <key|-> "<title>" [type]   e.g. feat/PROJ-123-add-login, fix/42-null-crash
  worktree <branch> [repo-dir]      <repo-parent>/worktrees/<repo>-<branch with / as ->
  type "<title>"                    feat | fix | docs | refactor | test | chore
Templates: settings git.branch and git.worktree; the repo's agent instructions win over both.`

// Main runs `smithy names`.
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "branch":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		if typeWords[args[1]] {
			fmt.Fprintln(os.Stderr, "key looks like a type; use `-` for no key and pass the type last: smithy names branch - \"<title>\" "+args[1])
			return 2
		}
		typ := ""
		if len(args) > 3 {
			typ = args[3]
		}
		fmt.Println(Branch(args[1], args[2], typ))
	case "worktree":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
		dir := "."
		if len(args) > 2 {
			dir = args[2]
		}
		w, err := Worktree(args[1], dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println(w)
	case "type":
		if len(args) < 2 {
			return 2
		}
		fmt.Println(Type(args[1]))
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	return 0
}
