package agents

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/ghx"
)

// errAPIContainer is why an api: worker refuses auto.sandbox container.
var errAPIContainer = errors.New("api workers don't run in a container (container mode is being removed next release): use auto.sandbox native or off")

// apiShellSandbox is the auto.sandbox wrapper for an API run's shell commands: nil when the
// sandbox is off, the native OS sandbox (Seatbelt on macOS, bubblewrap on Linux) for native, and
// an error for container or when the native sandbox can't start here (callers fail closed).
func apiShellSandbox(mode string, p Probe, paths SandboxPaths) (func([]string) []string, error) {
	switch mode {
	case "off", "":
		return nil, nil
	case "container":
		return nil, errAPIContainer
	case "native":
	default:
		return nil, fmt.Errorf("unknown sandbox mode %q", mode)
	}
	fs := nativeFS(paths)
	env := []string{}
	if paths.GoCache != "" {
		env = append(env, "GOCACHE="+paths.GoCache)
	}
	switch p.GOOS {
	case "darwin":
		exe, err := p.Look("/usr/bin/sandbox-exec")
		if err != nil {
			return nil, fmt.Errorf("sandbox-exec missing (/usr/bin/sandbox-exec)")
		}
		prof := SeatbeltProfile(fs)
		return func(argv []string) []string {
			return append(append([]string{exe, "-p", prof, "/usr/bin/env"}, env...), argv...)
		}, nil
	case "linux":
		exe, err := p.Look("bwrap")
		if err != nil {
			return nil, fmt.Errorf("bwrap missing (install bubblewrap)")
		}
		args := BwrapArgs(fs, p.Exists)
		return func(argv []string) []string {
			a := append([]string{exe}, args...)
			for _, e := range env {
				k, v, _ := strings.Cut(e, "=")
				a = append(a, "--setenv", k, v)
			}
			return append(append(a, "--"), argv...)
		}, nil
	}
	return nil, fmt.Errorf("the native sandbox needs macOS or Linux, not %s", p.GOOS)
}

// apiSandboxPaths are the native sandbox's paths for an API run in cwd: the same writable and
// secret dirs a claude worker's sandbox gets.
func apiSandboxPaths(cwd string) (SandboxPaths, error) {
	home, _ := os.UserHomeDir()
	secret, err := PoolDirs()
	if err != nil {
		return SandboxPaths{}, err
	}
	gocache := GoCacheDir(realPath(cards.Home()), cwd)
	if err := os.MkdirAll(gocache, 0o700); err != nil {
		return SandboxPaths{}, err
	}
	p := SandboxPaths{Cwd: realPath(cwd), Home: home, SmithyHome: ghx.Home(), Warden: cards.Home(), GoCache: gocache, Secret: secret}
	if g, err := ghx.Run("git", "-C", cwd, "rev-parse", "--path-format=absolute", "--git-common-dir"); err == nil && g != "" {
		p.GitDir = realPath(g)
	}
	return p, nil
}

// SeatbeltProfile is a sandbox-exec profile from nativeFS: writes only under allowWrite (and the
// temp and device dirs), never under denyWrite, no reads of denyRead, and no network.
func SeatbeltProfile(fs map[string][]string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny network*)\n(deny file-write*)\n")
	sub := func(verb string, ps []string) {
		for _, p := range ps {
			fmt.Fprintf(&b, "(%s (subpath %s))\n", verb, strconv.Quote(realPath(p)))
		}
	}
	tmp := []string{"/dev", "/private/tmp", "/private/var/folders"}
	if t := os.Getenv("TMPDIR"); t != "" {
		tmp = append(tmp, t)
	}
	sub("allow file-write*", append(tmp, fs["allowWrite"]...))
	sub("deny file-write*", fs["denyWrite"])
	sub("deny file-read*", fs["denyRead"])
	return b.String()
}

// BwrapArgs are the bubblewrap options from nativeFS: the root read-only, allowWrite bound
// writable, denyWrite read-only again, denyRead hidden, no network. exists reports a path;
// missing ones are skipped (bwrap can't bind them).
func BwrapArgs(fs map[string][]string, exists func(string) bool) []string {
	a := []string{"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp", "--unshare-net", "--die-with-parent", "--new-session"}
	has := func(p string) bool { return exists == nil || exists(p) }
	for _, p := range fs["allowWrite"] {
		if has(p) {
			a = append(a, "--bind", p, p)
		}
	}
	for _, p := range fs["denyWrite"] {
		if has(p) && !slices.Contains(fs["denyRead"], p) {
			a = append(a, "--ro-bind", p, p)
		}
	}
	for _, p := range fs["denyRead"] {
		if !has(p) {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			a = append(a, "--ro-bind", "/dev/null", p)
		} else {
			a = append(a, "--tmpfs", p)
		}
	}
	return a
}
