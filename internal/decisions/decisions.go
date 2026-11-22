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

	"github.com/Zovaris/smithy/internal/ghx"
)

// Usage is the text for `smithy decisions --help`.
const Usage = `usage: smithy decisions add "<question>" "<choice>" --why "<reason>" [--by consult|timeout] [--ref <pr|file>]
       smithy decisions pending | list
       smithy decisions confirm <n>|--all
       smithy decisions change <n> "<new choice>"
The log of the user's decisions taken without them, one line each, in
~/.smithy/decisions/<repo>.md (the main checkout's folder name; shared by its worktrees).
add appends one entry; pending lists the unconfirmed ones, list all, numbered; confirm marks
them kept; change replaces the choice and marks it confirmed. Exit 0 = ok, 2 = bad usage.`

const (
	open = "- [ ] "
	done = "- [x] "
)

// repoSlug names the log: the folder of the main checkout, so worktrees share one log.
var repoSlug = func() string {
	out, err := ghx.Run("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || strings.TrimSpace(out) == "" {
		return "no-repo"
	}
	return filepath.Base(filepath.Dir(strings.TrimSpace(out)))
}

// File is the decisions log of the current repo.
func File() string { return filepath.Join(ghx.Home(), "decisions", repoSlug()+".md") }

func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

type entry struct {
	line int
	text string
}

func read() ([]string, []entry, error) {
	b, err := os.ReadFile(File())
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	var es []entry
	for i, l := range lines {
		if strings.HasPrefix(l, open) || strings.HasPrefix(l, done) {
			es = append(es, entry{i, l})
		}
	}
	return lines, es, nil
}

func write(lines []string) error {
	return os.WriteFile(File(), []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func add(q, choice, why, by, ref string) error {
	if by != "consult" && by != "timeout" {
		return fmt.Errorf("--by must be consult or timeout, got %q", by)
	}
	l := fmt.Sprintf("%s%s · by %s · %s → %s · why: %s", open, time.Now().Format("2006-01-02"), by, flat(q), flat(choice), flat(why))
	if ref != "" {
		l += " · ref: " + flat(ref)
	}
	if err := os.MkdirAll(filepath.Dir(File()), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(File(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(l + "\n")
	return err
}

func show(es []entry, onlyOpen bool) {
	n := 0
	for i, e := range es {
		if onlyOpen && !strings.HasPrefix(e.text, open) {
			continue
		}
		n++
		fmt.Printf("%d. %s\n", i+1, e.text[len(open):])
		if !strings.HasPrefix(e.text, open) {
			fmt.Printf("   (confirmed)\n")
		}
	}
	if n == 0 && onlyOpen {
		fmt.Println("none pending")
	}
}

// change swaps the choice in an entry: the text between the first " → " and the last " · why: ".
func change(text, choice string) (string, error) {
	a := strings.Index(text, " → ")
	b := strings.LastIndex(text, " · why: ")
	if a < 0 || b < a {
		return "", fmt.Errorf("cannot find the choice in %q", text)
	}
	return done + text[len(open):a] + " → " + flat(choice) + " (changed from: " + text[a+len(" → "):b] + ")" + text[b:], nil
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
	if len(pos) == 0 {
		return bad("missing subcommand")
	}
	if pos[0] == "add" {
		if len(pos) != 3 || strings.TrimSpace(pos[1]) == "" || strings.TrimSpace(pos[2]) == "" || strings.TrimSpace(why) == "" {
			return bad(`add needs "<question>" "<choice>" --why "<reason>"`)
		}
		if err := add(pos[1], pos[2], why, by, ref); err != nil {
			return bad(err.Error())
		}
		fmt.Println("recorded in", File())
		return 0
	}
	lines, es, err := read()
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy decisions:", err)
		return 1
	}
	pick := func(s string) (int, bool) {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > len(es) {
			return 0, false
		}
		return n - 1, true
	}
	switch pos[0] {
	case "pending", "list":
		if len(pos) != 1 {
			return bad("unexpected argument " + pos[1])
		}
		show(es, pos[0] == "pending")
		return 0
	case "confirm":
		var idx []int
		switch {
		case all && len(pos) == 1:
			for i := range es {
				idx = append(idx, i)
			}
		case !all && len(pos) == 2:
			i, ok := pick(pos[1])
			if !ok {
				return bad("no decision " + pos[1])
			}
			idx = []int{i}
		default:
			return bad("confirm takes <n> or --all")
		}
		for _, i := range idx {
			lines[es[i].line] = done + strings.TrimPrefix(strings.TrimPrefix(es[i].text, open), done)
		}
		if err := write(lines); err != nil {
			fmt.Fprintln(os.Stderr, "smithy decisions:", err)
			return 1
		}
		fmt.Printf("confirmed %d\n", len(idx))
		return 0
	case "change":
		if len(pos) != 3 || strings.TrimSpace(pos[2]) == "" {
			return bad(`change takes <n> "<new choice>"`)
		}
		i, ok := pick(pos[1])
		if !ok {
			return bad("no decision " + pos[1])
		}
		text := open + strings.TrimPrefix(strings.TrimPrefix(es[i].text, open), done)
		nl, err := change(text, pos[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "smithy decisions:", err)
			return 1
		}
		lines[es[i].line] = nl
		if err := write(lines); err != nil {
			fmt.Fprintln(os.Stderr, "smithy decisions:", err)
			return 1
		}
		fmt.Printf("changed %d\n", i+1)
		return 0
	}
	return bad("unknown subcommand " + pos[0])
}
