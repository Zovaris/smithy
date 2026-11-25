package main

import (
	"github.com/Zovaris/smithy/internal/auto"
	_ "github.com/Zovaris/smithy/internal/linear"
	"github.com/Zovaris/smithy/internal/tui"
)

func init() {
	register("auto", "autonomous intake: poll issue sources, take queued issues, report back, serve the loop", auto.Usage, "", autoMain)
	registerSub("auto", "poll", "--source=")
	registerSub("auto", "take", "--max=")
	registerSub("auto", "report", "--pr=")
	registerSub("auto", "status", "--json")
	registerSub("auto", "serve", "--agent= --once --max-hours=")
}

var serveMain = auto.ServeMain

func autoMain(args []string) int {
	if len(args) > 0 && args[0] == "serve" {
		tui.Version = version
		return serveMain(args[1:], tui.RunServeView)
	}
	return auto.Main(args)
}
