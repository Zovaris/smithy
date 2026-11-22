# warden

Many tasks at once. Each task gets one silent worker in its own git worktree, ending in its own
PR (or a report). warden watches small status cards with a watcher that costs no tokens and
talks to you only for decisions that are yours. Started by you, or by an agent after your yes.

| Command | Does |
|---|---|
| `warden <tasks>` | One task per line, a task dir, board ids, or an idea it offers to plan with smelt |
| `warden status` · `status all` | The current batch, or every task list and batch on this machine |
| `warden cleanup` | Re-verify waiting PRs after the batch, end their workers |
| `warden doctor` | Checks git, GitHub access, the agent CLI, models and permissions |

A batch: plan in one message → every worker launched in your turn (dependents wait without
tokens) → each builds at smelt `fast` and opens a PR → one check wave on the whole batch →
merges when you said `auto` → worktrees and branches removed.

Settings: `warden.max` (5), `warden.mode` (`smelt normal`), `warden.merge` (asked once per batch),
`warden.cleanup` (`auto`), `warden.worker_model` (`route`). See [settings](../../docs/settings.md).
