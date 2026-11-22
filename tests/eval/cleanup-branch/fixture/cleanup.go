// Package cleanup reports the outcome of cleaning up a finished branch.
package cleanup

import "fmt"

// Result returns the message and exit code for a cleanup of branch. removedWorktree says a
// worktree was removed; branchExists says the local branch is still there.
func Result(branch string, removedWorktree, branchExists bool) (string, int) {
	if !branchExists {
		return "cleaned " + branch, 0
	}
	return fmt.Sprintf("kept %s: branch still exists", branch), 1
}
