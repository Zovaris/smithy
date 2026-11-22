package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const commitUsage = `Atomic commits in the repo's own message style.
  smithy commit style                    detected style, rules and recent examples (read before writing messages)
  smithy commit check "<message>"        exit 1 with the reason if the message breaks the style
  smithy commit "<message>" <path>...    commit exactly these paths with this message (after check)
Style: commitlint config > .gitmessage > the last 30 subjects (conventional, gitmoji or plain).
Atomic means one concern per commit: call it once per group of paths, never ` + "`git add -A`" + `.
Never bypasses hooks; a failing hook fails the commit.`

var (
	conventional = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9._/-]+\))?!?: [^ ]`)
	emojiCode    = regexp.MustCompile(`^:[a-z0-9_+-]+:`)
	scopeRe      = regexp.MustCompile(`^[a-z]+\(([a-z0-9._/-]+)\)`)
)

const subjectLimit = 72

func subjects() []string {
	return lines(git("log", "-n", "30", "--no-merges", "--format=%s"))
}

func hasEmoji(s string) bool {
	if emojiCode.MatchString(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s)
	return r > unicode.MaxASCII && !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r)
}

func hasCommitlint(top string) bool {
	for _, p := range []string{"commitlint.config.*", ".commitlintrc*"} {
		if m, _ := filepath.Glob(filepath.Join(top, p)); len(m) > 0 {
			return true
		}
	}
	return false
}

// Style returns conventional, gitmoji or plain for a repo with these recent subjects.
func Style(commitlint bool, subjects []string) string {
	if commitlint || len(subjects) == 0 {
		return "conventional"
	}
	conv, emoji := 0, 0
	for _, s := range subjects {
		if conventional.MatchString(s) {
			conv++
		}
		if hasEmoji(s) {
			emoji++
		}
	}
	switch {
	case conv*2 >= len(subjects):
		return "conventional"
	case emoji*2 >= len(subjects):
		return "gitmoji"
	}
	return "plain"
}

func detect() string {
	return Style(hasCommitlint(git("rev-parse", "--show-toplevel")), subjects())
}

// Check returns why a message breaks the style, or nil when it follows it.
func Check(msg, style string) error {
	subject, _, _ := strings.Cut(msg, "\n")
	if subject == "" {
		return fmt.Errorf("empty message")
	}
	if n := utf8.RuneCountInString(subject); n > subjectLimit {
		return fmt.Errorf("subject is %d chars; keep it ≤ %d", n, subjectLimit)
	}
	if strings.HasSuffix(subject, ".") {
		return fmt.Errorf("subject ends with a period")
	}
	switch style {
	case "conventional":
		if !conventional.MatchString(subject) {
			return fmt.Errorf("not conventional: '<type>(<scope>): <imperative summary>' (types: feat fix docs style refactor perf test build ci chore revert)")
		}
	case "gitmoji":
		if !hasEmoji(subject) {
			return fmt.Errorf("gitmoji style: start with an emoji or :code:")
		}
	}
	return nil
}

// Scopes returns the conventional scopes in these subjects, most used first, at most 8.
func Scopes(subjects []string) []string {
	count := map[string]int{}
	for _, s := range subjects {
		if m := scopeRe.FindStringSubmatch(s); m != nil {
			count[m[1]]++
		}
	}
	out := make([]string, 0, len(count))
	for k := range count {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if count[out[i]] != count[out[j]] {
			return count[out[i]] > count[out[j]]
		}
		return out[i] > out[j]
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// CommitMain runs `smithy commit` and returns the exit code.
func CommitMain(args []string) int {
	const p = "smithy commit"
	if quiet("git", "rev-parse", "--git-dir") != nil {
		return die(p, "not a git repo")
	}
	first := ""
	if len(args) > 0 {
		first = args[0]
	}
	switch first {
	case "style":
		st := detect()
		fmt.Println("style:", st)
		fmt.Printf("subject limit: %d chars (smithy commit rejects longer)\n", subjectLimit)
		switch st {
		case "conventional":
			fmt.Println("format: <type>(<scope>): <imperative summary, lowercase, no period, ≤72>")
		case "gitmoji":
			fmt.Println("format: <emoji> <imperative summary, ≤72>")
		case "plain":
			fmt.Println("format: <Imperative summary, ≤72>, matching the examples below")
		}
		subs := subjects()
		if sc := Scopes(subs); len(sc) > 0 {
			fmt.Println("scopes in use:", strings.Join(sc, " "))
		}
		fmt.Println("examples:")
		for i, s := range subs {
			if i == 5 {
				break
			}
			fmt.Println("  " + s)
		}
		return 0
	case "check":
		if len(args) != 2 {
			die(p, `usage: smithy commit check "<message>"`)
			return 2
		}
		if err := Check(args[1], detect()); err != nil {
			return die(p, "%s", err)
		}
		fmt.Println("ok")
		return 0
	case "", "-h", "--help":
		fmt.Println(commitUsage)
		return 2
	}
	msg, paths := args[0], args[1:]
	if len(paths) == 0 {
		die(p, "name the paths for this commit (atomic: one concern per commit)")
		return 2
	}
	for _, f := range paths {
		// A deleted file git still tracks is a valid path: committing its removal.
		if _, err := os.Lstat(f); err != nil && quiet("git", "ls-files", "--error-unmatch", "--", f) != nil {
			die(p, "no such path: %s", f)
			return 2
		}
	}
	if err := Check(msg, detect()); err != nil {
		return die(p, "%s", err)
	}
	if staged := lines(git("diff", "--cached", "--name-only")); len(staged) > 0 {
		return die(p, "already staged: %s; unstage or include them deliberately", strings.Join(staged, " "))
	}
	if c := attached("git", append([]string{"add", "--"}, paths...)...); c != 0 {
		return c
	}
	if git("diff", "--cached", "--name-only") == "" {
		return die(p, "nothing to commit in: %s", strings.Join(paths, " "))
	}
	if c := attached("git", "commit", "-q", "-m", msg); c != 0 {
		return c
	}
	return attached("git", "log", "-1", "--format=%h %s")
}
