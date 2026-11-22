// Package agents runs headless coding agents (smithy agent, smithy consult), routes tasks to a
// model tier (smithy route), finds installed skills (smithy skills) and sets up the agent
// instructions block (smithy setup).
package agents

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
)

const agentUsage = `Harness-agnostic agent runner: runs one headless coding agent on a prompt file.
  smithy agent [--bg] [--cwd <dir>] [--log <file>] [--model <m>] [--effort low|medium] [--timeout <min>]
               [--readonly] <prompt-file>
Agent CLI: $AGENT_CLI picks one by name; else the first installed of the list below.
  AGENT_CMD   any shell command; gets the prompt on stdin and in $AGENT_PROMPT_FILE, the model
              in $AGENT_MODEL, and $AGENT_READONLY=1 for read-only runs
  claude      claude -p --permission-mode ${AGENT_PERMISSION_MODE:-auto}   (plan when read-only)
  codex       codex exec --sandbox workspace-write, the repo's git dir writable and network on
              (so workers can commit, push and open PRs); read-only sandbox when read-only
  opencode    opencode run --standalone --auto (plan agent when read-only); -m provider/model
  gemini      gemini -p --approval-mode yolo (plan when read-only)
  cursor      agent -p --force (Cursor CLI)
  pi          pi -p (--tools read when read-only)
  dsh         dsh headless --approval-policy never (DeepSeek Harness; model from its profile)
Effort: --effort, else $AGENT_EFFORT, else the agent.effort setting (default medium). Anything
above medium is capped at medium. Passed as --effort (claude) or -c model_reasoning_effort
(codex), and as $AGENT_EFFORT to every CLI; the others keep their own default and say so.
Timeout: --timeout, else the agent.timeout setting (minutes, default 90; 0 = none). When it
passes, the agent's whole process group is killed (Unix; on Windows only the agent process), "timed out after Nm" goes to the log and
smithy agent exits 124; --bg runs are watched by a detached smithy that does the same.
Extra flags for the chosen CLI: $AGENT_ARGS. Workers get WARDEN_HARNESS set to the CLI and the
parent harness's variables removed, so their own scripts detect the right harness.
--bg prints ` + "`<pid> <log>`" + ` and returns at once; otherwise prints the agent's output when it ends
and exits with its code (output goes through a file, so a server the CLI leaves running can't
keep the caller waiting).`

// CLIs is the order in which installed agent CLIs are picked when AGENT_CLI and AGENT_CMD are unset.
var CLIs = []string{"claude", "codex", "opencode", "gemini", "cursor", "pi", "dsh"}

// ParentVars are the harness variables removed from a worker's environment.
var ParentVars = []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CODEX_THREAD_ID", "OPENCODE", "OPENCODE_SESSION_ID", "GEMINI_CLI", "PI_CODING_AGENT", "DSH_SESSION_ID", "AI_AGENT"}

// ErrNoCLI means no agent CLI is installed and AGENT_CMD is unset.
var ErrNoCLI = errors.New("no agent CLI found: install claude, codex, opencode, gemini, cursor (agent), pi or dsh, or set AGENT_CMD")

// Options are the flags of one agent run.
type Options struct {
	Bg         bool
	Cwd        string
	Log        string
	Model      string
	Effort     string
	ReadOnly   bool
	PromptFile string
	Timeout    time.Duration
}

// DefaultTimeout is the agent.timeout setting when unset, in minutes.
const DefaultTimeout = "90"

// ParseTimeout reads a timeout in minutes (fractions allowed); 0 → none. Anything else that is
// not a finite positive number warns and falls back to DefaultTimeout, so a typo keeps the cap.
func ParseTimeout(min string) time.Duration {
	f, err := strconv.ParseFloat(strings.TrimSpace(min), 64)
	if err == nil && f == 0 {
		return 0
	}
	if err != nil || f < 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		fmt.Fprintf(os.Stderr, "smithy agent: timeout %q is not a number of minutes; using %sm\n", min, DefaultTimeout)
		f, _ = strconv.ParseFloat(DefaultTimeout, 64)
	}
	return time.Duration(f * float64(time.Minute))
}

func minutes(d time.Duration) string { return strconv.FormatFloat(d.Minutes(), 'g', -1, 64) }

func supervisor(o Options, log string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	a := []string{"agent", "--timeout", minutes(o.Timeout), "--cwd", o.Cwd, "--log", log, "--effort", o.Effort}
	if o.Model != "" {
		a = append(a, "--model", o.Model)
	}
	if o.ReadOnly {
		a = append(a, "--readonly")
	}
	cmd := exec.Command(exe, append(a, o.PromptFile)...)
	cmd.Dir = o.Cwd
	return cmd, nil
}

// Spec is what to execute for one run: the argv, whether stdin is the prompt file (else the null
// device) and the harness name the worker sees in WARDEN_HARNESS.
type Spec struct {
	Argv        []string
	PromptStdin bool
	Harness     string
}

// Efforts are the reasoning efforts smithy passes on, lowest first; MaxEffort is the cap.
var Efforts = []string{"low", "medium"}

// MaxEffort is the highest effort smithy ever asks an agent for.
const MaxEffort = "medium"

// ClampEffort normalizes an effort: "" → MaxEffort, low and medium as they are, anything else
// (high, xhigh, max, unknown) → MaxEffort. capped reports whether a higher value was lowered.
func ClampEffort(e string) (effort string, capped bool) {
	e = strings.ToLower(strings.TrimSpace(e))
	switch e {
	case "":
		return MaxEffort, false
	case "minimal", "low":
		return "low", false
	case "medium":
		return "medium", false
	}
	return MaxEffort, true
}

// DefaultEffort is the effort from $AGENT_EFFORT, else the agent.effort setting, clamped.
func DefaultEffort() string {
	e := os.Getenv("AGENT_EFFORT")
	if e == "" {
		e = config.Get("agent.effort", MaxEffort)
	}
	e, _ = ClampEffort(e)
	return e
}

// EffortFlags are the CLIs that take an effort flag; the rest only get $AGENT_EFFORT.
var EffortFlags = map[string]bool{"claude": true, "codex": true}

// Bin is the executable name of an agent CLI.
func Bin(cli string) string {
	if cli == "cursor" {
		return "agent"
	}
	return cli
}

func installed(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// ResolveCLI picks the CLI from AGENT_CLI, AGENT_CMD ("custom") or the first installed one.
func ResolveCLI(getenv func(string) string, have func(string) bool) string {
	cli := getenv("AGENT_CLI")
	if cli == "" && getenv("AGENT_CMD") == "" {
		for _, c := range CLIs {
			if have(Bin(c)) {
				cli = c
				break
			}
		}
	}
	if getenv("AGENT_CMD") != "" {
		cli = "custom"
	}
	return cli
}

// Build returns the command for cli. prompt is the prompt text (for CLIs that take it as an
// argument), extra the split $AGENT_ARGS and gitDir the repo's common git dir (codex only).
func Build(cli string, o Options, prompt string, extra []string, gitDir string, getenv func(string) string) (Spec, error) {
	var a []string
	opt := func(flag string) {
		if o.Model != "" {
			a = append(a, flag, o.Model)
		}
	}
	stdin := false
	switch cli {
	case "custom":
		h := getenv("WARDEN_HARNESS")
		if h == "" {
			h = "unknown"
		}
		return Spec{[]string{"bash", "-c", getenv("AGENT_CMD")}, true, h}, nil
	case "claude":
		perm := getenv("AGENT_PERMISSION_MODE")
		if perm == "" {
			perm = "auto"
		}
		if o.ReadOnly {
			perm = "plan"
		}
		a = []string{"claude", "-p", "--permission-mode", perm}
		opt("--model")
		if o.Effort != "" {
			a = append(a, "--effort", o.Effort)
		}
		a = append(a, extra...)
		return Spec{a, true, "claude-cli"}, nil
	case "codex":
		a = []string{"codex", "exec"}
		if o.ReadOnly {
			a = append(a, "--sandbox", "read-only")
		} else {
			a = append(a, "--sandbox", "workspace-write", "-c", "sandbox_workspace_write.network_access=true")
			if gitDir != "" {
				a = append(a, "--add-dir", gitDir)
			}
		}
		opt("-m")
		if o.Effort != "" {
			a = append(a, "-c", "model_reasoning_effort="+o.Effort)
		}
		a = append(append(a, extra...), "-")
		stdin = true
	case "opencode":
		a = []string{"opencode", "run", "--standalone", "--archived"}
		if o.ReadOnly {
			a = append(a, "--agent", "plan")
		} else {
			a = append(a, "--auto")
		}
		opt("-m")
		a = append(append(a, extra...), prompt)
	case "gemini":
		mode := "yolo"
		if o.ReadOnly {
			mode = "plan"
		}
		a = []string{"gemini", "--approval-mode", mode}
		opt("-m")
		a = append(append(a, extra...), "-p", prompt)
	case "cursor":
		a = []string{"agent", "-p", "--force"}
		opt("--model")
		a = append(append(a, extra...), prompt)
	case "pi":
		a = []string{"pi", "-p"}
		if o.ReadOnly {
			a = append(a, "--tools", "read")
		}
		opt("--model")
		a = append(append(a, extra...), prompt)
	case "dsh":
		a = []string{"dsh", "headless", "--approval-policy", "never"}
		if o.ReadOnly {
			a = append(a, "--permission-mode", "read-only")
		}
		a = append(append(a, extra...), "-")
		stdin = true
	default:
		return Spec{}, ErrNoCLI
	}
	return Spec{a, stdin, cli}, nil
}

// Env is the worker's environment: base without the parent harness's variables, with
// WARDEN_HARNESS and the AGENT_* run variables set.
func Env(base []string, s Spec, o Options) []string {
	drop := map[string]bool{"WARDEN_HARNESS": true, "AGENT_PROMPT_FILE": true, "AGENT_MODEL": true, "AGENT_READONLY": true, "AGENT_EFFORT": true}
	for _, k := range ParentVars {
		drop[k] = true
	}
	var env []string
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			env = append(env, kv)
		}
	}
	ro := "0"
	if o.ReadOnly {
		ro = "1"
	}
	return append(env, "AGENT_PROMPT_FILE="+o.PromptFile, "AGENT_MODEL="+o.Model, "AGENT_EFFORT="+o.Effort, "AGENT_READONLY="+ro, "WARDEN_HARNESS="+s.Harness)
}

// SplitArgs splits $AGENT_ARGS into words the way a shell would, honouring quotes and backslashes.
func SplitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	in, word := byte(0), false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case in == '\'':
			if c == '\'' {
				in = 0
			} else {
				cur.WriteByte(c)
			}
		case in == '"':
			if c == '"' {
				in = 0
			} else if c == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
				i++
				cur.WriteByte(s[i])
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			in, word = c, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			word = true
		case c == ' ' || c == '\t' || c == '\n':
			if word {
				out = append(out, cur.String())
				cur.Reset()
				word = false
			}
		default:
			cur.WriteByte(c)
			word = true
		}
	}
	if word {
		out = append(out, cur.String())
	}
	return out
}

func absFile(p string) (string, error) {
	a, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	d, err := filepath.EvalSymlinks(filepath.Dir(a))
	if err != nil {
		return "", err
	}
	return filepath.Join(d, filepath.Base(a)), nil
}

// Run runs one agent and returns the exit code. In the background it prints `<pid> <log>`;
// otherwise it prints the agent's output once it ends.
func Run(o Options) int {
	cli := ResolveCLI(os.Getenv, installed)
	pf, err := absFile(o.PromptFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	o.PromptFile = pf
	if o.Cwd == "" {
		o.Cwd, _ = os.Getwd()
	}
	gitDir := ""
	if cli == "codex" && !o.ReadOnly {
		gitDir, _ = ghx.Run("git", "-C", o.Cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	}
	prompt := ""
	if cli != "custom" {
		b, err := os.ReadFile(pf)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		prompt = strings.TrimRight(string(b), "\n")
	}
	if cli != "custom" && !EffortFlags[cli] {
		fmt.Fprintf(os.Stderr, "smithy agent: %s has no effort flag; it runs at its own default (AGENT_EFFORT=%s is set)\n", cli, o.Effort)
	}
	s, err := Build(cli, o, prompt, SplitArgs(os.Getenv("AGENT_ARGS")), gitDir, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if fi, err := os.Stat(o.Cwd); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "smithy agent: cd: %s: No such file or directory\n", o.Cwd)
		return 1
	}
	if _, err := exec.LookPath(s.Argv[0]); err != nil {
		fmt.Fprintf(os.Stderr, "smithy agent: %s: command not found\n", s.Argv[0])
		return 127
	}
	stdinPath := os.DevNull
	if s.PromptStdin {
		stdinPath = pf
	}
	stdin, err := os.Open(stdinPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer stdin.Close()
	log := o.Log
	var out *os.File
	if log == "" {
		out, err = os.CreateTemp("", "agent.")
		if err == nil {
			log = out.Name()
		}
	} else {
		path := log
		if !filepath.IsAbs(path) {
			path = filepath.Join(o.Cwd, path)
		}
		out, err = os.Create(path)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...)
	cmd.Dir, cmd.Env = o.Cwd, Env(os.Environ(), s, o)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, out, out
	if o.Bg {
		if o.Timeout > 0 {
			if cmd, err = supervisor(o, out.Name()); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		detach(cmd)
		err = cmd.Start()
		out.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			var ee *exec.ExitError
			if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 124) {
				fmt.Fprintf(os.Stderr, "smithy agent: %s exited right after starting (%v): %s\n", s.Argv[0], err, lastLine(log))
				return 1
			}
		case <-time.After(startCheck):
		}
		fmt.Println(strconv.Itoa(cmd.Process.Pid), log)
		return 0
	}
	rc := wait(cmd, o.Timeout, out)
	out.Close()
	if b, err := os.ReadFile(out.Name()); err == nil {
		os.Stdout.Write(b)
	}
	if o.Log == "" {
		os.Remove(out.Name())
	}
	return rc
}

// startCheck is how long --bg watches a new agent for an immediate failure (bad login, bad flag).
var startCheck = 3 * time.Second

func lastLine(file string) string {
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	l := lines[len(lines)-1]
	if len(l) > 300 {
		l = l[:300]
	}
	return l
}

func wait(cmd *exec.Cmd, timeout time.Duration, out *os.File) int {
	ownGroup(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	var expired <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		expired = t.C
	}
	timedOut := false
	for {
		select {
		case err := <-done:
			if timedOut {
				return 124
			}
			if err != nil {
				return exitCode(err)
			}
			return 0
		case s := <-sig:
			forward(cmd, s)
		case <-expired:
			timedOut = true
			forward(cmd, syscall.SIGKILL)
			fmt.Fprintf(out, "smithy agent: timed out after %sm\n", minutes(timeout))
		}
	}
}

// AgentMain runs `smithy agent` and returns the exit code.
func AgentMain(args []string) int {
	o := Options{Model: os.Getenv("AGENT_MODEL"), Effort: os.Getenv("AGENT_EFFORT"), Timeout: ParseTimeout(config.Get("agent.timeout", DefaultTimeout))}
	if o.Effort == "" {
		o.Effort = config.Get("agent.effort", MaxEffort)
	}
loop:
	for len(args) > 1 {
		switch args[0] {
		case "--bg":
			o.Bg, args = true, args[1:]
			continue
		case "--readonly":
			o.ReadOnly, args = true, args[1:]
			continue
		case "--cwd":
			o.Cwd = args[1]
		case "--log":
			o.Log = args[1]
		case "--model":
			o.Model = args[1]
		case "--effort":
			o.Effort = args[1]
		case "--timeout":
			o.Timeout = ParseTimeout(args[1])
		default:
			break loop
		}
		args = args[2:]
	}
	if len(args) == 1 && !isFile(args[0]) {
		fmt.Fprintf(os.Stderr, "smithy agent: prompt file not found: %s\n", args[0])
		return 2
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, agentUsage)
		return 2
	}
	o.PromptFile = args[0]
	var capped bool
	asked := o.Effort
	if o.Effort, capped = ClampEffort(o.Effort); capped {
		fmt.Fprintf(os.Stderr, "smithy agent: effort %q capped at %s\n", asked, o.Effort)
	}
	return Run(o)
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
