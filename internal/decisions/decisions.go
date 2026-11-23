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
	"github.com/Zovaris/smithy/internal/text"
)

// Usage is the text for `smithy decisions --help`.
const Usage = `usage: smithy decisions add "<question>" "<choice>" --why "<reason>" [--by self|consult|timeout, default self] [--ref <pr|file>]
       smithy decisions pending | list [--ref <prefix>]
       smithy decisions confirm <n>|--all
       smithy decisions change <n> "<new choice>"
       smithy decisions reject <n> "<why>"
The log of the user's decisions taken without them, one line each, in
~/.smithy/decisions/<owner>-<repo>.md (from origin's URL; the main checkout's folder name when there
is no origin; shared by its worktrees).
add appends one entry; pending lists the unconfirmed ones; confirm marks them kept; change
replaces the choice and marks it confirmed; reject keeps the entry pending with a
"rejected: <why>" field, so the agent sees it in pending and takes the decision again. Numbers are positions in the pending list only (as pending prints
them); list shows every entry, unnumbered (--ref <prefix> keeps those whose ref starts with it). Fields can't contain " · " or " → ".
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

func flat(s string) string { return text.Clean(s) }

// clean flattens a field and rejects the separators the line format parses by position.
func clean(name, s string) (string, error) {
	s = flat(s)
	if strings.Contains(s, strings.TrimSpace(sep)) || strings.Contains(s, strings.TrimSpace(arw)) {
		return "", fmt.Errorf("%s can't contain %q or %q: %q", name, "·", "→", s)
	}
	return s, nil
}

func read() ([]string, error) { return readFile(File()) }

func readFile(file string) ([]string, error) {
	b, err := os.ReadFile(file)
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

// refMatches reports whether an entry's ref field starts with prefix (every entry when prefix is empty).
func refMatches(line, prefix string) bool {
	if prefix == "" {
		return true
	}
	f := strings.Split(line, sep)
	last := f[len(f)-1]
	r, ok := strings.CutPrefix(last, "ref: ")
	return ok && strings.HasPrefix(r, prefix)
}

// Main runs `smithy decisions`.
func Main(args []string) int {
	var pos []string
	why, by, ref, all := "", "self", "", false
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
		if by != "consult" && by != "timeout" && by != "self" {
			return bad(fmt.Sprintf("--by must be consult, timeout or self, got %q", by))
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
		if ref != "" && pos[0] != "list" {
			return bad("--ref filters list only")
		}
		lines, err := read()
		if err != nil {
			return fail(err)
		}
		if pos[0] == "list" {
			for _, l := range lines {
				if (strings.HasPrefix(l, open) || strings.HasPrefix(l, done)) && refMatches(l, ref) {
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
	case "reject":
		if len(pos) != 3 || all {
			return bad(`reject takes <n> "<why>"`)
		}
		n, err := strconv.Atoi(pos[1])
		if err != nil {
			return bad("bad number " + pos[1])
		}
		if err := Reject(n, pos[2]); err != nil {
			if strings.Contains(err.Error(), "can't contain") || strings.Contains(err.Error(), "is empty") {
				return bad(err.Error())
			}
			return fail(err)
		}
		fmt.Println("rejected 1; the agent sees it in smithy decisions pending")
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

// Pending is the unconfirmed entries of the current repo's log, without the checkbox prefix.
func Pending() ([]string, error) { return PendingIn(File()) }

// PendingIn is Pending for a given log file, so a caller can resolve File() once.
func PendingIn(file string) ([]string, error) {
	lines, err := readFile(file)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, i := range pending(lines) {
		out = append(out, strings.TrimPrefix(lines[i], open))
	}
	return out, nil
}

// pick finds pending decision n (1-based, as pending numbers them).
func pick(lines []string, n int) (int, error) {
	p := pending(lines)
	if n < 1 || n > len(p) {
		return 0, fmt.Errorf("no pending decision %d (smithy decisions pending numbers them)", n)
	}
	return p[n-1], nil
}

// Confirm marks pending decision n as kept.
func Confirm(n int) error {
	return update(func(lines []string) ([]string, error) {
		i, err := pick(lines, n)
		if err != nil {
			return nil, err
		}
		lines[i] = done + strings.TrimPrefix(lines[i], open)
		return lines, nil
	})
}

// Reject keeps pending decision n open with a "rejected: <why>" field (replacing an earlier one),
// so the agent that took it sees the rejection in pending and decides again.
func Reject(n int, why string) error {
	w, err := clean("why", why)
	if err != nil {
		return err
	}
	if w == "" {
		return fmt.Errorf("why is empty")
	}
	return update(func(lines []string) ([]string, error) {
		i, err := pick(lines, n)
		if err != nil {
			return nil, err
		}
		var keep []string
		for _, f := range strings.Split(lines[i], sep) {
			if !strings.HasPrefix(f, "rejected: ") {
				keep = append(keep, f)
			}
		}
		lines[i] = strings.Join(append(keep, "rejected: "+w), sep)
		return lines, nil
	})
}
