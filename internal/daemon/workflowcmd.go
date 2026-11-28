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
                      | add <file> | validate <file|name> | example [<name>]
A workflow is a flow of steps you run: run, agent, if, switch, set, json, foreach, ask, notify,
call, each with when, timeout, retry and on_error. Files are JSON in ~/.smithy/workflows
(docs/workflows.md). Schedules start workflows on their own: smithy schedule.
  list      every workflow, its schedules and last run
  show      one workflow's inputs and steps
  run       run it now (through the daemon when one runs, else here; ask steps need the daemon);
            --input sets one of its inputs
  history   its newest runs with each step's input, output, duration and status
  add       copy a workflow or schedule file (a repo's, say) into ~/.smithy/workflows; a schedule
            lands disabled
  validate  check a file, or a stored workflow, without running it
  example   add the examples and their schedules, disabled (or one by name)
Steps run with nobody watching: shell commands go through the guard (a blocked command fails the
step), agents run read-only under smithy agent's auto profile (auto.sandbox, failing closed). Each
run stops at max_minutes (30), max_agents (3) and max_cost_usd (1.00) unless the file sets them.`

// ScheduleUsage is the text for `smithy schedule --help`.
const ScheduleUsage = `usage: smithy schedule list [--json] | show <name> | add <name> --workflow <w> (--cron "<spec>" | --webhook | --github <event> [--label <l>] | --watch <path>) [--input k=v]...
                      | enable <name> | disable <name> | remove <name> | history <name> [--limit <n>] | hook <name>
A schedule starts a workflow on a trigger: a cron line, a webhook, a GitHub event
(dependabot_alert, pr_red, issue_labeled with --label) or a change under a file or folder. The
daemon fires enabled schedules; files are JSON in ~/.smithy/workflows/schedules.
  add       write a new schedule, disabled
  enable    let it fire; pins the schedule file and its workflow (and every workflow it calls)
            as they are now, so a later change stops it until you enable it again
  disable   stop it          remove   disable and delete it
  history   the runs it started
  hook      an enabled webhook schedule's URL and token`

// WorkflowMain runs `smithy workflow`.
func WorkflowMain(args []string, stdout, stderr io.Writer) int {
	return workflowMain(defaultStore(), args, stdout, stderr)
}

// ScheduleMain runs `smithy schedule`.
func ScheduleMain(args []string, stdout, stderr io.Writer) int {
	return scheduleMain(defaultStore(), args, stdout, stderr)
}

func defaultStore() workflow.Store {
	st := workflow.Store{Dir: workflow.Home()}
	st.Migrate(time.Now())
	return st
}

type cliFlags struct {
	json   bool
	limit  int
	inputs map[string]string
	vals   map[string]string
	bools  map[string]bool
	rest   []string
}

func parseCLI(args []string, valued, boolean []string, stderr io.Writer, cmd string) (cliFlags, bool) {
	f := cliFlags{limit: 10, inputs: map[string]string{}, vals: map[string]string{}, bools: map[string]bool{}}
	is := func(l []string, a string) bool {
		for _, x := range l {
			if "--"+x == a {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			f.json = true
		case a == "--limit" && i+1 < len(args):
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				fmt.Fprintf(stderr, "smithy %s: --limit takes a positive number\n", cmd)
				return f, false
			}
			f.limit = n
		case a == "--input" && i+1 < len(args):
			i++
			k, v, ok := strings.Cut(args[i], "=")
			if !ok || k == "" {
				fmt.Fprintf(stderr, "smithy %s: --input takes name=value\n", cmd)
				return f, false
			}
			f.inputs[k] = v
		case is(valued, a) && i+1 < len(args):
			i++
			f.vals[strings.TrimPrefix(a, "--")] = args[i]
		case is(boolean, a):
			f.bools[strings.TrimPrefix(a, "--")] = true
		default:
			f.rest = append(f.rest, a)
		}
	}
	return f, true
}

func cliFail(stderr io.Writer, cmd string, err error) int {
	fmt.Fprintf(stderr, "smithy %s: %s\n", cmd, text.Clean(err.Error()))
	return 1
}

func oneName(f cliFlags, stderr io.Writer, cmd, sub string) (string, bool) {
	if len(f.rest) != 1 {
		fmt.Fprintf(stderr, "smithy %s %s: needs one name\n", cmd, sub)
		return "", false
	}
	return f.rest[0], true
}

func workflowMain(st workflow.Store, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stdout, WorkflowUsage)
		return 0
	}
	f, ok := parseCLI(args[1:], nil, nil, stderr, "workflow")
	if !ok {
		return 2
	}
	fail := func(err error) int { return cliFail(stderr, "workflow", err) }
	switch args[0] {
	case "list":
		l, _ := st.List()
		if f.json {
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
		name, ok := oneName(f, stderr, "workflow", "show")
		if !ok {
			return 2
		}
		w, _, err := st.Get(name)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, w.Name)
		if w.Description != "" {
			fmt.Fprintln(stdout, w.Description)
		}
		for _, k := range sortedOut(w.Inputs) {
			fmt.Fprintf(stdout, "input %s (default %q)\n", k, w.Inputs[k])
		}
		printSteps(stdout, w.Steps, "")
		return 0
	case "history":
		name, ok := oneName(f, stderr, "workflow", "history")
		if !ok {
			return 2
		}
		h, err := st.History(name, f.limit)
		if err != nil {
			return fail(err)
		}
		return printRuns(stdout, h, f.json)
	case "add":
		if len(f.rest) != 1 {
			fmt.Fprintln(stderr, "smithy workflow add: needs one file")
			return 2
		}
		what, err := st.Add(f.rest[0])
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "added %s\n", what)
		return 0
	case "validate":
		if len(f.rest) != 1 {
			fmt.Fprintln(stderr, "smithy workflow validate: needs a file or a workflow name")
			return 2
		}
		var err error
		if b, rerr := os.ReadFile(f.rest[0]); rerr == nil {
			_, err = workflow.Parse(b)
		} else {
			_, _, err = st.Get(f.rest[0])
		}
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "valid")
		return 0
	case "example":
		return addExamples(st, f.rest, stdout, stderr)
	case "run":
		name, ok := oneName(f, stderr, "workflow", "run")
		if !ok {
			return 2
		}
		if c, err := localClient(); err == nil {
			if err := c.RunWorkflow(context.Background(), name, "cli", f.inputs); err != nil {
				return fail(err)
			}
			fmt.Fprintf(stdout, "started %s on the daemon; smithy workflow history %s shows it\n", name, name)
			return 0
		}
		sc := &workflow.Scheduler{Store: st, Env: workflow.Env{Shell: workflow.Shell, Agent: workflow.Agent, Guard: workflow.Guard,
			Notify: func(_, title, msg string) error { fmt.Fprintf(stdout, "notify: %s\n%s\n", title, msg); return nil }}}
		r, err := sc.Start(context.Background(), name, workflow.Invocation{Trigger: "cli", Inputs: f.inputs})
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

func addExamples(st workflow.Store, rest []string, stdout, stderr io.Writer) int {
	names := workflow.ExampleNames()
	if len(rest) == 1 {
		if _, ok := workflow.Examples[rest[0]]; !ok {
			return cliFail(stderr, "workflow", fmt.Errorf("no example %q (%s)", rest[0], strings.Join(names, ", ")))
		}
		names = rest
	}
	code := 0
	add := func(js string) (string, error) {
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("smithy-example-%d-%d.json", os.Getpid(), time.Now().UnixNano()))
		if err := os.WriteFile(tmp, []byte(js), 0o600); err != nil {
			return "", err
		}
		defer os.Remove(tmp)
		return st.Add(tmp)
	}
	for _, n := range names {
		what, err := add(workflow.Examples[n])
		if err != nil {
			fmt.Fprintln(stderr, "smithy workflow:", text.Clean(err.Error()))
			code = 1
			continue
		}
		line := "added " + what
		if js, ok := workflow.ExampleSchedules[n]; ok {
			if sw, err := add(js); err == nil {
				line += " and " + sw + " (disabled: smithy schedule enable " + n + ")"
			}
		}
		if w, _, err := st.Get(n); err == nil && w.Dir == "" && strings.Contains(workflow.Examples[n], "gh ") {
			line += `; set "dir" to a repo checkout first`
		}
		fmt.Fprintln(stdout, line)
	}
	return code
}

func scheduleMain(st workflow.Store, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(stdout, ScheduleUsage)
		return 0
	}
	f, ok := parseCLI(args[1:], []string{"workflow", "cron", "github", "label", "watch", "dir", "description"}, []string{"webhook"}, stderr, "schedule")
	if !ok {
		return 2
	}
	fail := func(err error) int { return cliFail(stderr, "schedule", err) }
	switch args[0] {
	case "list":
		l, _ := st.Schedules()
		if f.json {
			return printJSON(stdout, l)
		}
		if len(l) == 0 {
			fmt.Fprintln(stdout, "no schedules (smithy schedule add <name> --workflow <w> --cron \"0 9 * * *\")")
		}
		for _, in := range l {
			fmt.Fprintln(stdout, scheduleLine(in))
		}
		return 0
	case "show":
		name, ok := oneName(f, stderr, "schedule", "show")
		if !ok {
			return 2
		}
		_, b, err := st.GetSchedule(name)
		if err != nil {
			return fail(err)
		}
		fmt.Fprint(stdout, string(b))
		return 0
	case "add":
		name, ok := oneName(f, stderr, "schedule", "add")
		if !ok {
			return 2
		}
		sc := workflow.Schedule{Name: name, Workflow: f.vals["workflow"], Description: f.vals["description"], Cron: f.vals["cron"], Webhook: f.bools["webhook"],
			Watch: f.vals["watch"], Dir: f.vals["dir"]}
		if e := f.vals["github"]; e != "" {
			sc.GitHub = &workflow.GitHubTrigger{Event: e, Label: f.vals["label"]}
		}
		if len(f.inputs) > 0 {
			sc.Inputs = f.inputs
		}
		if _, _, err := st.GetSchedule(name); err == nil {
			return fail(fmt.Errorf("schedule %s already exists", name))
		}
		if _, _, err := st.Get(sc.Workflow); err != nil {
			return fail(err)
		}
		if err := st.SaveSchedule(sc); err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "added schedule %s (%s → %s), disabled: smithy schedule enable %s\n", name, sc.Trigger(), sc.Workflow, name)
		return 0
	case "enable", "disable", "remove":
		name, ok := oneName(f, stderr, "schedule", args[0])
		if !ok {
			return 2
		}
		var err error
		switch args[0] {
		case "enable":
			err = st.EnableSchedule(name, time.Now())
		case "disable":
			err = st.DisableSchedule(name)
		default:
			err = st.RemoveSchedule(name)
		}
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s %s\n", map[string]string{"enable": "enabled", "disable": "disabled", "remove": "removed"}[args[0]], name)
		return 0
	case "history":
		name, ok := oneName(f, stderr, "schedule", "history")
		if !ok {
			return 2
		}
		h, err := st.ScheduleHistory(name, f.limit)
		if err != nil {
			return fail(err)
		}
		return printRuns(stdout, h, f.json)
	case "hook":
		name, ok := oneName(f, stderr, "schedule", "hook")
		if !ok {
			return 2
		}
		sc, okHook := st.CheckHook(name, st.HookToken(name))
		if !okHook {
			return fail(fmt.Errorf("%s needs to be a webhook schedule, enabled", name))
		}
		base := "http://127.0.0.1:<daemon port>"
		if i := Running(); i != nil {
			base = i.URL()
		}
		fmt.Fprintf(stdout, "POST %s/v1/hooks/schedules/%s\nX-Smithy-Hook: %s\nbody: a JSON object of %s's inputs; the response holds the run's status and outputs\n",
			strings.TrimRight(base, "/"), name, st.HookToken(name), sc.Workflow)
		return 0
	}
	fmt.Fprintf(stderr, "smithy schedule: unknown subcommand %q\n%s\n", args[0], ScheduleUsage)
	return 2
}

func printRuns(stdout io.Writer, h []workflow.Record, asJSON bool) int {
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

func workflowLine(in workflow.Info) string {
	line := fmt.Sprintf("%-24s %d steps", in.Name, len(in.Steps))
	if len(in.Schedules) > 0 {
		line += " · schedules " + strings.Join(in.Schedules, ", ")
	}
	if in.Last != nil {
		line += " · last " + in.Last.Status + " " + text.Ago(time.Since(in.Last.Started))
	}
	if in.Err != "" {
		line += " · " + in.Err
	}
	return line
}

func scheduleLine(in workflow.SchedInfo) string {
	state := "off"
	switch {
	case in.Err != "":
		state = "error"
	case in.Changed:
		state = "changed"
	case in.Enabled:
		state = "on"
	}
	line := fmt.Sprintf("%-22s %-7s %-26s → %s", in.Name, state, in.Trigger(), in.Workflow)
	if in.Next != nil {
		line += " · next " + in.Next.Format("Mon 15:04")
	}
	if in.Last != nil {
		line += " · last " + in.Last.Status + " " + text.Ago(time.Since(in.Last.Started))
	}
	if in.Err != "" {
		line += " · " + in.Err
	}
	if in.Changed {
		line += " · its file or workflow changed since enable; enable again"
	}
	return line
}

func runLine(r workflow.Record) string {
	trig := r.Trigger
	if r.Schedule != "" {
		trig += ":" + r.Schedule
	}
	line := fmt.Sprintf("%s  %-7s %s %s", r.Started.Local().Format("2006-01-02 15:04"), r.Status, trig, r.Ended.Sub(r.Started).Round(time.Second))
	if r.CostUSD > 0 {
		line += fmt.Sprintf(" $%.2f", r.CostUSD)
	}
	if r.Error != "" {
		line += " · " + text.Clean(r.Error)
	}
	return line
}
