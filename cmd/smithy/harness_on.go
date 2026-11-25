//go:build harness

package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/daemon"
	_ "github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/tui"
)

// harnessBuild is true in the harness build (docs/adr/0009); its commands register themselves.
const harnessBuild = true

func init() {
	register("daemon", harnessSummary["daemon"], daemon.Usage, "--bg --port= --agent= --once --max-hours=", func(args []string) int {
		tui.Version = version
		return daemon.Main(args, tui.RunServeView)
	})
	register("attach", harnessSummary["attach"], tui.AttachUsage, "--", func(args []string) int {
		return tui.Attach(args)
	})
	register("chat", harnessSummary["chat"], tui.ChatUsage, "--", func(args []string) int {
		return tui.Chat(args)
	})
	register("web", harnessSummary["web"], daemon.WebUsage, "--", func(args []string) int {
		tok := func() (string, error) { return daemon.LoadToken(daemon.TokenFile(), false) }
		return daemon.Web(args, daemon.Running, tok, tui.OpenURL, os.Stdout, os.Stderr)
	})
	noArgs = func() int { return tui.Chat(nil) }
	serveMain = daemon.ServeOrAttach
}
