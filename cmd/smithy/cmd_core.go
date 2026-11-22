package main

import (
	"fmt"
	"os"

	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/harness"
	"github.com/Zovaris/smithy/internal/names"
	"github.com/Zovaris/smithy/internal/tasks"
	"github.com/Zovaris/smithy/internal/wait"
)

func init() {
	register("config", "settings and models (smithy config list)", config.Usage, "--repo", configMain)
	register("harness", `"<harness> <adapter>" for this shell`,
		"usage: smithy harness\nPrints \"<harness> <adapter>\" for the agent harness this shell runs in.", "",
		func([]string) int {
			h, a := harness.Detect()
			fmt.Println(h, a)
			return 0
		})
	register("names", "branch and worktree names", names.Usage, "", names.Main)
	register("tasks", "the shared task list (smithy tasks board --all)", tasks.Usage, "", tasks.Main)
	registerSub("tasks", "add", "--blocked-by= --key= --speed=")
	registerSub("tasks", "board", "--all")
	register("status", "every task list and batch on this machine",
		"usage: smithy status\nEvery task list and warden batch on this machine (smithy tasks board --all).", "",
		func([]string) int { return tasks.Main([]string{"board", "--all"}) })
	register("cards", "warden status cards", cards.Usage, "--changed-since= --every= --json", cards.Main)
	register("wait", "wait without tokens for PRs, runs, branches, issues or a batch", wait.Usage,
		"--all --events= --every= --max=", wait.Main)
}

// configMain gives the lookups that exit 3 when unset a one-line reason on stderr.
func configMain(args []string) int {
	if len(args) == 2 {
		switch args[0] {
		case "model":
			if _, err := config.Model(args[1]); err != nil {
				switch args[1] {
				case "cheap", "mid", "strong":
					fmt.Fprintf(os.Stderr, "smithy config: no %s model set for this harness; ask the user, then smithy config set-model %s <name>\n", args[1], args[1])
					return 3
				}
			}
		case "get":
			if _, _, err := config.Effective(args[1]); err != nil {
				fmt.Fprintf(os.Stderr, "smithy config: %s is unset; ask the user, then smithy config set %s <value>\n", args[1], args[1])
				return 3
			}
		}
	}
	if len(args) == 1 && args[0] == "get" {
		fmt.Fprintln(os.Stderr, "usage: smithy config get <key>")
		return 2
	}
	return config.Main(args)
}
