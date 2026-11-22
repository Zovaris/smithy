`Result` says "cleaned" for a branch that never existed. When the branch does not exist and no
worktree was removed there was nothing to clean: return the message `no branch <name>` and exit
code 1. Removing a worktree whose branch is already gone still counts as cleaned (code 0). Keep
the other results as they are. Add a test.
