package main

import "github.com/Zovaris/smithy/internal/tui"

func init() {
	register("tui", "live dashboard: auto watcher, queue, batches, tasks, decisions", tui.Usage, "--no-anim",
		func(args []string) int { tui.Version = version; return tui.Main(args) })
}
