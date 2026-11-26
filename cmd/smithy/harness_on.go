//go:build harness

package main

import (
	"os"
	"time"

	"github.com/Zovaris/smithy/internal/agents"
	"github.com/Zovaris/smithy/internal/apiloop"
	"github.com/Zovaris/smithy/internal/apiloop/provider"
	"github.com/Zovaris/smithy/internal/codemode"
	"github.com/Zovaris/smithy/internal/daemon"
	"github.com/Zovaris/smithy/internal/remote"
	_ "github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/tui"
)

// harnessBuild is true in the harness build (docs/adr/0009); its commands register themselves.
const harnessBuild = true

func init() {
	tui.Version, daemon.Version = version, version
	agents.APIProvider = provider.New
	agents.CodeMode = func(t apiloop.Tools) apiloop.Tools { return codemode.Wrap(t, codemode.Limits{}) }
	register("daemon", harnessSummary["daemon"], daemon.Usage, "--bg --port= --idle-exit= --demo --agent= --once --max-hours=", func(args []string) int {
		return daemon.Main(args, tui.RunServeView)
	})
	register("stop", harnessSummary["stop"], daemon.StopUsage, "", func([]string) int {
		return daemon.Stop(os.Stdout, 10*time.Second)
	})
	register("attach", harnessSummary["attach"], tui.AttachUsage, "--", tui.Attach)
	register("chat", harnessSummary["chat"], tui.ChatUsage, "--standalone --server= --", tui.Chat)
	register("web", harnessSummary["web"], daemon.WebUsage, "--", func(args []string) int {
		tok := func() (string, error) { return daemon.LoadToken(daemon.TokenFile(), false) }
		return daemon.Web(args, daemon.Running, tok, tui.OpenURL, os.Stdout, os.Stderr)
	})
	register("pair", harnessSummary["pair"], daemon.PairUsage, "list revoke", func(args []string) int {
		return daemon.PairCmd(args, os.Stdout, os.Stderr)
	})
	register("servers", harnessSummary["servers"], remote.Usage, "list add rm use discover --code", func(args []string) int {
		return remote.Cmd(args, remote.Env{})
	})
	noArgs = func() int { return tui.Chat(nil) }
	serveMain = daemon.ServeOrAttach
}
