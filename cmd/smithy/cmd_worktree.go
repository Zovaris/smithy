package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/names"
)

const worktreeUsage = `usage: smithy worktree add <repo> <branch> <from>
Fetches origin and creates a worktree for <branch>, cut from origin/<from>, at the path
` + "`smithy names worktree`" + ` gives (git.worktree), never inside the repo. Prints the path.
Agents under the unattended profile can't run ` + "`git worktree add`" + `; they use this.`

func init() {
	register("worktree", "create a task worktree at its standard path", worktreeUsage, "", worktreeMain)
}

func worktreeMain(args []string) int {
	if len(args) != 4 || args[0] != "add" {
		fmt.Fprintln(os.Stderr, worktreeUsage)
		return 2
	}
	repo, branch, from := args[1], args[2], args[3]
	for _, v := range []string{branch, from} {
		if v == "" || strings.HasPrefix(v, "-") || strings.ContainsAny(v, " \t\n") {
			fmt.Fprintf(os.Stderr, "smithy worktree: bad branch name %q\n", v)
			return 2
		}
	}
	if _, err := ghx.Run("git", "check-ref-format", "--branch", branch); err != nil {
		fmt.Fprintf(os.Stderr, "smithy worktree: bad branch name %q\n", branch)
		return 2
	}
	path, err := names.Worktree(branch, repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "smithy worktree:", err)
		return 1
	}
	if _, err := ghx.Run("git", "-C", repo, "fetch", "origin"); err != nil {
		fmt.Fprintln(os.Stderr, "smithy worktree: fetch origin:", firstLine(err.Error()))
		return 1
	}
	if _, err := ghx.Run("git", "-C", repo, "worktree", "add", "--no-track", "-b", branch, path, "origin/"+from); err != nil {
		fmt.Fprintln(os.Stderr, "smithy worktree:", firstLine(err.Error()))
		return 1
	}
	fmt.Println(path)
	return 0
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
