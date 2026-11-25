package main

import "github.com/Zovaris/smithy/internal/guard"

func init() {
	register("guard", "dangerous-command guard: check a command, Claude Code hook, list the rules", guard.Usage, "", guard.Main)
}
