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

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/pool"
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
	// Home is the user's home dir, Exists reports a file or socket, Getenv reads the env, Secret is a
	// service's key from smithy auth and Context the docker context's endpoint; nil means none.
	Home    string
	Exists  func(string) bool
	Getenv  func(string) string
	Secret  func(svc string) string
	Context func(docker string) string
}

// HostProbe looks at this machine.
func HostProbe() Probe {
	home, _ := os.UserHomeDir()
	return Probe{GOOS: runtime.GOOS, Look: exec.LookPath, Version: func(cli string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, cli, "--version").Output()
		return string(out), err
	}, Home: home, Exists: func(f string) bool { _, err := os.Stat(f); return err == nil }, Getenv: os.Getenv,
		Secret: auth.Stored,
		Context: func(docker string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			out, _ := exec.CommandContext(ctx, docker, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output()
			return strings.TrimSpace(string(out))
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
		return containerCheck(cli, p)
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
	case "gemini":
		return fmt.Errorf("native sandbox can't hold gemini workers to the allowed hosts: Gemini CLI's seatbelt profiles either open every host or need a proxy smithy doesn't ship, and on Linux it only offers docker/podman (use auto.sandbox container)")
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

// ContainerEngine is the docker binary (on PATH, else Kiri's or OrbStack's), else podman, else "".
func ContainerEngine(p Probe) string {
	if _, err := p.Look("docker"); err == nil {
		return "docker"
	}
	for _, b := range []string{".kiri/bin/docker", ".orbstack/bin/docker"} {
		if f := filepath.Join(p.Home, b); p.Home != "" && p.Exists != nil && p.Exists(f) {
			return f
		}
	}
	if _, err := p.Look("podman"); err == nil {
		return "podman"
	}
	return ""
}

// dockerSockets are the engine sockets tried, in order, when DOCKER_HOST is unset and the docker
// context's socket is gone: Kiri, OrbStack, Colima, Docker Desktop, the system socket.
var dockerSockets = []string{"~/.kiri/run/docker.sock", "~/.orbstack/run/docker.sock", "~/.colima/default/docker.sock",
	"~/.docker/run/docker.sock", "/var/run/docker.sock"}

// DockerHost is the DOCKER_HOST a docker engine runs with ("" leaves it: podman, DOCKER_HOST
// already set, or the context's socket exists) and the endpoint it ends up using, for doctor.
func DockerHost(engine string, p Probe) (set, endpoint string) {
	if engine == "" || filepath.Base(engine) == "podman" {
		return "", ""
	}
	if p.Getenv != nil {
		if h := p.Getenv("DOCKER_HOST"); h != "" {
			return "", h
		}
	}
	exists := func(f string) bool { return p.Exists != nil && p.Exists(f) }
	if p.Context != nil {
		if ep := p.Context(engine); ep != "" {
			if f, ok := strings.CutPrefix(ep, "unix://"); !ok || exists(f) {
				return "", ep
			}
		}
	}
	for _, f := range dockerSockets {
		if rest, ok := strings.CutPrefix(f, "~/"); ok {
			if p.Home == "" {
				continue
			}
			f = filepath.Join(p.Home, rest)
		}
		if exists(f) {
			return "unix://" + f, "unix://" + f
		}
	}
	return "", ""
}

// ContainerNoNetwork is why a container with no network is refused: claude can't reach its API
// and git can't push.
const ContainerNoNetwork = "container needs a network: smithy config set auto.sandbox_network proxy (egress allow-list and secrets) or bridge (open egress)"

// ContainerNoCredential is why a claude container with no credential is refused: a Keychain login
// never reaches the container.
const ContainerNoCredential = "no claude credential for the container: run claude setup-token, then smithy auth set claude"

// credNames are every model credential env name a container could carry.
var credNames = []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"}

// ContainerCreds are the {smithy auth service, env name} pairs a harness kind's container worker
// may carry, the only credentials it gets: claude its token or Anthropic key, codex its OpenAI key,
// opencode none (its free models need no login) unless model names the anthropic or openai provider.
func ContainerCreds(kind, model string) [][2]string {
	claude := [][2]string{{"claude", "CLAUDE_CODE_OAUTH_TOKEN"}, {"anthropic", "ANTHROPIC_API_KEY"}}
	openai := [][2]string{{"openai", "OPENAI_API_KEY"}}
	switch kind {
	case "claude":
		return claude
	case "codex":
		return openai
	case "opencode":
		switch {
		case strings.HasPrefix(model, "anthropic/"):
			return claude[1:]
		case strings.HasPrefix(model, "openai/"):
			return openai
		}
	}
	return nil
}

// ContainerSecrets drops from env the credential names creds doesn't list, then adds NAME=value
// from smithy auth for the listed ones env lacks. Values stay in the engine's env; the container
// gets them by name (-e NAME), never on argv.
func ContainerSecrets(env []string, creds [][2]string, secret func(svc string) string) []string {
	allowed := map[string]bool{}
	for _, c := range creds {
		allowed[c[1]] = true
	}
	have := map[string]bool{}
	var out []string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if slices.Contains(credNames, k) && !allowed[k] {
			continue
		}
		if v != "" {
			have[k] = true
		}
		out = append(out, kv)
	}
	for _, c := range creds {
		if have[c[1]] || secret == nil {
			continue
		}
		if k := secret(c[0]); k != "" {
			out = append(out, c[1]+"="+k)
			have[c[1]] = true
		}
	}
	return out
}

// containerCheck says why a container worker can't start: no engine, no network, or for claude no
// credential in the env or smithy auth.
func containerCheck(cli string, p Probe) error {
	if err := containerBase(p); err != nil {
		return err
	}
	if k := Kind(cli); k != "claude" && k != "opencode" {
		if img, err := SandboxSetting("auto.sandbox_image", DefaultSandboxImage); err != nil {
			return err
		} else if img == DefaultSandboxImage {
			return fmt.Errorf("the default sandbox image carries claude and opencode only: build an image with %s and set auto.sandbox_image", k)
		}
	}
	creds := ContainerCreds(Kind(cli), "")
	if len(creds) == 0 {
		return nil
	}
	for _, s := range creds {
		if (p.Getenv != nil && p.Getenv(s[1]) != "") || (p.Secret != nil && p.Secret(s[0]) != "") {
			return nil
		}
	}
	if Kind(cli) == "claude" {
		return fmt.Errorf("%s", ContainerNoCredential)
	}
	return fmt.Errorf("no %s credential for the container: smithy auth set %s", cli, creds[0][0])
}

// PoolContainerCreds are ContainerCreds for a pooled run: a claude account's own token
// (smithy auth set claude:<account>); other harnesses log in from the mounted account dir.
func PoolContainerCreds(a pool.Account) [][2]string {
	if a.Harness == "claude" {
		return [][2]string{{"claude:" + a.Name, "CLAUDE_CODE_OAUTH_TOKEN"}}
	}
	return nil
}

// containerBase says why no container can start: no engine or no network.
func containerBase(p Probe) error {
	if ContainerEngine(p) == "" {
		return fmt.Errorf("no docker or podman on PATH (nor ~/.kiri/bin/docker or ~/.orbstack/bin/docker)")
	}
	n, err := SandboxSetting("auto.sandbox_network", "")
	if err != nil {
		return err
	}
	if n == "" || n == "none" {
		return fmt.Errorf("%s", ContainerNoNetwork)
	}
	if n == "host" {
		return fmt.Errorf("auto.sandbox_network host shares the host's network, so the container isolates nothing: use proxy (or bridge)")
	}
	if n == "proxy" {
		if _, err := proxyBinary(p); err != nil {
			return err
		}
	}
	return nil
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
	Secret                  []string // more dirs never read or written from the shell (smithy pool account dirs)
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
			join(p.Cwd, ".claude"), join(p.Cwd, ".smithy/config.json")}, append(p.ReadOnly, p.Secret...)...)...),
		"denyRead": keep(append([]string{join(sm, "auth.json"), join(home, ".ssh"), join(home, ".aws"), join(home, ".netrc"), join(home, ".git-credentials"),
			join(home, ".gnupg")}, p.Secret...)...),
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
	secret, err := PoolDirs()
	if err != nil {
		return "", "", err
	}
	p := SandboxPaths{Cwd: realPath(cwd), Home: home, SmithyHome: sm, Warden: cards.Home(), GoCache: gocache, ReadOnly: ro,
		SigningKey: key, AgentSocket: sock, Secret: secret}
	if gitDir != "" {
		p.GitDir = realPath(gitDir)
	}
	return NativeSettings(p, SandboxDomains(extra), runtime.GOOS, weak != "false"), gocache, nil
}

// PoolDirs are the smithy pool account dirs (each a harness login) and their default root: a
// sandboxed worker's shell never reads or writes them, and a container mounts only its run's own.
func PoolDirs() ([]string, error) {
	out := []string{filepath.Join(ghx.Home(), "accounts")}
	as, err := pool.Load()
	if err != nil {
		return nil, fmt.Errorf("reading the pool: %v", err)
	}
	for _, a := range as {
		if a.Dir != "" && !slices.Contains(out, a.Dir) {
			out = append(out, a.Dir)
		}
	}
	for i, d := range out {
		out[i] = realPath(d)
	}
	return out, nil
}

// SandboxCheckAgent is SandboxCheck for an agent setting that may be "pool": then every harness
// in the pool is checked (claude accounts get the version floor); in container mode each claude
// account needs its own token (claude:<account>), since the Keychain login never reaches a container.
func SandboxCheckAgent(mode, cli string, p Probe) error {
	if mode == "off" || cli != "pool" {
		return SandboxCheck(mode, cli, p)
	}
	as, err := pool.Load()
	if err != nil {
		return fmt.Errorf("reading the pool: %v", err)
	}
	if mode == "container" {
		if len(as) == 0 {
			return fmt.Errorf("the pool has no accounts (smithy pool add)")
		}
		if err := containerBase(p); err != nil {
			return err
		}
		for _, a := range as {
			if creds := PoolContainerCreds(a); len(creds) > 0 && (p.Secret == nil || p.Secret(creds[0][0]) == "") {
				return fmt.Errorf("pool account %s: no claude credential for the container: run claude setup-token, then smithy auth set %s", a.Name, creds[0][0])
			}
		}
		return nil
	}
	var seen []string
	for _, a := range as {
		if slices.Contains(seen, a.Harness) {
			continue
		}
		seen = append(seen, a.Harness)
		if err := SandboxCheck(mode, a.Harness, p); err != nil {
			return fmt.Errorf("pool account %s: %v", a.Name, err)
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("the pool has no accounts (smithy pool add)")
	}
	return nil
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
	if filepath.Base(c.Engine) == "podman" {
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
	"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "OPENAI_API_KEY", "HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy", "SMITHY_PROXY"}

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

// overlaps reports whether d is, holds or sits inside one of dirs.
func overlaps(d string, dirs []string) bool {
	for _, x := range dirs {
		if d == x || strings.HasPrefix(d, x+string(filepath.Separator)) || strings.HasPrefix(x, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// container builds the Container for a worker in cwd on this machine; the git dir's hooks and
// config are mounted read-only over it. A pooled run (acct.Dir set) also mounts its own account
// dir and passes its config-dir variable; no other account's dir is mounted.
func container(cwd, gitDir string, acct pool.Account) (Container, error) {
	var c Container
	var err error
	get := func(key, def string) string {
		v, e := SandboxSetting(key, def)
		if e != nil && err == nil {
			err = e
		}
		return v
	}
	c = Container{Engine: ContainerEngine(HostProbe()), Image: get("auto.sandbox_image", DefaultSandboxImage),
		Network: get("auto.sandbox_network", ""), Memory: get("auto.sandbox_memory", "4g"), Cwd: cwd}
	if err != nil {
		return Container{}, err
	}
	if runtime.GOOS == "linux" {
		c.User = strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	}
	secret, err := PoolDirs()
	if err != nil {
		return Container{}, err
	}
	for _, d := range []string{cwd, gitDir, filepath.Join(ghx.Home(), "plans"), cards.Home()} {
		if fi, err := os.Stat(d); d != "" && err == nil && fi.IsDir() && !slices.Contains(c.Mounts, d) && !overlaps(realPath(d), secret) {
			c.Mounts = append(c.Mounts, d)
		}
	}
	if acct.Dir != "" && pool.Harnesses[acct.Harness] != "" {
		if fi, err := os.Stat(acct.Dir); err != nil || !fi.IsDir() {
			return Container{}, fmt.Errorf("pool account %s: dir %s: not found (smithy pool login %s)", acct.Name, acct.Dir, acct.Name)
		}
		own := realPath(acct.Dir)
		holds := own == realPath(ghx.Home()) || strings.HasPrefix(realPath(ghx.Home()), own+string(filepath.Separator))
		for _, d := range secret {
			holds = holds || strings.HasPrefix(d, own+string(filepath.Separator))
		}
		if holds {
			return Container{}, fmt.Errorf("pool account %s: dir %s holds another account's dir or %s; give it a dir of its own", acct.Name, acct.Dir, ghx.Home())
		}
		c.Mounts = append(c.Mounts, acct.Dir)
		c.Env = append(c.Env, pool.Harnesses[acct.Harness])
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

// NativeGitEnv routes git to GitHub over HTTPS with gh's credential helper: the native sandbox
// denies ~/.ssh (keys and known_hosts) and its proxy only carries HTTPS, so SSH remotes fail.
// It extends any GIT_CONFIG_COUNT entries already in env.
func NativeGitEnv(env []string) []string {
	n := 0
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GIT_CONFIG_COUNT="); ok {
			fmt.Sscan(v, &n)
		}
	}
	pairs := [][2]string{
		{"url.https://github.com/.insteadOf", "git@github.com:"},
		{"url.https://github.com/.insteadOf", "ssh://git@github.com/"},
		{"credential.https://github.com.helper", ""},
		{"credential.https://github.com.helper", "!gh auth git-credential"},
	}
	out := []string{}
	for i, p := range pairs {
		out = append(out, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", n+i, p[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", n+i, p[1]))
	}
	return append(out, fmt.Sprintf("GIT_CONFIG_COUNT=%d", n+len(pairs)))
}
