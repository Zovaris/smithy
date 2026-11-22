# warden

Many tasks at once. Each task gets one silent worker in its own git worktree, ending in its own
PR (or a report). warden supervises them from small status cards and a watcher that costs no
tokens, and only talks to you for decisions that are yours.

Started by you (by name, the way your harness starts skills) or by an agent after your yes.

## Commands

| Command | Does |
|---|---|
| `/warden <tasks>` | One task per line, a task dir, board ids, or an idea it offers to plan with smelt |
| `/warden status` · `status all` | The current batch, or every task list and batch on this machine |
| `/warden cleanup` | Re-verify waiting PRs after the batch, end their workers |
| `/warden doctor` | Checks git, GitHub access (gh, a token or neither), the agent CLI, models and permissions for this harness |

## How a batch runs

1. Reads the tasks, their dependencies and the base branch; shows the plan in one message.
2. Launches every worker in your turn; tasks that must wait (dependencies, `warden.max`) wait in
   their own session without tokens.
3. Each worker builds with smelt at `fast` (no agents), opens a PR and ends; warden checks the whole batch in one wave after the merges.
4. Warden waits on every PR's CI at once and merges when you said `auto` (it asks once per batch
   unless `warden.merge` is set); red CI goes back to the worker.
5. Finished worktrees and branches are removed.

## Settings

`warden.max` (5), `warden.mode` (`smelt normal`: workers build at `fast`, then one check wave on the whole batch), `warden.merge` (unset: warden asks once per batch),
`warden.cleanup` (`auto`), `warden.worker_model` (`route`). See [settings](../../docs/settings.md).
