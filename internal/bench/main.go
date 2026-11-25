package bench

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/usage"
)

// Usage is `smithy bench --help`.
const Usage = `usage: smithy bench list
       smithy bench run --agent <cli> [--model <m>] [--skills <version>]
                        [--task <id>]... [--timeout <min>] [--keep]
       smithy bench runs
       smithy bench compare [--json] <a> <b>

Runs smithy's own task set: small fixture repos (bugs, features, refactors,
docs) whose tests are the oracle. Each task gets a fresh git worktree and one
headless agent run (smithy agent, profile auto); the tests are restored,
then run. Records pass rate, time, tokens and cost in ~/.smithy/bench/.

--agent is a CLI smithy drives (claude, codex, …). api:<provider>/<model>
needs the api backend. --skills is a label for the skills version installed.

compare measures run b against run a: pass rate, time, tokens and cost
deltas, then the tasks that regressed (pass to fail) or got fixed. A run is
its id, a unique id prefix, latest, latest~N or a saved .json path; the
daemon serves the same runs to the web UI (Bench).`

// Dir is where results go.
func Dir() string { return filepath.Join(ghx.Home(), "bench") }

// Main is `smithy bench`.
func Main(args []string) int { return mainIn(Dir(), args, os.Stdout, os.Stderr) }

func mainIn(dir string, args []string, out, errw io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errw, Usage)
		return 2
	}
	switch args[0] {
	case "list":
		ts, err := Tasks()
		if err != nil {
			fmt.Fprintln(errw, "smithy bench:", err)
			return 1
		}
		for _, t := range ts {
			fmt.Fprintf(out, "%-26s %-9s %s\n", t.ID, t.Kind, t.Prompt)
		}
		return 0
	case "run":
		return runMain(dir, args[1:], out, errw)
	case "runs":
		return runsMain(dir, out, errw)
	case "compare":
		return compareMain(dir, args[1:], out, errw)
	}
	fmt.Fprintln(errw, Usage)
	return 2
}

func runMain(dir string, args []string, out, errw io.Writer) int {
	o := Options{Out: dir}
	for len(args) > 0 {
		f := args[0]
		if f == "--keep" {
			o.Keep, args = true, args[1:]
			continue
		}
		if len(args) < 2 {
			fmt.Fprintln(errw, Usage)
			return 2
		}
		switch v := args[1]; f {
		case "--agent":
			o.Agent = v
		case "--model":
			o.Model = v
		case "--skills":
			o.Skills = v
		case "--task":
			o.Tasks = append(o.Tasks, v)
		case "--timeout":
			o.Timeout = v
		default:
			fmt.Fprintln(errw, Usage)
			return 2
		}
		args = args[2:]
	}
	if o.Agent == "" {
		fmt.Fprintln(errw, "smithy bench run: --agent is required\n"+Usage)
		return 2
	}
	if strings.HasPrefix(o.Agent, "api:") {
		fmt.Fprintln(errw, "smithy bench run: the api backend is not in this build yet; use a CLI agent (claude, codex, …)")
		return 2
	}
	if !known(o.Agent) {
		fmt.Fprintf(errw, "smithy bench run: unknown agent %q (known: %s)\n", o.Agent, strings.Join(agents.CLIs, ", "))
		return 2
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(errw, "smithy bench:", err)
		return 1
	}
	r, err := Run(o, cliAgent(self, o, errw))
	if err != nil {
		fmt.Fprintln(errw, "smithy bench:", err)
		return 1
	}
	report(out, r, o.Out)
	return 0
}

// cliAgent runs one headless `smithy agent --profile auto` per task with AGENT_CLI set.
func cliAgent(self string, o Options, errw io.Writer) AgentFunc {
	return func(t Task, dir, prompt, log string) error {
		fmt.Fprintf(errw, "bench: %s …\n", t.ID)
		argv := []string{"agent", "--profile", "auto", "--cwd", dir, "--log", log}
		if o.Model != "" {
			argv = append(argv, "--model", o.Model)
		}
		if o.Timeout != "" {
			argv = append(argv, "--timeout", o.Timeout)
		}
		cmd := exec.Command(self, append(argv, prompt)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "AGENT_CLI="+o.Agent)
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		defer track(cmd)()
		return cmd.Run()
	}
}

func report(out io.Writer, r Result, dir string) {
	for _, t := range r.Tasks {
		mark := "FAIL"
		if t.Pass {
			mark = "pass"
		}
		fmt.Fprintf(out, "%s  %-26s %6.1fs  %s\n", mark, t.ID, t.Seconds, usage.Cost(t.Usage.CostUSD))
	}
	fmt.Fprintf(out, "\n%d/%d passed (%.0f%%) in %.0fs · %s\nsaved %s\n", r.Passed, r.Total, r.PassRate*100, r.Seconds, r.Usage, filepath.Join(dir, r.ID+".json"))
}

func known(cli string) bool {
	for _, c := range agents.CLIs {
		if c == cli {
			return true
		}
	}
	return false
}
