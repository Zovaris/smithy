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

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/events"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/pool"
	"github.com/Zovaris/smithy/internal/usage"
)

const agentUsage = `Harness-agnostic agent runner: runs one headless coding agent on a prompt file.
  smithy agent [--bg] [--cwd <dir>] [--log <file>] [--model <m>] [--effort low|medium] [--timeout <min>]
               [--readonly] [--profile auto] <prompt-file>
Agent CLI: $AGENT_CLI picks one by name; else the first installed of the list below.
  AGENT_CMD   any shell command; gets the prompt on stdin and in $AGENT_PROMPT_FILE, the model
              in $AGENT_MODEL, and $AGENT_READONLY=1 for read-only runs
  claude      claude -p --permission-mode ${AGENT_PERMISSION_MODE:-auto}   (plan when read-only);
              JSON output unless $AGENT_ARGS picks a format: the log keeps the plain answer
              and the reported usage goes to <log>.usage.json and a usage event
  codex       codex exec --sandbox workspace-write, the repo's git dir writable and network on
              (so workers can commit, push and open PRs); read-only sandbox when read-only
  opencode    opencode run --standalone --auto (plan agent when read-only); -m provider/model
  gemini      gemini -p --approval-mode yolo (plan when read-only)
  cursor      agent -p --force (Cursor CLI)
  pi          pi -p (--tools read when read-only)
  dsh         dsh headless --approval-policy never (DeepSeek Harness; model from its profile)
--profile auto (smithy auto serve's orchestrator; never a full bypass): claude runs with
  permission mode dontAsk and an allowed-tools list limited to Read, Edit, Write, Glob, Grep and Bash for
  git, gh, go/npm test and smithy; codex keeps --sandbox workspace-write with approval_policy never;
  the other CLIs keep their own headless mode above.
Effort: --effort, else $AGENT_EFFORT, else the agent.effort setting (default medium). Anything
above medium is capped at medium. Passed as --effort (claude) or -c model_reasoning_effort
(codex), and as $AGENT_EFFORT to every CLI; the others keep their own default and say so.
Timeout: --timeout, else the agent.timeout setting (minutes, default 90; 0 = none). When it
passes, the agent's whole process group is killed (Unix; on Windows only the agent process), "timed out after Nm" goes to the log and
smithy agent exits 124; --bg runs are watched by a detached smithy that does the same.
Pool (smithy pool, off until an account is added): a claude or codex run (AGENT_CLI=pool: any)
uses the least recently used account that is not limited, through CLAUDE_CONFIG_DIR or CODEX_HOME;
a usage-limit result marks it limited; a read-only or untouched run reruns once on the next one,
any other exits 75 with its warden card blocked (relaunch in the same worktree).
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
	Profile    string
	SmithyBins []string // absolute paths of the smithy binary and launcher, allowed under --profile auto
	AddDirs    []string // directories outside the working dir the agent may use (--add-dir), set under --profile auto
	// Sandbox is auto.sandbox for this run (off, native, container); SandboxSettings the claude
	// --settings JSON under native.
	Sandbox, SandboxSettings string
	PoolEnv                  []string // the pool account's config-dir variable (smithy pool), added to the worker's env
	Account                  string   // the pool account this run uses
	poolSkip                 []string // accounts already tried (limited) in this run
	poolStart                string   // the worktree's HEAD before the first attempt ("" when not a git repo)
}

// ProfileEnv carries --profile to every smithy agent an agent starts, so workers under an
// unattended orchestrator get the same allow-list.
const ProfileEnv = "SMITHY_AGENT_PROFILE"

// AutoTools are the only tools a claude session under --profile auto may use; everything else
// is denied (permission mode dontAsk), so an unattended run never asks and never bypasses.
// smithy itself is allowed only by the absolute paths in Options.SmithyBins.
func AutoTools(smithy []string) []string {
	t := []string{"Read", "Edit", "Write", "Glob", "Grep"}
	for _, g := range []string{"status", "diff", "add", "commit", "checkout", "switch", "branch", "log", "merge", "rev-parse", "show"} {
		t = append(t, "Bash(git "+g+" *)", "Bash(git "+g+")")
	}
	t = append(t, "Bash(git fetch)", "Bash(git fetch origin)", "Bash(git fetch origin *)", "Bash(git fetch --prune)", "Bash(git fetch --prune origin)",
		"Bash(git push origin *)", "Bash(git push -u origin *)", "Bash(git worktree list)", "Bash(git worktree list *)")
	for _, g := range []string{"pr create", "pr view", "pr checks", "pr list", "pr comment", "pr diff", "issue view", "issue comment", "run view", "run list"} {
		t = append(t, "Bash(gh "+g+" *)")
	}
	for _, c := range []string{"ls", "pwd", "cat", "head", "tail", "wc", "grep", "rg", "find", "which", "echo", "cd", "sort", "uniq", "diff", "test", "true", "sed -n", "basename", "dirname", "realpath", "date", "jq"} {
		t = append(t, "Bash("+c+")", "Bash("+c+" *)")
	}
	t = append(t, "Bash(go test *)", "Bash(go vet *)", "Bash(go build *)", "Bash(gofmt -l *)", "Bash(go fmt *)", "Bash(npm test *)", "Bash(npm run test *)")
	for _, b := range smithy {
		if filepath.IsAbs(b) {
			t = append(t, "Bash("+b+" *)", "Bash('"+b+"' *)", "Bash(\""+b+"\" *)")
		}
	}
	return t
}

// AutoDenied always wins over AutoTools (Claude Code checks deny rules first): config and
// credential commands, network fetchers, secrets on disk and files that run code later.
func AutoDenied(smithy []string) []string {
	d := []string{"Bash(git -c *)", "Bash(git config *)", "Bash(gh api *)", "Bash(gh auth *)", "Bash(gh gist *)", "Bash(gh alias *)",
		"Bash(gh extension *)", "Bash(smithy auth *)", "Bash(smithy config set *)", "Bash(smithy config set-model *)", "Bash(curl *)", "Bash(wget *)",
		// remote-exec and URL options of fetch/push, and file output of any git command
		"Bash(git * --upload-pack*)", "Bash(git * --receive-pack*)", "Bash(git * --exec*)", "Bash(git * --repo*)", "Bash(git * --output*)",
		"Bash(git push * http*)", "Bash(git push * git@*)", "Bash(git push * ssh*)", "Bash(git push * file*)", "Bash(git checkout * -- /*)",
		// every shell redirect and tee: workers write files with Edit/Write, never through the shell
		"Bash(*>*)", "Bash(* | tee *)", "Bash(*|tee *)",
		// shell readers must not reach the secrets the Read rules protect
		"Bash(*auth.json*)", "Bash(*.ssh*)", "Bash(*.config/gh*)", "Bash(*.aws*)", "Bash(*.netrc*)", "Bash(*.git-credentials*)",
		"Bash(find * -exec*)", "Bash(find * -delete*)", "Bash(sed -n *w *)",
		// pool accounts (smithy pool) hold other accounts' logins
		"Bash(*.smithy/accounts*)", "Bash(smithy pool *)"}
	for _, b := range smithy {
		if filepath.IsAbs(b) {
			d = append(d, "Bash("+b+" auth *)", "Bash("+b+" config set *)", "Bash("+b+" config set-model *)", "Bash("+b+" pool *)")
		}
	}
	for _, f := range []string{"~/.smithy/auth.json", "~/.smithy/accounts/**", "~/.ssh/**", "~/.config/gh/**", "~/.aws/**", "~/.netrc", "~/.git-credentials"} {
		d = append(d, "Read("+f+")")
	}
	for _, f := range []string{"~/.smithy/auth.json", "~/.smithy/config.json", "~/.smithy/accounts/**", "~/.smithy/pool.json", "~/.smithy/pool-state.json", "~/.ssh/**", "~/.config/**", "~/.aws/**", "~/.gitconfig", "~/.netrc",
		"~/.bashrc", "~/.bash_profile", "~/.profile", "~/.zshrc", "~/.zprofile", "~/.zshenv", "~/.claude/**",
		"**/.git/hooks/**", "**/.git/config", "**/.claude/settings*.json", "**/.smithy/config.json"} {
		d = append(d, "Edit("+f+")") // Edit rules cover every file-editing tool, Write included
	}
	return append(d, poolDenied()...)
}

// Kind is the CLI kind of an agent name or path: its base name, with agent meaning cursor.
func Kind(cli string) string {
	if cli == "" || cli == "custom" {
		return cli
	}
	b := strings.TrimSuffix(filepath.Base(cli), ".exe")
	if b == "agent" {
		return "cursor"
	}
	return b
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
	if o.Profile != "" {
		a = append(a, "--profile", o.Profile)
	}
	cmd := exec.Command(exe, append(a, o.PromptFile)...)
	cmd.Dir, cmd.Env = o.Cwd, Scrub(os.Environ())
	return cmd, nil
}

// Scrub drops the source API key env vars (LINEAR_API_KEY and the one named by linear.key_env)
// so a child process, which may read injected issue text, cannot read them.
func Scrub(base []string) []string {
	drop := map[string]bool{"LINEAR_API_KEY": true, config.Get("linear.key_env", "LINEAR_API_KEY"): true}
	var env []string
	for _, kv := range base {
		if k, _, _ := strings.Cut(kv, "="); !drop[k] {
			env = append(env, kv)
		}
	}
	return env
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
	o.Model = ResolveTier(cli, o.Model)
	opt := func(flag string) {
		if o.Model != "" {
			a = append(a, flag, o.Model)
		}
	}
	stdin := false
	path := cli
	cli = Kind(cli)
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
		if o.Profile == "auto" {
			perm = "dontAsk"
		}
		if o.ReadOnly {
			perm = "plan"
		}
		a = []string{"claude", "-p"}
		if !hasArg(extra, "--output-format") {
			a = append(a, "--output-format", "json")
		}
		a = append(a, "--permission-mode", perm)
		if o.Profile == "auto" && !o.ReadOnly {
			a = append(append(a, "--allowedTools"), AutoTools(o.SmithyBins)...)
			a = append(append(a, "--disallowedTools"), AutoDenied(o.SmithyBins)...)
			a = append(a, "--append-system-prompt", AutoNote)
			for _, d := range o.AddDirs {
				a = append(a, "--add-dir", d)
			}
		}
		if o.Sandbox == "native" {
			a = append(a, "--settings", o.SandboxSettings)
		}
		opt("--model")
		if o.Effort != "" {
			a = append(a, "--effort", o.Effort)
		}
		a = append(a, extra...)
		return Spec{withPath(a, path), true, "claude-cli"}, nil
	case "codex":
		a = []string{"codex", "exec"}
		if o.ReadOnly {
			a = append(a, "--sandbox", "read-only")
		} else {
			a = append(a, "--sandbox", "workspace-write", "-c", "sandbox_workspace_write.network_access="+strconv.FormatBool(o.Sandbox != "native"))
			if gitDir != "" {
				a = append(a, "--add-dir", gitDir)
			}
			if o.Profile == "auto" {
				a = append(a, "-c", "approval_policy=never")
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
	return Spec{withPath(a, path), stdin, cli}, nil
}

func hasArg(a []string, flag string) bool {
	for _, x := range a {
		if x == flag || strings.HasPrefix(x, flag+"=") {
			return true
		}
	}
	return false
}

// jsonResult reports whether argv is a claude run smithy switched to --output-format json, whose
// result line Finish turns back into text and a usage sidecar.
func jsonResult(argv []string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--output-format" && argv[i+1] == "json" {
			return Kind(argv[0]) == "claude"
		}
	}
	return false
}

// finish rewrites a claude json log to text and records its usage: the sidecar next to a named
// log and a "usage" event (batch and item from a <warden home>/<batch>/<item>.log path).
func finish(log string, named bool, account string) {
	u, ok := usage.Finish(log)
	if !named {
		os.Remove(usage.Sidecar(log))
	}
	if !ok {
		return
	}
	e := events.Event{Kind: "usage", Usage: &u, Account: account}
	if named {
		e.Detail = filepath.Base(log)
		if dir := filepath.Dir(log); realPath(filepath.Dir(dir)) == realPath(cards.Home()) {
			e.Batch, e.Item = filepath.Base(dir), strings.TrimSuffix(filepath.Base(log), ".log")
		}
	}
	_ = events.Append(e)
}

// realPath resolves symlinks so a linked warden home still matches; p as given when it can't.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// withPath runs an absolute or relative CLI path in place of the bare name.
func withPath(a []string, path string) []string {
	if strings.ContainsRune(path, os.PathSeparator) || strings.ContainsRune(path, '/') {
		a[0] = path
	}
	return a
}

// Env is the worker's environment: base without the parent harness's variables, with
// WARDEN_HARNESS and the AGENT_* run variables set.
func Env(base []string, s Spec, o Options) []string {
	drop := map[string]bool{ProfileEnv: true, SandboxRoleEnv: true, "WARDEN_HARNESS": true, "AGENT_PROMPT_FILE": true, "AGENT_MODEL": true, "AGENT_READONLY": true, "AGENT_EFFORT": true}
	if len(o.PoolEnv) > 0 {
		for _, k := range PoolDropEnv {
			drop[k] = true
		}
	}
	for _, k := range ParentVars {
		drop[k] = true
	}
	var env []string
	for _, kv := range Scrub(base) {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			env = append(env, kv)
		}
	}
	ro := "0"
	if o.ReadOnly {
		ro = "1"
	}
	if o.Profile != "" {
		env = append(env, ProfileEnv+"="+o.Profile)
	}
	env = append(env, o.PoolEnv...)
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
	if o.Profile == "auto" {
		o.SmithyBins = SmithyPaths()
		o.AddDirs = AutoDirs(o.SmithyBins)
	}
	gitDir := ""
	ph := PoolHarness(cli)
	var acct pool.Account
	switch {
	case ph != "" && o.Bg:
		if cli == "pool" {
			cli = peekHarness(ph)
		}
	case ph != "":
		var rc int
		if acct, rc = acquire(ph, &o); rc != 0 {
			if len(o.poolSkip) > 0 {
				blockCard(o, o.poolSkip[0])
			}
			return rc
		}
		if len(o.poolSkip) == 0 {
			o.poolStart = head(o.Cwd)
		}
		if cli == "pool" && acct.Name != "" {
			cli = acct.Harness
		}
	}
	// after the pool picked the harness, so the check is the CLI that actually runs
	if o.Profile == "auto" && os.Getenv(SandboxRoleEnv) != "orchestrator" {
		mode, err := SandboxMode()
		if err == nil {
			err = SandboxCheck(mode, cli, HostProbe())
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "smithy agent: auto.sandbox: %v; worker not started (fails closed)\n", err)
			return 1
		}
		o.Sandbox = mode
	}
	if (Kind(cli) == "codex" && !o.ReadOnly) || o.Sandbox == "native" || o.Sandbox == "container" {
		gitDir, _ = ghx.Run("git", "-C", o.Cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
	}
	gocache := ""
	if o.Sandbox == "native" && Kind(cli) == "claude" {
		var err error
		if o.SandboxSettings, gocache, err = nativeSettings(&o, gitDir); err != nil {
			fmt.Fprintf(os.Stderr, "smithy agent: auto.sandbox: %v; worker not started (fails closed)\n", err)
			return 1
		}
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
	if cli != "custom" && !EffortFlags[Kind(cli)] {
		fmt.Fprintf(os.Stderr, "smithy agent: %s has no effort flag; it runs at its own default (AGENT_EFFORT=%s is set)\n", cli, o.Effort)
	}
	s, err := Build(cli, o, prompt, SplitArgs(os.Getenv("AGENT_ARGS")), gitDir, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	asJSON := jsonResult(s.Argv)
	env := Env(os.Environ(), s, o)
	if gocache != "" {
		env = append(env, "GOCACHE="+gocache)
	}
	if o.Sandbox == "native" {
		env = append(env, NativeGitEnv(env)...)
	}
	if o.Sandbox == "container" {
		box, err := container(o.Cwd, gitDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "smithy agent: auto.sandbox: %v; worker not started (fails closed)\n", err)
			return 1
		}
		env, box.Env = ContainerEnv(env)
		s.Argv[0] = filepath.Base(s.Argv[0]) // a host path means nothing inside the image
		s.Argv = box.Argv(s.Argv)
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
		if len(o.poolSkip) > 0 {
			out, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		} else {
			out, err = os.Create(path)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := exec.Command(s.Argv[0], s.Argv[1:]...)
	cmd.Dir, cmd.Env = o.Cwd, env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, out, out
	if o.Bg {
		if o.Timeout > 0 || asJSON || ph != "" {
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
		launchCard(log, o.Cwd, cmd.Process.Pid)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			var ee *exec.ExitError
			if err != nil && !(errors.As(err, &ee) && (ee.ExitCode() == 124 || ee.ExitCode() == ExitLimited)) {
				fmt.Fprintf(os.Stderr, "smithy agent: %s exited right after starting (%v): %s\n", s.Argv[0], err, lastLine(log))
				setCard(log, o.Cwd, "state=failed", "note=worker exited: "+lastLine(log))
				return 1
			}
		case <-time.After(startCheck):
		}
		fmt.Println(strconv.Itoa(cmd.Process.Pid), log)
		return 0
	}
	rc := wait(cmd, o.Timeout, out)
	out.Close()
	hit := acct.Name != "" && limited(acct, out.Name(), Kind(cli), rc)
	if asJSON {
		finish(out.Name(), o.Log != "", o.Account)
	}
	if hit {
		if len(o.poolSkip) == 0 && (o.ReadOnly || untouched(o.Cwd, o.poolStart)) {
			if o.Log == "" {
				os.Remove(out.Name())
			}
			fmt.Fprintln(os.Stderr, "smithy agent: nothing changed yet; retrying once on the next pool account")
			o.poolSkip, o.PoolEnv, o.Account = []string{acct.Name}, nil, ""
			return Run(o)
		}
		blockCard(o, acct.Name)
		rc = ExitLimited
	}
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
	if os.Getenv(ProfileEnv) == "auto" {
		o.Profile = "auto"
	}
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
		case "--profile":
			if args[1] != "auto" {
				fmt.Fprintf(os.Stderr, "smithy agent: unknown profile %q (known: auto)\n", args[1])
				return 2
			}
			o.Profile = args[1]
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

// SmithyPaths are the absolute paths of this smithy binary and the use-smithy launcher.
func SmithyPaths() []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		add(exe)
	}
	// Every skill's launcher: skills say `smithy` for `<skill-dir>/scripts/smithy`, so an
	// orchestrator following tend or warden runs their launcher, not use-smithy's.
	var roots []string
	if d := os.Getenv("SMITHY_SKILL_DIR"); d != "" {
		roots = append(roots, filepath.Dir(filepath.Dir(d)))
	}
	for _, s := range []string{"use-smithy", "tend", "warden", "smelt", "scout", "ledger", "hush"} {
		if p, ok := FindSkill(s); ok {
			add(filepath.Join(filepath.Dir(p), "scripts", "smithy"))
			roots = append(roots, filepath.Dir(filepath.Dir(p)))
		}
	}
	for _, r := range roots {
		ents, _ := os.ReadDir(r)
		for _, e := range ents {
			l := filepath.Join(r, e.Name(), "scripts", "smithy")
			if fi, err := os.Stat(l); err == nil && !fi.IsDir() {
				add(l)
			}
		}
	}
	return out
}

// AutoDirs lists what an unattended agent needs outside its working dir: smithy's home (plans,
// batches, cards; secrets there stay behind the Read/Edit deny rules), each skills root, and the
// parent of the repo, where sibling worktrees are created. Claude Code otherwise refuses any
// Bash or file access outside the working dir in dontAsk mode.
func AutoDirs(bins []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && d != "/" && !seen[d] {
			if fi, err := os.Stat(d); err == nil && fi.IsDir() {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	home := os.Getenv("SMITHY_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".smithy")
		}
	}
	add(home)
	for _, b := range bins {
		if filepath.Base(filepath.Dir(b)) == "scripts" {
			add(filepath.Dir(filepath.Dir(filepath.Dir(b))))
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if top, err := exec.Command("git", "-C", wd, "rev-parse", "--show-toplevel").Output(); err == nil {
			add(filepath.Dir(strings.TrimSpace(string(top))))
		}
	}
	return out
}

// AutoNote tells an unattended agent how to live with the allow-list instead of giving up.
const AutoNote = "Unattended run: shell access is limited by policy. If a command is denied, " +
	"use the Read, Grep and Glob tools for files, split chained commands into single ones, and " +
	"use smithy commands (smithy worktree add, smithy commit, smithy pr) instead of raw git or gh " +
	"where they exist. Report blocked only when a git, gh, test or smithy step your task needs " +
	"is denied in its simplest form."

// tierHarness names the harness whose models.<harness>.<tier> settings a headless CLI uses.
var tierHarness = map[string]string{"claude": "claude-cli", "codex": "codex", "opencode": "opencode", "gemini": "gemini", "cursor": "cursor", "pi": "pi", "dsh": "dsh"}

// ResolveTier turns a tier name (cheap, mid, strong, judge) into the user's model for that CLI's
// harness; an unset tier gives "" (the CLI's default model) with one warning. Any other value is a
// model name and passes through.
func ResolveTier(cli, model string) string {
	switch model {
	case "cheap", "mid", "strong", "judge":
	default:
		return model
	}
	h := tierHarness[filepath.Base(cli)]
	if h == "" {
		h = filepath.Base(cli)
	}
	m, err := config.ModelFor(h, model)
	if err != nil || m == "" {
		fmt.Fprintf(os.Stderr, "smithy agent: no %s model set for %s (smithy config set-model %s <name>); using the CLI's default\n", model, h, model)
		return ""
	}
	return m
}
