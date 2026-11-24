package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
)

// SandboxRoleEnv marks the orchestrator serve starts: it stays on the allow-list only, since a
// sandboxed orchestrator could not start sandboxed workers (sandboxes don't nest). Env drops it, so
// every worker the orchestrator starts gets auto.sandbox. A worker that sets it itself is still
// inside its own sandbox, which its children inherit.
const SandboxRoleEnv = "SMITHY_SANDBOX_ROLE"

// DefaultSandboxDomains are the hosts a native-sandboxed worker may reach; auto.sandbox_domains adds more.
var DefaultSandboxDomains = []string{"github.com", "api.github.com", "api.anthropic.com", "api.linear.app"}

// DefaultSandboxImage is the container image when auto.sandbox_image is unset (build it from sandbox/Dockerfile).
const DefaultSandboxImage = "smithy-sandbox:latest"

// MinClaudeSandbox is the oldest Claude Code smithy's native sandbox settings were verified on
// (failIfUnavailable, network.strictAllowlist, enableWeakerNetworkIsolation); older ones are refused.
const MinClaudeSandbox = "2.1.331"

// Probe is how SandboxCheck looks at the host; tests replace it.
type Probe struct {
	GOOS    string
	Look    func(string) (string, error)
	Version func(cli string) (string, error) // `<cli> --version` output
}

// HostProbe looks at this machine.
func HostProbe() Probe {
	return Probe{GOOS: runtime.GOOS, Look: exec.LookPath, Version: func(cli string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, cli, "--version").Output()
		return string(out), err
	}}
}

// SandboxSetting reads an auto.sandbox* key from the user's settings only (never a repo's), so a
// cloned repo can't turn the sandbox off or widen it. A read error is returned: callers fail closed.
func SandboxSetting(key, fallback string) (string, error) {
	v, err := config.UserGet(key, fallback)
	if err != nil {
		return "", fmt.Errorf("reading %s from %s: %v", key, config.UserFile(), err)
	}
	return v, nil
}

// SandboxMode is auto.sandbox from the user's settings: off, native or container.
func SandboxMode() (string, error) {
	m, err := SandboxSetting("auto.sandbox", "off")
	if err != nil {
		return "", err
	}
	switch m {
	case "", "off":
		return "off", nil
	case "native", "container":
		return m, nil
	}
	return "", fmt.Errorf("auto.sandbox %q is not off, native or container", m)
}

var semver = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// versionAtLeast reports whether the first x.y.z in out is at least min.
func versionAtLeast(out, min string) bool {
	got, want := semver.FindStringSubmatch(out), semver.FindStringSubmatch(min)
	if got == nil || want == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		g, _ := strconv.Atoi(got[i])
		w, _ := strconv.Atoi(want[i])
		if g != w {
			return g > w
		}
	}
	return true
}

// SandboxCheck says why mode can't start a cli worker here (nil when it can). Callers fail closed:
// a worker whose sandbox can't start is not started.
func SandboxCheck(mode, cli string, p Probe) error {
	switch mode {
	case "off":
		return nil
	case "container":
		if ContainerEngine(p.Look) == "" {
			return fmt.Errorf("no docker or podman on PATH")
		}
		return nil
	case "native":
	default:
		return fmt.Errorf("unknown sandbox mode %q", mode)
	}
	switch Kind(cli) {
	case "codex":
		if p.GOOS != "darwin" && p.GOOS != "linux" {
			return fmt.Errorf("codex's sandbox needs macOS or Linux, not %s", p.GOOS)
		}
		return nil
	case "claude":
	default:
		return fmt.Errorf("native sandbox covers claude and codex workers, not %s (use auto.sandbox container)", cli)
	}
	switch p.GOOS {
	case "darwin":
		if _, err := p.Look("/usr/bin/sandbox-exec"); err != nil {
			return fmt.Errorf("sandbox-exec missing (/usr/bin/sandbox-exec)")
		}
	case "linux":
		for _, b := range []string{"bwrap", "socat"} {
			if _, err := p.Look(b); err != nil {
				return fmt.Errorf("%s missing (install bubblewrap and socat)", b)
			}
		}
	default:
		return fmt.Errorf("claude's sandbox needs macOS or Linux, not %s", p.GOOS)
	}
	if p.Version == nil {
		return fmt.Errorf("can't check the claude version")
	}
	out, err := p.Version(cli)
	if err != nil {
		return fmt.Errorf("%s --version failed: %v", cli, err)
	}
	if !versionAtLeast(out, MinClaudeSandbox) {
		return fmt.Errorf("claude %s is older than %s, the oldest the sandbox settings were verified on (update claude)",
			strings.TrimSpace(firstLine(out)), MinClaudeSandbox)
	}
	return nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// ContainerEngine is docker, else podman, else "".
func ContainerEngine(look func(string) (string, error)) string {
	for _, e := range []string{"docker", "podman"} {
		if _, err := look(e); err == nil {
			return e
		}
	}
	return ""
}

// SandboxDomains is DefaultSandboxDomains plus the comma list extra, without duplicates.
func SandboxDomains(extra string) []string {
	out := slices.Clone(DefaultSandboxDomains)
	for _, d := range strings.Split(extra, ",") {
		if d = strings.TrimSpace(d); d != "" && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// SandboxPaths are the absolute paths a native sandbox opens or closes; empty ones are left out.
type SandboxPaths struct {
	Cwd, GitDir, Home, SmithyHome, Warden, GoCache string
	ReadOnly                                       []string // --add-dir dirs a worker reads but never writes from the shell
	// SigningKey is git's SSH signing public key (user.signingkey, a .pub) and AgentSocket the
	// ssh-agent socket: signed commits work through the agent while the private keys stay unreadable.
	SigningKey, AgentSocket string
}

// NativeDirs narrows the --add-dir list of a native-sandboxed worker, since Claude Code also opens
// every --add-dir dir for sandboxed writes: smithy's home becomes its plans and warden dirs, the repo
// parent (sibling worktrees) is dropped, and every other dir (skills roots) stays readable only.
func NativeDirs(dirs []string, smithyHome, plans, warden, repoParent string) (add, readOnly []string) {
	for _, d := range dirs {
		switch d {
		case smithyHome:
			for _, x := range []string{plans, warden} {
				if x != "" && !slices.Contains(add, x) {
					add = append(add, x)
				}
			}
		case repoParent:
		default:
			add, readOnly = append(add, d), append(readOnly, d)
		}
	}
	return add, readOnly
}

// NativeSettings is the claude --settings JSON for a native-sandboxed worker: Bash and its children
// write only to the worktree, its git dir, smithy's plans and warden dirs and its Go build cache,
// reach only domains, never read the secrets smithy knows of, and can't fall back to running
// unsandboxed. strictAllowlist blocks unlisted hosts even when a permission mode would allow them.
// weakerNet (macOS only) turns on enableWeakerNetworkIsolation so gh and other Go CLIs can verify TLS.
func NativeSettings(p SandboxPaths, domains []string, goos string, weakerNet bool) string {
	keep := func(ps ...string) []string {
		var out []string
		for _, x := range ps {
			if x != "" && filepath.IsAbs(x) && !slices.Contains(out, x) {
				out = append(out, x)
			}
		}
		return out
	}
	join := func(dir, name string) string {
		if dir == "" {
			return ""
		}
		return filepath.Join(dir, name)
	}
	if domains == nil {
		domains = []string{}
	}
	home, sm := p.Home, p.SmithyHome
	fs := map[string][]string{
		"allowWrite": keep(p.Cwd, p.GitDir, join(sm, "plans"), p.Warden, p.GoCache),
		"denyWrite": keep(append([]string{join(sm, "auth.json"), join(sm, "config.json"), join(p.GitDir, "hooks"), join(p.GitDir, "config"),
			join(p.Cwd, ".claude"), join(p.Cwd, ".smithy/config.json")}, p.ReadOnly...)...),
		"denyRead": keep(join(sm, "auth.json"), join(home, ".ssh"), join(home, ".aws"), join(home, ".netrc"), join(home, ".git-credentials"),
			join(home, ".gnupg")),
	}
	if p.SigningKey != "" && strings.HasSuffix(p.SigningKey, ".pub") && filepath.IsAbs(p.SigningKey) {
		fs["allowRead"] = []string{p.SigningKey}
	}
	net := map[string]any{"strictAllowlist": true, "allowedDomains": domains}
	if p.AgentSocket != "" && goos == "darwin" {
		net["allowUnixSockets"] = []string{p.AgentSocket}
	}
	sb := map[string]any{
		"enabled":                  true,
		"failIfUnavailable":        true,
		"allowUnsandboxedCommands": false,
		"autoAllowBashIfSandboxed": false,
		"network":                  net,
		"filesystem":               fs,
	}
	if goos == "darwin" && weakerNet {
		sb["enableWeakerNetworkIsolation"] = true
	}
	b, _ := json.Marshal(map[string]any{"sandbox": sb})
	return string(b)
}

// GoCacheDir is a worker's own Go build cache: under the warden dir (writable to it), one per worktree.
func GoCacheDir(warden, cwd string) string {
	return filepath.Join(warden, "gocache", filepath.Base(cwd))
}

// nativeSettings fills SandboxPaths from this machine for a worker in cwd, narrows its --add-dir
// list (NativeDirs) and returns the settings JSON and the worker's GOCACHE.
func nativeSettings(o *Options, gitDir string) (settings, gocache string, err error) {
	extra, err := SandboxSetting("auto.sandbox_domains", "")
	if err != nil {
		return "", "", err
	}
	weak, err := SandboxSetting("auto.sandbox_weaker_net", "true")
	if err != nil {
		return "", "", err
	}
	cwd := o.Cwd
	parent := ""
	if top, err := ghx.Run("git", "-C", cwd, "rev-parse", "--show-toplevel"); err == nil {
		parent = filepath.Dir(top)
	}
	sm := ghx.Home()
	var ro []string
	o.AddDirs, ro = NativeDirs(o.AddDirs, sm, filepath.Join(sm, "plans"), cards.Home(), parent)
	home, _ := os.UserHomeDir()
	gocache = GoCacheDir(realPath(cards.Home()), cwd)
	if err := os.MkdirAll(gocache, 0o700); err != nil {
		return "", "", err
	}
	for i, d := range ro {
		ro[i] = realPath(d)
	}
	key, sock := "", ""
	if f, _ := ghx.Run("git", "-C", cwd, "config", "--get", "gpg.format"); f == "ssh" {
		if k, _ := ghx.Run("git", "-C", cwd, "config", "--get", "user.signingkey"); strings.HasPrefix(k, "~/") {
			key = filepath.Join(home, k[2:])
		} else {
			key = k
		}
		sock = os.Getenv("SSH_AUTH_SOCK")
	}
	p := SandboxPaths{Cwd: realPath(cwd), Home: home, SmithyHome: sm, Warden: cards.Home(), GoCache: gocache, ReadOnly: ro,
		SigningKey: key, AgentSocket: sock}
	if gitDir != "" {
		p.GitDir = realPath(gitDir)
	}
	return NativeSettings(p, SandboxDomains(extra), runtime.GOOS, weak != "false"), gocache, nil
}

// Container is one `docker|podman run` for a worker.
type Container struct {
	Engine, Image, Network, Cwd, User, Memory string
	Mounts                                    []string // host paths, mounted read-write at the same path
	ReadOnly                                  []string // host paths inside Mounts, mounted again read-only over them
	Env                                       []string // names only: values come from the engine's env, never argv
}

// Argv wraps argv in `<engine> run --rm -i` with no capabilities, a read-only root and tmpfs /tmp,
// pid and memory limits, the mounts, -w Cwd and the env names. Network is "none" unless set.
func (c Container) Argv(argv []string) []string {
	network, memory := c.Network, c.Memory
	if network == "" {
		network = "none"
	}
	if memory == "" {
		memory = "4g"
	}
	a := []string{c.Engine, "run", "--rm", "-i", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--read-only", "--tmpfs", "/tmp", "--pids-limit", "512", "--memory", memory, "--network", network}
	if c.Engine == "podman" {
		a = append(a, "--userns=keep-id")
	} else if c.User != "" {
		a = append(a, "--user", c.User)
	}
	a = append(a, "-w", c.Cwd)
	for _, m := range c.Mounts {
		a = append(a, "-v", m+":"+m)
	}
	for _, m := range c.ReadOnly {
		a = append(a, "-v", m+":"+m+":ro")
	}
	a = append(a, "-e", "HOME=/tmp")
	for _, n := range c.Env {
		a = append(a, "-e", n)
	}
	return append(append(a, c.Image), argv...)
}

// containerPass are the env names a container worker gets (when set): its run variables, model
// API keys and proxy settings. A GitHub token passes only as SMITHY_SANDBOX_GH_TOKEN (a scoped
// token), renamed GH_TOKEN; the host's own GitHub tokens never do.
var containerPass = []string{ProfileEnv, "WARDEN_HARNESS", "AGENT_PROMPT_FILE", "AGENT_MODEL", "AGENT_EFFORT", "AGENT_READONLY",
	"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "OPENAI_API_KEY", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy"}

// ContainerEnv is the engine's env (base, with GH_TOKEN set to SMITHY_SANDBOX_GH_TOKEN or removed)
// and the names to pass into the container.
func ContainerEnv(base []string) (env, names []string) {
	scoped := ""
	for _, kv := range base {
		if k, v, _ := strings.Cut(kv, "="); k == "SMITHY_SANDBOX_GH_TOKEN" {
			scoped = v
		}
	}
	have := map[string]bool{}
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if k == "GH_TOKEN" {
			continue
		}
		have[k] = true
		env = append(env, kv)
	}
	for _, n := range containerPass {
		if have[n] {
			names = append(names, n)
		}
	}
	if scoped != "" {
		env = append(env, "GH_TOKEN="+scoped)
		names = append(names, "GH_TOKEN")
	}
	return env, names
}

// container builds the Container for a worker in cwd on this machine; the git dir's hooks and
// config are mounted read-only over it.
func container(cwd, gitDir string) (Container, error) {
	var c Container
	var err error
	get := func(key, def string) string {
		v, e := SandboxSetting(key, def)
		if e != nil && err == nil {
			err = e
		}
		return v
	}
	c = Container{Engine: ContainerEngine(exec.LookPath), Image: get("auto.sandbox_image", DefaultSandboxImage),
		Network: get("auto.sandbox_network", ""), Memory: get("auto.sandbox_memory", "4g"), Cwd: cwd}
	if err != nil {
		return Container{}, err
	}
	if runtime.GOOS == "linux" {
		c.User = strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	}
	for _, d := range []string{cwd, gitDir, filepath.Join(ghx.Home(), "plans"), cards.Home()} {
		if fi, err := os.Stat(d); d != "" && err == nil && fi.IsDir() && !slices.Contains(c.Mounts, d) {
			c.Mounts = append(c.Mounts, d)
		}
	}
	if gitDir != "" {
		for _, n := range []string{"hooks", "config"} {
			if _, err := os.Stat(filepath.Join(gitDir, n)); err == nil {
				c.ReadOnly = append(c.ReadOnly, filepath.Join(gitDir, n))
			}
		}
	}
	return c, nil
}
