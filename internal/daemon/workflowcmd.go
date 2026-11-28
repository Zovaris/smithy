//go:build harness

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/text"
	"github.com/Zovaris/smithy/internal/workflow"
)

// WorkflowUsage is the text for `smithy workflow --help`.
const WorkflowUsage = `usage: smithy workflow list [--json] | show <name> | run <name> [--input k=v]... | history <name> [--json] [--limit <n>]
                      | enable <name> | disable <name> | add <file> | validate <file|name> | example [<name>] | hook <name>
Workflows the daemon runs: triggers (a cron schedule, by hand, a webhook) and steps that compose:
run, agent, if, switch, set, json, foreach, ask, notify, call. Every step takes when, timeout,
retry and on_error. Files are JSON in ~/.smithy/workflows (docs/workflows.md has the format).
  list      every workflow: enabled, triggers, next and last run
  show      one workflow's steps
  run       run it now, enabled or not (through the daemon when one runs, else here; ask steps
            need the daemon); --input sets one of its inputs
  history   its newest runs with each step's input, output, duration and status
  enable    let it run on its schedule and webhook; pins the file as it is now, so a later change
            outside smithy (or a save from the web editor) stops it until you enable it again
  disable   stop its schedule and webhook
  add       copy a workflow file (a repo's, say) into ~/.smithy/workflows, disabled
  validate  check a file, or a stored workflow, without running it
  example   add the examples, disabled: dependabot-daily, repo-health, webhook-reply (or one)
  hook      print an enabled webhook workflow's URL and token
Steps run unattended: shell commands go through the guard (a blocked command fails the step),
agents run read-only under smithy agent's auto profile (auto.sandbox, failing closed). Each run
stops at max_minutes (30), max_agents (3) and max_cost_usd (1.00) unless the file sets them.`

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
	inputs := map[string]string{}
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
		case a == "--input" && i+1 < len(args):
			i++
			k, v, ok := strings.Cut(args[i], "=")
			if !ok || k == "" {
				fmt.Fprintln(stderr, "smithy workflow: --input takes name=value")
				return 2
			}
			inputs[k] = v
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
			fmt.Fprintln(stdout, "no workflows in "+st.Dir+" (smithy workflow example adds three)")
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
		fmt.Fprintf(stdout, "%s · %s\n", w.Name, triggers(w))
		if w.Description != "" {
			fmt.Fprintln(stdout, w.Description)
		}
		printSteps(stdout, w.Steps, "")
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
			printResults(stdout, r.Steps, "  ")
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
	case "validate":
		if len(rest) != 1 {
			fmt.Fprintln(stderr, "smithy workflow validate: needs a file or a workflow name")
			return 2
		}
		var err error
		if b, rerr := os.ReadFile(rest[0]); rerr == nil {
			_, err = workflow.Parse(b)
		} else {
			_, _, err = st.Get(rest[0])
		}
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "valid")
		return 0
	case "example":
		names := workflow.ExampleNames()
		if len(rest) == 1 {
			if _, ok := workflow.Examples[rest[0]]; !ok {
				return fail(fmt.Errorf("no example %q (%s)", rest[0], strings.Join(names, ", ")))
			}
			names = rest
		}
		code := 0
		for _, n := range names {
			tmp := filepath.Join(os.TempDir(), fmt.Sprintf("smithy-workflow-%s-%d.json", n, os.Getpid()))
			if err := os.WriteFile(tmp, []byte(workflow.Examples[n]), 0o600); err != nil {
				return fail(err)
			}
			w, err := st.Add(tmp)
			os.Remove(tmp)
			if err != nil {
				fmt.Fprintln(stderr, "smithy workflow:", text.Clean(err.Error()))
				code = 1
				continue
			}
			hint := "then smithy workflow enable " + w.Name
			if w.Dir == "" && w.Schedule != "" {
				hint = `set "dir" to a repo checkout in ` + filepath.Join(st.Dir, w.Name+".json") + ", " + hint
			}
			fmt.Fprintf(stdout, "added %s, disabled: %s\n", w.Name, hint)
		}
		return code
	case "hook":
		name, ok := needName()
		if !ok {
			return 2
		}
		w, on, err := st.Load(name)
		if err != nil {
			return fail(err)
		}
		if !w.Webhook || !on {
			return fail(fmt.Errorf("%s needs \"webhook\": true and an enable first", name))
		}
		base := "http://127.0.0.1:<daemon port>"
		if i := Running(); i != nil {
			base = i.URL()
		}
		fmt.Fprintf(stdout, "POST %s/v1/hooks/workflows/%s\nX-Smithy-Hook: %s\nbody: a JSON object of inputs; the response holds the run's status and outputs\n", strings.TrimRight(base, "/"), name, st.HookToken(name))
		return 0
	case "run":
		name, ok := needName()
		if !ok {
			return 2
		}
		if c, err := localClient(); err == nil {
			if err := c.RunWorkflow(context.Background(), name, "cli", inputs); err != nil {
				return fail(err)
			}
			fmt.Fprintf(stdout, "started %s on the daemon; smithy workflow history %s shows it\n", name, name)
			return 0
		}
		sc := &workflow.Scheduler{Store: st, Env: workflow.Env{Shell: workflow.Shell, Agent: workflow.Agent, Guard: workflow.Guard,
			Notify: func(_, title, msg string) error { fmt.Fprintf(stdout, "notify: %s\n%s\n", title, msg); return nil }}}
		r, err := sc.Start(context.Background(), name, workflow.Invocation{Trigger: "cli", Inputs: inputs})
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, runLine(r))
		printResults(stdout, r.Steps, "  ")
		for _, k := range sortedOut(r.Outputs) {
			fmt.Fprintf(stdout, "output %s: %s\n", k, r.Outputs[k])
		}
		if r.Status != workflow.StatusOK {
			return 1
		}
		return 0
	}
	fmt.Fprintf(stderr, "smithy workflow: unknown subcommand %q\n%s\n", args[0], WorkflowUsage)
	return 2
}

func sortedOut(m map[string]string) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

func printSteps(w io.Writer, steps []workflow.Step, indent string) {
	for i, s := range steps {
		fmt.Fprintf(w, "%s%2d. %-7s %-10s %s\n", indent, i+1, s.Kind, s.ID, s.Summary())
		printSteps(w, s.Do, indent+"    ")
	}
}

func printResults(w io.Writer, rs []workflow.StepResult, indent string) {
	for _, s := range rs {
		fmt.Fprintf(w, "%s%-12s %-7s %6s  %s\n", indent, s.ID, s.Status, (time.Duration(s.DurationMS) * time.Millisecond).Round(time.Millisecond), s.Line())
		printResults(w, s.Steps, indent+"  ")
	}
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

func triggers(w workflow.Workflow) string {
	t := []string{"manual"}
	if w.Schedule != "" {
		t = append(t, w.Schedule)
	}
	if w.Webhook {
		t = append(t, "webhook")
	}
	return strings.Join(t, " · ")
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
	line := fmt.Sprintf("%-24s %-7s %-22s", in.Name, state, triggers(in.Workflow))
	if in.Next != nil {
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

func runLine(r workflow.Record) string {
	line := fmt.Sprintf("%s  %-7s %s %s", r.Started.Local().Format("2006-01-02 15:04"), r.Status, r.Trigger, r.Ended.Sub(r.Started).Round(time.Second))
	if r.CostUSD > 0 {
		line += fmt.Sprintf(" $%.2f", r.CostUSD)
	}
	if r.Error != "" {
		line += " · " + text.Clean(r.Error)
	}
	return line
}
