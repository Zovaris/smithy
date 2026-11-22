package main

import "github.com/Zovaris/smithy/internal/plan"

func init() {
	register("stack", "the repo's test tooling as key: value lines", plan.StackUsage, "", plan.StackMain)
	register("changed", "source files changed against a base (--csv for Stryker)", plan.ChangedUsage, "--csv", plan.ChangedMain)
	register("guard-tests", "pre-edit hook: blocks test edits in the green phase", plan.GuardUsage, "", plan.GuardMain)
	register("task-dir", "resolve and create the task dir for a plan slug", plan.TaskDirUsage, "--default", plan.TaskDirMain)
}
