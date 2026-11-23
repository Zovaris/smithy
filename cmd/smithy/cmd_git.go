package main

import "github.com/Zovaris/smithy/internal/gitx"

func init() {
	register("commit", "atomic commits in the repo's message style", gitx.CommitUsage, "", gitx.CommitMain)
	register("pr", "pull requests that follow the repo's PR template", gitx.PRUsage,
		"--base= --title= --body-file= --draft", gitx.PRMain)
	registerSub("pr", "create", "--base= --title= --body-file= --draft")
	registerSub("pr", "merge", "--rebase --merge --squash")
	register("cleanup", "remove a finished task's worktree and local branch", gitx.CleanupUsage, "--force --drop-holdout", gitx.CleanupMain)
	register("verify", "proof for a worker's PR claim", gitx.VerifyUsage, "", gitx.VerifyMain)
	register("pr-state", "compact JSON state of one PR (tend)", gitx.PRStateUsage, "--from", gitx.PRStateMain)
}
