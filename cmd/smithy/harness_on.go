//go:build harness

package main

import (
	"github.com/Zovaris/smithy/internal/daemon"
	_ "github.com/Zovaris/smithy/internal/store"
	"github.com/Zovaris/smithy/internal/tui"
)

// harnessBuild is true in the harness build (docs/adr/0009); its commands register themselves.
const harnessBuild = true

func init() {
	register("daemon", "run the auto loop with a local API (events, decisions)", daemon.Usage, "--bg --port= --agent= --once --max-hours=", func(args []string) int {
		tui.Version = version
		return daemon.Main(args, tui.RunServeView)
	})
	serveMain = daemon.ServeOrAttach
}
