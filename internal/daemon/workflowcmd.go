//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/text"
	"github.com/Zovaris/smithy/internal/workflow"
)

// WorkflowUsage is the text for `smithy workflow --help`.
const WorkflowUsage = `usage: smithy workflow list [--json] | show <name> | run <name> | history <name> [--json] [--limit <n>]
                      | enable <name> | disable <name> | add <file> | example
Scheduled workflows the daemon runs: a cron schedule plus ordered steps (run a shell command, ask
an agent, branch on a result, notify you). Files are JSON in ~/.smithy/workflows.
  list      every workflow: enabled, schedule, next and last run
  show      one workflow's steps
  run       run it now, enabled or not (through the daemon when one runs, else here)
  history   its newest runs with each step's output
  enable    let it run on its schedule; pins the file as it is now, so a later change made
            outside smithy stops it until you enable it again
  disable   stop its schedule
  add       copy a workflow file (a repo's, say) into ~/.smithy/workflows, disabled
  example   add the example: daily 17:00 Dependabot alerts, an agent's merge yes/no and why
Steps run unattended: shell commands go through the guard (a blocked command fails the step),
agents run read-only under smithy agent's auto profile (auto.sandbox, failing closed). Each
run stops at max_minutes (30), max_agents (3) and max_cost_usd (1.00) unless the file sets them.`

// WorkflowMain runs `smithy workflow`.
func WorkflowMain(args []string, stdout, stderr io.Writer) int {
	return workflowMain(workflow.Store{Dir: workflow.Home()}, args, stdout, stderr)
}

func workflowMain(st workflow.Store, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stdout, WorkflowUsage)
		return 0
	}
	asJSON, limit := false, 10
	var rest []string
	for i := 1; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			asJSON = true
		case a == "--limit" && i+1 < len(args):
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				fmt.Fprintln(stderr, "smithy workflow: --limit takes a positive number")
				return 2
			}
			limit = n
		default:
			rest = append(rest, a)
		}
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "smithy workflow:", text.Clean(err.Error()))
		return 1
	}
	needName := func() (string, bool) {
		if len(rest) != 1 {
			fmt.Fprintf(stderr, "smithy workflow %s: needs one workflow name\n", args[0])
			return "", false
		}
		return rest[0], true
	}
	switch args[0] {
	case "list":
		l, err := st.List()
		if err != nil {
			return fail(err)
		}
		if asJSON {
			return printJSON(stdout, l)
		}
		if len(l) == 0 {
			fmt.Fprintln(stdout, "no workflows in "+st.Dir+" (smithy workflow example adds one)")
		}
		for _, in := range l {
			fmt.Fprintln(stdout, workflowLine(in))
		}
		return 0
	case "show":
		name, ok := needName()
		if !ok {
			return 2
		}
		w, _, err := st.Get(name)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s · %s\n", w.Name, w.Schedule)
		if w.Description != "" {
			fmt.Fprintln(stdout, w.Description)
		}
		for i, s := range w.Steps {
			fmt.Fprintf(stdout, "%2d. %-6s %-10s %s\n", i+1, s.Kind, s.ID, stepSummary(s))
		}
		return 0
	case "history":
		name, ok := needName()
		if !ok {
			return 2
		}
		h, err := st.History(name, limit)
		if err != nil {
			return fail(err)
		}
		if asJSON {
			return printJSON(stdout, h)
		}
		if len(h) == 0 {
			fmt.Fprintln(stdout, "no runs yet")
		}
		for _, r := range h {
			fmt.Fprintln(stdout, runLine(r))
			for _, s := range r.Steps {
				fmt.Fprintf(stdout, "  %-10s %s\n", s.ID, stepResultLine(s))
			}
		}
		return 0
	case "enable", "disable":
		name, ok := needName()
		if !ok {
			return 2
		}
		var err error
		if args[0] == "enable" {
			err = st.Enable(name, time.Now())
		} else {
			err = st.Disable(name)
		}
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%sd %s\n", args[0], name)
		return 0
	case "add":
		if len(rest) != 1 {
			fmt.Fprintln(stderr, "smithy workflow add: needs one file")
			return 2
		}
		w, err := st.Add(rest[0])
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "added %s (disabled; smithy workflow enable %s)\n", w.Name, w.Name)
		return 0
	case "example":
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("smithy-workflow-example-%d.json", os.Getpid()))
		if err := os.WriteFile(tmp, []byte(workflow.Example), 0o600); err != nil {
			return fail(err)
		}
		defer os.Remove(tmp)
		w, err := st.Add(tmp)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "added %s, disabled: set \"dir\" to a repo checkout in %s, then smithy workflow enable %s\n",
			w.Name, filepath.Join(st.Dir, w.Name+".json"), w.Name)
		return 0
	case "run":
		name, ok := needName()
		if !ok {
			return 2
		}
		if c, err := localClient(); err == nil {
			if err := c.RunWorkflow(context.Background(), name, "cli"); err != nil {
				return fail(err)
			}
			fmt.Fprintf(stdout, "started %s on the daemon; smithy workflow history %s shows it\n", name, name)
			return 0
		}
		sc := &workflow.Scheduler{Store: st, Env: workflow.Env{Shell: workflow.Shell, Agent: workflow.Agent, Guard: workflow.Guard,
			Notify: func(_, title, msg string) error { fmt.Fprintf(stdout, "notify: %s\n%s\n", title, msg); return nil }}}
		r, err := sc.RunNow(context.Background(), name, "cli")
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, runLine(r))
		if r.Status != workflow.StatusOK {
			return 1
		}
		return 0
	}
	fmt.Fprintf(stderr, "smithy workflow: unknown subcommand %q\n%s\n", args[0], WorkflowUsage)
	return 2
}

func localClient() (*Client, error) {
	a, err := LocalAsker()
	if err != nil {
		return nil, err
	}
	return a.C, nil
}

func printJSON(w io.Writer, v any) int {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	_ = e.Encode(v)
	return 0
}

func workflowLine(in workflow.Info) string {
	state := "off"
	switch {
	case in.Err != "":
		state = "error"
	case in.Changed:
		state = "changed"
	case in.Enabled:
		state = "on"
	}
	line := fmt.Sprintf("%-24s %-7s %-14s", in.Name, state, in.Schedule)
	if !in.Next.IsZero() {
		line += " next " + in.Next.Format("Mon 15:04")
	}
	if in.Last != nil {
		line += " · last " + in.Last.Status + " " + text.Ago(time.Since(in.Last.Started))
	}
	if in.Err != "" {
		line += " · " + in.Err
	}
	if in.Changed {
		line += " · file changed since enable; enable again to run it"
	}
	return line
}

func stepSummary(s workflow.Step) string {
	switch s.Kind {
	case workflow.KindRun:
		return oneLineOf(s.Run, 70)
	case workflow.KindAgent:
		a := ""
		if s.Answer != "" {
			a = " (" + s.Answer + ")"
		}
		return oneLineOf(s.Prompt, 60) + a
	case workflow.KindIf:
		test := ""
		switch {
		case s.Exit != nil:
			test = fmt.Sprintf("exit == %d", *s.Exit)
		case s.Match != "":
			test = "output ~ /" + s.Match + "/"
		default:
			test = "answer == " + s.Equals
		}
		return fmt.Sprintf("%s %s → %s, else %s", s.Step, test, orNext(s.Then), orNext(s.Else))
	}
	return oneLineOf(s.Title+": "+s.Message, 70)
}

func orNext(s string) string {
	if s == "" {
		return "next"
	}
	return s
}

func oneLineOf(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}

func runLine(r workflow.Record) string {
	line := fmt.Sprintf("%s  %-7s %s %s", r.Started.Local().Format("2006-01-02 15:04"), r.Status, r.Trigger, r.Ended.Sub(r.Started).Round(time.Second))
	if r.CostUSD > 0 {
		line += fmt.Sprintf(" $%.2f", r.CostUSD)
	}
	if r.Error != "" {
		line += " · " + oneLineOf(r.Error, 120)
	}
	return line
}

func stepResultLine(s workflow.StepResult) string {
	switch {
	case s.Error != "":
		return "error: " + oneLineOf(s.Error, 100)
	case s.Kind == workflow.KindIf:
		return "→ " + s.Branch
	case s.Answer != "":
		return s.Answer + " " + oneLineOf(s.Why, 90)
	case s.Kind == workflow.KindRun:
		return fmt.Sprintf("exit %d · %s", s.Exit, oneLineOf(s.Output, 90))
	}
	return oneLineOf(s.Output, 100)
}
