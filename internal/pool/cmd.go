package pool

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/usage"
)

// Row is one account in `smithy pool list` and the TUI's pool table.
type Row struct {
	Name, Harness, Dir, Status, Resets string
	Runs                               int
	Cost                               *float64
}

// Rows lists every account with its status at now and today's runs and reported cost (from the
// usage events that name the account).
func Rows(now time.Time) ([]Row, error) {
	as, err := Load()
	if err != nil || len(as) == 0 {
		return nil, err
	}
	st := State()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	es, _ := events.Read(day, 0)
	var out []Row
	for _, a := range as {
		r := Row{Name: a.Name, Harness: a.Harness, Dir: a.Dir, Status: "ok", Resets: "-"}
		if u := st[a.Name].LimitedUntil; u.After(now) {
			r.Status, r.Resets = "limited", u.In(now.Location()).Format("01-02 15:04")
		}
		for _, e := range es {
			if e.Kind == "usage" && e.Account == a.Name {
				r.Runs++
				if e.Usage != nil && e.Usage.CostUSD != nil {
					c := *e.Usage.CostUSD
					if r.Cost != nil {
						c += *r.Cost
					}
					r.Cost = &c
				}
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// loginArgs is the interactive command that signs an account in.
func loginArgs(harness string) []string {
	if harness == "codex" {
		return []string{"codex", "login"}
	}
	return []string{"claude", "/login"}
}

// Main runs `smithy pool` and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, Usage)
		return 2
	}
	need := map[string][2]int{"add": {2, 3}, "remove": {1, 1}, "login": {1, 1}, "clear": {1, 1}, "list": {0, 0}}
	n, ok := need[args[0]]
	if !ok || len(args)-1 < n[0] || len(args)-1 > n[1] {
		fmt.Fprintln(stderr, Usage)
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "smithy pool:", err); return 1 }
	switch args[0] {
	case "add":
		dir := ""
		if len(args) == 4 {
			dir = args[3]
		}
		a, err := Add(args[1], args[2], dir)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "added %s (%s, %s)\nsign in: smithy pool login %s\n", a.Name, a.Harness, a.Dir, a.Name)
	case "remove":
		if err := Remove(args[1]); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "removed %s (its dir stays on disk)\n", args[1])
	case "clear":
		if _, err := Find(args[1]); err != nil {
			return fail(err)
		}
		if err := Clear(args[1]); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "cleared %s's limit\n", args[1])
	case "login":
		a, err := Find(args[1])
		if err != nil {
			return fail(err)
		}
		if err := MakeDir(a.Dir); err != nil {
			return fail(err)
		}
		argv := loginArgs(a.Harness)
		fmt.Fprintf(stdout, "running %s with %s; sign in, then exit\n", argv[0], Harnesses[a.Harness])
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Env = append(os.Environ(), a.Env())
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
		if err := cmd.Run(); err != nil {
			return fail(err)
		}
	case "list":
		rows, err := Rows(time.Now())
		if err != nil {
			return fail(err)
		}
		if len(rows) == 0 {
			fmt.Fprintln(stdout, "no accounts (smithy pool add <name> claude|codex)")
			return 0
		}
		fmt.Fprintf(stdout, "%-12s %-7s %-8s %-12s %5s %8s  %s\n", "ACCOUNT", "HARNESS", "STATUS", "RESETS", "RUNS", "COST", "DIR")
		for _, r := range rows {
			fmt.Fprintf(stdout, "%-12s %-7s %-8s %-12s %5d %8s  %s\n", r.Name, r.Harness, r.Status, r.Resets, r.Runs, usage.Cost(r.Cost), r.Dir)
		}
	}
	return 0
}
