# Execute a task list here

`/smelt build all [dir]` works every task of a task list in this session. Three tasks or fewer,
or under ~1 hour of work in all: build them yourself in order, no implementer subagents. More:
one fresh-context implementer agent per task, so your own context only holds briefs, diffs and
rulings. For tasks that can run at the same time in separate worktrees and PRs, warden on
`<dir>` (read the SKILL.md `smithy skills warden` prints) is the parallel alternative.

## Models

Each implementer agent gets the model `smithy route --file <task file> --speed <speed> --domain
"<the product's nouns, e.g. secret for a secret scanner>"` picks
(cheap, mid or strong, from the task's risk, difficulty and size); tasks you build yourself
skip it. Each check agent gets `smithy route --agent <name>` (the adversary and security run on
at least mid); the last fix round uses strong. You may move a pick one tier with a reason (a
domain noun, like "secret" in a secret scanner, is not risk), never below the `--agent` floor;
a model the user named always wins. A tier whose model is unset prints `-`: use-smithy's model-tier rule.

## Loop

`smithy tasks list <dir>` first; work `smithy tasks next <dir>` until it says all done.
For each task:

1. `smithy tasks set <file> status=doing`.
2. **Brief.** Prompt = `agents/implementer.md` + `## You get`: the task file's path and text, the
   spec path, the test commands from `smithy stack`, the files and interfaces the task lists, and
   the rulings so far that touch it, and the report contract (`references/CHECK.md`). Never your
   conversation.
3. **Status.** The implementer ends with one of:
   - `DONE` → step 4.
   - `DONE_WITH_CONCERNS` → read the concerns, decide each (ruling), then step 4.
   - `NEEDS_CONTEXT` → answer from the spec or code (ruling) and send it back; if only the user
     can answer, ask (under hush, stop with a `blocked:` line).
   - `BLOCKED` → `smithy tasks set <file> status=todo`, note why, move to the next unblocked task.
4. **Verify** (the verify gate): read the diff, run the task's tests (full suite: `references/TDD.md`).
5. **Check** the task's diff against the task file with the Spec-check list of `agents/reviewer.md` yourself, at
   every speed. The speed's agents run once, on the whole branch, after the last task.
6. **Fix loop**: send the reproduced findings back to a fresh implementer with the diff. At most
   3 rounds; round 3 uses the strong model. Still failing → `BLOCKED` with the findings.
7. Commit the task atomically (`smithy commit`); every commit builds and passes its tests, so a
   file a later task fills in starts as a stub here. Its `status=done pr=<url>` is set at ship, once
   the PR exists.

The branch ships as one PR, or two when a skeleton/CI task ships first as its own PR so CI
exists on the base before the rest.

After the last task, at the highest `Speed:` among the tasks: `fast` → ship; `normal` or `full` →
one check wave on the whole branch (SKILL.md's table, its agents in parallel), one fix wave,
the re-review (`references/RECEIVING.md`, step 5), then ship.

## Rulings

Decisions that come up without the user, by impact:
- **low** (local, cheap to undo): decide yourself.
- **medium** (shapes a module or an internal contract): `smithy consult "<question>"
  "<options, neutral order>" [--context <file>]`, one tool-less call to the user's judge model (else strong)
  in whatever harness you're in. Follow its pick (the call fails → decide yourself); `NEED-INFO` → find that out and ask again;
  `ESCALATE` → it's high.
- **high** (scope, customer-visible behaviour, money, data, security, public API or schema,
  production, irreversible): the user's, by use-smithy's **Decisions**: ask with the
  `ask.timeout` timer, then consult and `smithy decisions add`; its hard floor waits for the user.

Every decision made without the user is one line in `<dir>/rulings.md`:
`- NN · <question> → <choice> (why)`, with its exact boundary ("multi-column rows only"): agents grade against the words. Rulings bind later tasks (pass the relevant ones in each
brief) and become the PR's "Decided without you". A ruling that turns out to be the user's call
goes to them before ship.
