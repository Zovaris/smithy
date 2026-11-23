// Package decisions keeps the log of the user's decisions an agent took without them (no answer
// within ask.timeout, or nobody to ask), so the user can keep or change each one on return.
package decisions

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
)

// Usage is the text for `smithy decisions --help`.
const Usage = `usage: smithy decisions add "<question>" "<choice>" --why "<reason>" [--by consult|timeout] [--ref <pr|file>]
       smithy decisions pending | list
       smithy decisions confirm <n>|--all
       smithy decisions change <n> "<new choice>"
The log of the user's decisions taken without them, one line each, in
~/.smithy/decisions/<repo>.md (the main checkout's folder name; shared by its worktrees).
add appends one entry; pending lists the unconfirmed ones; confirm marks them kept; change
replaces the choice and marks it confirmed. Numbers are positions in the pending list only (as pending prints
them); list shows every entry, unnumbered. Fields can't contain " · " or " → ".
Exit 0 = ok, 1 = cannot read or write the log, 2 = bad usage.`

const (
	open = "- [ ] "
	done = "- [x] "
	sep  = " · "
	arw  = " → "
)

// repoSlug names the log: origin's owner-repo, else the folder of the main checkout, so worktrees
// share one log and two checkouts named alike don't.
var repoSlug = func() string {
	out, err := ghx.Run("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || strings.TrimSpace(out) == "" {
		return "no-repo"
	}
	url, _ := ghx.Run("git", "remote", "get-url", "origin")
	return slug(strings.TrimSpace(url), filepath.Base(filepath.Dir(strings.TrimSpace(out))))
}

func slug(url, dir string) string {
	u := strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")
	parts := strings.FieldsFunc(u, func(r rune) bool { return r == '/' || r == ':' })
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "-" + parts[len(parts)-1]
	}
	return dir
}

// File is the decisions log of the current repo.
func File() string { return filepath.Join(ghx.Home(), "decisions", repoSlug()+".md") }

func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

// clean flattens a field and rejects the separators the line format parses by position.
func clean(name, s string) (string, error) {
	s = flat(s)
	if strings.Contains(s, strings.TrimSpace(sep)) || strings.Contains(s, strings.TrimSpace(arw)) {
		return "", fmt.Errorf("%s can't contain %q or %q: %q", name, "·", "→", s)
	}
	return s, nil
}

func read() ([]string, error) {
	b, err := os.ReadFile(File())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n"), nil
}

// write replaces the log through a temp file and a rename, so a reader never sees half a file.
func write(lines []string) error {
	tmp, err := os.CreateTemp(filepath.Dir(File()), ".decisions-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), File())
}

// update runs f on the log's lines under the log's lock and writes the result.
func update(f func([]string) ([]string, error)) error {
	if err := os.MkdirAll(filepath.Dir(File()), 0o700); err != nil {
		return err
	}
	return config.WithLock(File(), func() error {
		lines, err := read()
		if err != nil {
			return err
		}
		if lines, err = f(lines); err != nil {
			return err
		}
		return write(lines)
	})
}

// pending is the line index of each unconfirmed entry, in order: pending number n is pending[n-1].
func pending(lines []string) []int {
	var p []int
	for i, l := range lines {
		if strings.HasPrefix(l, open) {
			p = append(p, i)
		}
	}
	return p
}

// change swaps the choice: fields are date, by, "question → choice", why[, ref].
func change(line, choice string) (string, error) {
	f := strings.Split(strings.TrimPrefix(line, open), sep)
	if len(f) < 4 {
		return "", fmt.Errorf("malformed entry %q", line)
	}
	qc := strings.Split(f[2], arw)
	if len(qc) != 2 {
		return "", fmt.Errorf("malformed entry %q", line)
	}
	f[2] = qc[0] + arw + choice + " (changed from: " + qc[1] + ")"
	return done + strings.Join(f, sep), nil
}

// Main runs `smithy decisions`.
func Main(args []string) int {
	var pos []string
	why, by, ref, all := "", "consult", "", false
	for i := 0; i < len(args); i++ {
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch a := args[i]; a {
		case "-h", "--help":
			fmt.Println(Usage)
			return 0
		case "--why":
			why = next()
		case "--by":
			by = next()
		case "--ref":
			ref = next()
		case "--all":
			all = true
		default:
			if strings.HasPrefix(a, "--") {
				fmt.Fprintf(os.Stderr, "smithy decisions: unknown option %s\n", a)
				return 2
			}
			pos = append(pos, a)
		}
	}
	bad := func(msg string) int {
		fmt.Fprintln(os.Stderr, "smithy decisions:", msg)
		fmt.Fprintln(os.Stderr, Usage)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(os.Stderr, "smithy decisions:", err)
		return 1
	}
	if len(pos) == 0 {
		return bad("missing subcommand")
	}
	switch pos[0] {
	case "add":
		if len(pos) != 3 {
			return bad(`add needs "<question>" "<choice>" --why "<reason>"`)
		}
		if by != "consult" && by != "timeout" {
			return bad(fmt.Sprintf("--by must be consult or timeout, got %q", by))
		}
		fields := []string{time.Now().Format("2006-01-02"), "by " + by}
		var vals [4]string
		for i, kv := range [][2]string{{"question", pos[1]}, {"choice", pos[2]}, {"--why", why}, {"--ref", ref}} {
			v, err := clean(kv[0], kv[1])
			if err != nil {
				return bad(err.Error())
			}
			if v == "" && i < 3 {
				return bad(kv[0] + " is empty")
			}
			vals[i] = v
		}
		fields = append(fields, vals[0]+arw+vals[1], "why: "+vals[2])
		if vals[3] != "" {
			fields = append(fields, "ref: "+vals[3])
		}
		err := update(func(l []string) ([]string, error) { return append(l, open+strings.Join(fields, sep)), nil })
		if err != nil {
			return fail(err)
		}
		fmt.Println("recorded in", File())
		return 0
	case "pending", "list":
		if len(pos) != 1 {
			return bad("unexpected argument " + pos[1])
		}
		lines, err := read()
		if err != nil {
			return fail(err)
		}
		if pos[0] == "list" {
			for _, l := range lines {
				if strings.HasPrefix(l, open) || strings.HasPrefix(l, done) {
					fmt.Println(l)
				}
			}
			return 0
		}
		p := pending(lines)
		if len(p) == 0 {
			fmt.Println("none pending")
		}
		for n, i := range p {
			fmt.Printf("%d. %s\n", n+1, strings.TrimPrefix(lines[i], open))
		}
		return 0
	case "confirm", "change":
		if pos[0] == "confirm" && !(all && len(pos) == 1) && !(!all && len(pos) == 2) {
			return bad("confirm takes <n> or --all")
		}
		var choice string
		if pos[0] == "change" {
			if len(pos) != 3 || all {
				return bad(`change takes <n> "<new choice>"`)
			}
			c, err := clean("choice", pos[2])
			if err != nil || c == "" {
				return bad(fmt.Sprintf("bad choice %q", pos[2]))
			}
			choice = c
		}
		count := 0
		err := update(func(lines []string) ([]string, error) {
			p := pending(lines)
			pick := p
			if !all {
				n, err := strconv.Atoi(pos[1])
				if err != nil || n < 1 || n > len(p) {
					return nil, fmt.Errorf("no pending decision %s (smithy decisions pending numbers them)", pos[1])
				}
				pick = []int{p[n-1]}
			}
			for _, i := range pick {
				if choice == "" {
					lines[i] = done + strings.TrimPrefix(lines[i], open)
				} else {
					l, err := change(lines[i], choice)
					if err != nil {
						return nil, err
					}
					lines[i] = l
				}
			}
			count = len(pick)
			return lines, nil
		})
		if err != nil {
			return fail(err)
		}
		fmt.Printf("%sed %d\n", strings.TrimSuffix(pos[0], "e"), count)
		return 0
	}
	return bad("unknown subcommand " + pos[0])
}
