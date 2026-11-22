# Base recipe

Event `base <branch> moved <sha>`: the default branch (or the named one) moved on origin.

List the open PRs you tend (`smithy wait prs` scope: yours,
a warden batch's, or labeled) and run `smithy pr-state` on each. Those whose `action` is
`sync-base` go through the PR recipe now, before their CI goes stale. Nothing else to do; report
`base <branch> <sha>: synced <n> PRs` only when you synced at least one.
