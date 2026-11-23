package main

import "github.com/Zovaris/smithy/internal/tui"

func init() {
	register("tui", "live dashboard of the forge: agents, incoming issues, what needs you, activity", tui.Usage, "--no-anim --demo",
		func(args []string) int { tui.Version = version; return tui.Main(args) })
}
