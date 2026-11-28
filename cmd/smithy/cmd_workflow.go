//go:build harness

package main

import (
	"os"

	"github.com/Zovaris/smithy/internal/daemon"
)

func init() {
	register("workflow", harnessSummary["workflow"], daemon.WorkflowUsage, "--json --limit= --input=",
		func(args []string) int { return daemon.WorkflowMain(args, os.Stdout, os.Stderr) })
	for _, s := range []string{"list", "show", "run", "history", "add", "validate", "example"} {
		registerSub("workflow", s, "--json --limit= --input=")
	}
	register("schedule", harnessSummary["schedule"], daemon.ScheduleUsage, "--json --limit= --input= --workflow= --cron= --webhook --github= --label= --watch= --dir= --description=",
		func(args []string) int { return daemon.ScheduleMain(args, os.Stdout, os.Stderr) })
	for _, s := range []string{"list", "show", "add", "enable", "disable", "remove", "history", "hook"} {
		registerSub("schedule", s, "--json --limit= --input= --workflow= --cron= --webhook --github= --label= --watch= --dir= --description=")
	}
}
