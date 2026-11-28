package main

import (
	"github.com/Zovaris/smithy/internal/tui/kit"
	"github.com/Zovaris/smithy/internal/tui/stats"
)

func init() {
	register("stats", "live stats of the forge: agents, incoming issues, what needs you, activity", stats.Usage, "--no-anim --demo",
		func(args []string) int { kit.Version = version; return stats.Main(args) })
}
