package agents

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/Zovaris/smithy/internal/pool"
)

// SandboxRoleEnv marks the orchestrator serve starts: it stays on the allow-list only, since a
// sandboxed orchestrator could not start sandboxed workers (sandboxes don't nest). Env drops it, so
// every worker the orchestrator starts gets auto.sandbox. A worker that sets it itself is still
// inside its own sandbox, which its children inherit.
const SandboxRoleEnv = "SMITHY_SANDBOX_ROLE"

// DefaultSandboxDomains are the hosts a native-sandboxed worker may reach; auto.sandbox_domains adds more.
var DefaultSandboxDomains = []string{"github.com", "api.github.com", "api.anthropic.com", "api.linear.app"}

// MinClaudeSandbox is the oldest Claude Code smithy's native sandbox settings were verified on
// (failIfUnavailable, network.strictAllowlist, enableWeakerNetworkIsolation); older ones are refused.
const MinClaudeSandbox = "2.1.331"

// Probe is how SandboxCheck looks at the host; tests replace it.
type Probe struct {
	GOOS    string
	Look    func(string) (string, error)
	Version func(cli string) (string, error) // `<cli> --version` output
	// Home is the user's home dir and Exists reports a file; nil means none.
	Home   string
	Exists func(string) bool
}

// HostProbe looks at this machine.
func HostProbe() Probe {
	home, _ := os.UserHomeDir()
	return Probe{GOOS: runtime.GOOS, Look: exec.LookPath, Version: func(cli string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, cli, "--version").Output()
		return string(out), err
	}, Home: home, Exists: func(f string) bool { _, err := os.Stat(f); return err == nil }}
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

// ErrContainerRemoved is why auto.sandbox container is refused: the container mode was removed.
var ErrContainerRemoved = errors.New("auto.sandbox container was removed in v0.23: smithy config set auto.sandbox native (or off)")

// SandboxMode is auto.sandbox from the user's settings: off or native.
func SandboxMode() (string, error) {
	m, err := SandboxSetting("auto.sandbox", "off")
	if err != nil {
		return "", err
	}
	switch m {
	case "", "off":
		return "off", nil
	case "native":
		return m, nil
	case "container":
		return "", ErrContainerRemoved
	}
	return "", fmt.Errorf("auto.sandbox %q is not off or native", m)
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
		return fmt.Errorf("native sandbox can't hold gemini workers to the allowed hosts: Gemini CLI's seatbelt profiles either open every host or need a proxy smithy doesn't ship (use claude, or auto.sandbox off)")
	case "claude":
	default:
		return fmt.Errorf("native sandbox covers claude and codex workers, not %s", cli)
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

// nativeFS is the filesystem part of the native sandbox for p: allowWrite, denyWrite, denyRead
// (and allowRead for an SSH signing key). Claude's sandbox settings and the API loop's shell
// sandbox (Seatbelt, bubblewrap) both build on it.
func nativeFS(p SandboxPaths) map[string][]string {
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
	home, sm := p.Home, p.SmithyHome
	fs := map[string][]string{
		"allowWrite": keep(p.Cwd, p.GitDir, join(sm, "plans"), p.Warden, p.GoCache),
		"denyWrite": keep(append([]string{join(sm, "auth.json"), join(sm, "auth.key"), join(sm, "config.json"), join(p.GitDir, "hooks"), join(p.GitDir, "config"),
			join(p.Cwd, ".claude"), join(p.Cwd, ".smithy/config.json")}, append(p.ReadOnly, p.Secret...)...)...),
		"denyRead": keep(append([]string{join(sm, "auth.json"), join(sm, "auth.key"), join(sm, "daemon.token"), join(home, ".ssh"), join(home, ".aws"), join(home, ".netrc"), join(home, ".git-credentials"),
			join(home, ".gnupg")}, p.Secret...)...),
	}
	if p.SigningKey != "" && strings.HasSuffix(p.SigningKey, ".pub") && filepath.IsAbs(p.SigningKey) {
		fs["allowRead"] = []string{p.SigningKey}
	}
	return fs
}

// NativeSettings is the claude --settings JSON for a native-sandboxed worker: Bash and its children
// write only to the worktree, its git dir, smithy's plans and warden dirs and its Go build cache,
// reach only domains, never read the secrets smithy knows of, and can't fall back to running
// unsandboxed. strictAllowlist blocks unlisted hosts even when a permission mode would allow them.
// weakerNet (macOS only) turns on enableWeakerNetworkIsolation so gh and other Go CLIs can verify TLS.
func NativeSettings(p SandboxPaths, domains []string, goos string, weakerNet bool) string {
	if domains == nil {
		domains = []string{}
	}
	fs := nativeFS(p)
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
// sandboxed worker's shell never reads or writes them.
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
// in the pool is checked (claude accounts get the version floor).
func SandboxCheckAgent(mode, cli string, p Probe) error {
	if mode == "off" || cli != "pool" {
		return SandboxCheck(mode, cli, p)
	}
	as, err := pool.Load()
	if err != nil {
		return fmt.Errorf("reading the pool: %v", err)
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

// NativeGitEnv routes git to GitHub over HTTPS with gh's credential helper: the native sandbox
// denies ~/.ssh (keys and known_hosts) and its proxy only carries HTTPS, so SSH remotes fail.
// It extends any GIT_CONFIG_COUNT entries already in env.
func NativeGitEnv(env []string) []string {
	n := 0
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GIT_CONFIG_COUNT="); ok {
			_, _ = fmt.Sscan(v, &n)
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
