package main

import (
	"github.com/Zovaris/smithy/internal/cleanup"
	"github.com/Zovaris/smithy/internal/gitx"
	"github.com/Zovaris/smithy/internal/uninstall"
)

func init() {
	register("commit", "atomic commits in the repo's message style", gitx.CommitUsage, "", gitx.CommitMain)
	register("pr", "pull requests that follow the repo's PR template", gitx.PRUsage,
		"--base= --title= --body-file= --draft", gitx.PRMain)
	registerSub("pr", "create", "--base= --title= --body-file= --draft")
	registerSub("pr", "merge", "--rebase --merge --squash")
	register("cleanup", "remove a finished task's worktree and local branch, or old builds, images and worktrees", gitx.CleanupUsage+"\n"+cleanup.Usage, "--force --drop-holdout --yes", cleanupMain)
	register("uninstall", "remove smithy: binaries, the skills it installed, daemon, images, PATH line (lists first)", uninstall.Usage, "--yes -y --purge --confirm-purge", uninstall.Main)
	register("verify", "proof for a worker's PR claim", gitx.VerifyUsage, "", gitx.VerifyMain)
	register("pr-state", "compact JSON state of one PR (tend)", gitx.PRStateUsage, "--from", gitx.PRStateMain)
}

// cleanupMain sends `cleanup builds [--yes]` to the builds cleanup; anything else is a task's cleanup.
func cleanupMain(args []string) int {
	if len(args) > 0 && args[0] == "builds" && (len(args) == 1 || len(args[1]) > 1 && args[1][:2] == "--") {
		return cleanup.Main(args[1:])
	}
	return gitx.CleanupMain(args)
}
