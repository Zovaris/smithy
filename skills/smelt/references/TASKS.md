# Tasks

Contents: Where it lives · Splitting · Task file — `NN-<slug>.md` · Commands · After the list is written
The task list is the hand-off between planning and building, smelt and warden. `smithy tasks`
reads and updates it; use it instead of hand-editing status lines.

## Where it lives

An agreed spec in a folder of its own: that folder is the task dir (no second folder). A spec at the repo root stays there: make the task dir with `smithy task-dir` and point `Spec:` at it. Otherwise run
`smithy task-dir <slug>`: it reads the user's settings, creates the dir and prints it.
Exit 3 prints `ask <key>: <question> (default: <value> → <dir>)`: ask the user (recommendation first), save it with `smithy config set --repo <key> <value>`, rerun.
Nobody to ask (hush, a launched session): `smithy task-dir <slug> --default` takes the defaults unsaved; say so at the end.
It asks at most twice per repo, ever (`.smithy/config.json` stays out of git; `git rm --cached` it if committed):

1. `tasks.commit` — commit the specs and tasks to git? `true` → `docs/specs/<slug>/`, reviewed
   with the code. `false` → `.docs/specs/<slug>/`, next to the code but out of git.
2. `tasks.ignore` (only when `false`) — how `.docs/` stays out of git: `gitignore` adds it to
   `.gitignore` (commit that one line with `smithy commit`, so the whole team ignores it) or
   `exclude` adds it to `.git/info/exclude` (this machine only, nothing committed).

`tasks.location` (default `repo`) switches the whole thing: `local` puts plans in
`~/.smithy/plans/<repo>/<slug>/` (`$SMELT_PLANS`), `board` puts tasks on the user's external tracker
(GitHub Issues via `gh` when installed, or Linear/Jira through the harness's tools) and keeps the spec where
`tasks.commit` says. `smithy config list` shows the current values.

Other places: list them in `$SMELT_TASKS_DIRS` (colon-separated). Board tasks keep the same fields
(status, blocked by, PR) in the issue body or labels; the spec stays in the repo or local dir and the issues link to it.

## Splitting

- **Tracer bullets:** each task is a thin vertical slice through every layer that is demoable on
  its own, not a horizontal layer.
- **Prefactor first:** "make the change easy, then make the easy change". Wide renames or
  migrations use expand → migrate → contract tasks.
- **Fewest tasks that work.** One task per part that can be built and tested on its own; a
  CLI or a feature one agent builds in under an hour is 1 to 3 tasks, not one per file. Each task
  costs a warden worker and a PR (or a slice of `build all`'s one PR), so split only where it buys real parallel time.
- Each fits one fresh context window (nothing bigger than ~1 day of human work).
- `Blocked by` only for real dependencies (one task needs another's code or schema). Tasks that
  merely touch nearby files are not blocked; tasks that would edit the same lines are.
- **Width over tidiness:** tasks run in parallel, so the wall time is the longest chain, not the
  task count. Shape the split so the chain is short: one shared foundation first (skeleton, CI,
  a schema or interface), then as many independent tasks as possible, each coding against the
  agreed interface rather than waiting for its implementation. A chain longer than 2 (3 if
  the first is a skeleton/CI task shipped as its own PR) is a finding: say so with the list
  and how to shorten it. Parallel only pays when at least two independent tasks are each big (more than ~15 minutes of agent work).
- **Self-review** before showing it:
  1. Coverage: every acceptance criterion of the spec lands in exactly one task.
  2. Interfaces line up: each `Consumes` is `Produces`d by a blocker or already exists.
  3. Collisions: unblocked tasks share no file, test files too (each task gets its own).
  4. Proportion: no task is a horizontal layer, none is bigger than a day, none is trivial
     enough to merge into its neighbour. A test of a stub never asserts the stub's output.
  5. Width: the longest `Blocked by` chain and each task's `Speed:`.
- Show the list (number, title, one line, blocked by); wait for approval (else SKILL.md Always).

## Task file — `NN-<slug>.md`

```markdown
# NN · <title>
Status: todo
Blocked by: <NN, NN | none>
Speed: <fast | normal | full>
PR: -
Spec: <relative path to the spec: ./spec.md, or the agreed spec where it already lives>

## What
<one paragraph>

## Acceptance criteria
- [ ] <from the spec, narrowed to this slice>

## Files
- create: <path> · modify: <path> · test: <path>

## Interfaces
- Consumes: <functions, types, endpoints, tables it relies on, with signatures>
- Produces: <what it adds or changes for later tasks, with signatures>

## Notes
<seams, risks>
```

`Speed:` defaults to fast (a smelt session: normal): each task is small, and the check wave runs
once, on the branch or batch, at the highest `Speed:` in the list (`build all`) or the batch's
mode (warden). Set normal or full only on a task that holds logic. Files and Interfaces let a
fresh implementer start without reading the plan and show where two tasks would collide.

Status moves `todo → doing → done` (or `dropped` with a line saying why). Whoever starts a task
sets `doing`; whoever lands it sets `done` and `PR`. In a repo task dir the `done` + `PR` change is
committed in the task's own branch, so it lands with the PR; nothing is committed to the base
branch just to move a status. Uncommitted dirs (`.docs/specs`), local dirs and boards are
updated directly.

## Commands

```
smithy tasks list  [dir]          # the board: status, blockers, PR per task
smithy tasks next  [dir]          # first todo whose blockers are done
smithy tasks ready [dir]          # every todo that can start now (what warden launches)
smithy tasks set <file> status=doing|done pr=<url>
smithy tasks where                # task dirs for this repo (repo and local)
smithy tasks add "<title>" [dir] [--blocked-by 01] [--key K] [--speed fast|normal|full]  # next NN file
smithy tasks import <list.md> [dir]  # one file of `## NN · Title` sections → NN files
smithy tasks board [--all]        # one line per task list and batch: this repo, or every repo
```

## After the list is written

Ask how to run it (nobody to ask: SKILL.md Always), recommendation first, only offering what is
installed. Recommend **here** unless at least two independent tasks are each big (more than ~15
minutes of agent work): for a small list or a long chain, one session building in order is
faster and several times cheaper than parallel workers.
- **Here, all of it:** smelt `build all`: built in order, one check wave on the whole branch at
  the end.
- **Here, one at a time:** smelt `build next` (repeat until `smithy tasks next` exits 3).
- **In parallel:** warden on the task dir (workers build at `fast`; warden checks the batch once
  at its speed; read the SKILL.md `smithy skills warden` prints) when it is installed (listed
  among your skills, or `<skill-dir>/../warden/SKILL.md` exists). Mixing is fine: build some here,
  hand the rest over; both read and write the same status lines.
- **Later:** stop here; the list waits.
