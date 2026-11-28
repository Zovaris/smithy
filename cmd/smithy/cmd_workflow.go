//go:build harness

package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/daemon"
)

func init() {
	register("workflow", harnessSummary["workflow"], daemon.WorkflowUsage, "--json --limit= --input=",
		func(args []string) int { return daemon.WorkflowMain(args, os.Stdout, os.Stderr) })
	for _, s := range []string{"list", "show", "run", "history", "enable", "disable", "add", "validate", "example", "hook"} {
		registerSub("workflow", s, "--json --limit= --input=")
	}
}
