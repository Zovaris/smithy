//go:build harness

package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/daemon"
)

func init() {
	register("workflow", harnessSummary["workflow"], daemon.WorkflowUsage, "--json --limit=",
		func(args []string) int { return daemon.WorkflowMain(args, os.Stdout, os.Stderr) })
	for _, s := range []string{"list", "show", "run", "history", "enable", "disable", "add", "example"} {
		registerSub("workflow", s, "--json --limit=")
	}
}
