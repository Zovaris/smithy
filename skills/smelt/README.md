# smelt

One engineering task, step by step: brainstorm → spec → task list → test-first build →
independent test agents → review → PR → retro. Start at any phase, at the speed the task needs.

Started by you (by name, the way your harness starts skills) or by an agent through use-smithy.

## Commands

| Command | Does |
|---|---|
| `/smelt <task>` | Picks the entry: no plan → `plan`, a plan or task → `build`, a bug → `debug` |
| `/smelt plan <idea>` | Brainstorm, grill the open decisions, write the spec and the task list |
| `/smelt build next` · `build all` | Build the next ready task, or every task in order (one implementer each when there are more than three) |
| `/smelt <phase> <task>` | One phase: `brainstorm`, `spec`, `build`, `check`, `ship`, `retro` |
| `/smelt from check <task>` | From that phase to the end, e.g. on code you already wrote |
| `/smelt debug <bug>` | Root cause, failing test, fix, regression test, PR |

## Speeds

| | fast | normal (default) | full |
|---|---|---|---|
| For | small or clear changes | most features and fixes | money, auth, data loss, public APIs, or when asked |
| Plan | criteria from the task | a spec if none exists | brainstorm, grill, spec, builder check for large or open specs, tasks |
| Checks (scripts) | suite, typecheck, lint, scan | + property tests you write | + the mutation tool |
| Agents, one wave in parallel | none (you check against the spec); security if risky | adversary + reviewer; security if risky | adversary, property, reviewer, security, e2e for UI or HTTP; mutation after fixes; hostile last |
| Target | ~5 min | ~10–15 min | the safest flow |

Every speed builds test-first and passes the verify gate: no "done" without fresh evidence.

## What you get

- A spec and a task list (`NN-<slug>.md`) in `docs/specs/`, `.docs/specs/` or `~/.smithy/plans/`.
- Atomic commits in your repo's style and a PR filled from your template.
- A branch named by `smithy names branch` (your `git.branch` setting), cleaned up after merge.

Hand the task list to **warden** to build it in parallel.
