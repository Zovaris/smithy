package agents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Zovaris/smithy/internal/agents/sandbox"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/layout"
)

// nativeSettings fills sandbox.SandboxPaths from this machine for a worker in cwd, narrows its --add-dir
// list (sandbox.NativeDirs) and returns the settings JSON and the worker's GOCACHE.
func nativeSettings(o *Options, gitDir string) (settings, gocache string, err error) {
	extra, err := sandbox.SandboxSetting("auto.sandbox_domains", "")
	if err != nil {
		return "", "", err
	}
	weak, err := sandbox.SandboxSetting("auto.sandbox_weaker_net", "true")
	if err != nil {
		return "", "", err
	}
	cwd := o.Cwd
	parent := ""
	if top, err := ghx.Run("git", "-C", cwd, "rev-parse", "--show-toplevel"); err == nil {
		parent = filepath.Dir(top)
	}
	sm := layout.Home()
	var ro []string
	o.AddDirs, ro = sandbox.NativeDirs(o.AddDirs, sm, filepath.Join(sm, "plans"), layout.WardenHome(), parent)
	home := layout.UserHome()
	gocache = sandbox.GoCacheDir(sandbox.RealPath(layout.WardenHome()), cwd)
	if err := os.MkdirAll(gocache, 0o700); err != nil {
		return "", "", err
	}
	for i, d := range ro {
		ro[i] = sandbox.RealPath(d)
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
	secret, err := sandbox.PoolDirs()
	if err != nil {
		return "", "", err
	}
	p := sandbox.SandboxPaths{Cwd: sandbox.RealPath(cwd), Home: home, SmithyHome: sm, Warden: layout.WardenHome(), GoCache: gocache, ReadOnly: ro,
		SigningKey: key, AgentSocket: sock, Secret: secret}
	if gitDir != "" {
		p.GitDir = sandbox.RealPath(gitDir)
	}
	return sandbox.NativeSettings(p, sandbox.SandboxDomains(extra), runtime.GOOS, weak != "false"), gocache, nil
}
