# Check: one wave of agents, then review

## 1 · Prepare
1. Base: `git fetch origin <base>`, then `git merge-base HEAD origin/<base>` (base = PR target,
   else the default branch; never the local `<base>`). Diff = `git diff <merge-base>` after
   `git add -N <new files>`. Empty diff → say so and stop.
2. Spec: the task/spec path, commit messages, the PR body or `docs/specs/`. None → the reviewer
   uses the PR description and says so.

## 2 · Launch, all at once
Each agent the speed lists (SKILL.md) runs as a fresh agent, in parallel, in one wave. Prompt =
its `agents/<name>.md` text (or absolute path), a `## You get` block with exactly what that file
lists, and the report contract below. Never your conversation, plan or opinion of the code
(it would share your blind spots). Give every agent `<smithy>`, the
absolute path of this skill's `scripts/smithy`. Without a subagent tool: write each prompt to a
file and run `smithy agent --bg --log <log> <prompt-file>` (read-only ones with `--readonly`),
then read the logs.

| Agent | Runs | Writes tests |
|---|---|---|
| `agents/reviewer.md` | normal, full: one read-only agent, both reports (a reply missing one goes back once); fast: you run its Spec-check | no |
| `agents/adversary.md` | normal, full, runtime logic only | yes, holdout |
| `agents/regression.md` | a reported bug with a repro, instead of the adversary | runs the fixer's test |
| `agents/security.md` | full, and any speed when the diff is risky (SKILL.md) | no |
| `agents/property.md` | full: parsers, serializers, math, money, sorting, state machines | yes |
| `agents/mutation.md` | full, after you run the mutation tool from `smithy stack`: reads surviving mutants | proposes |
| `agents/e2e.md` | full, UI flows or HTTP endpoints changed and the app runs locally | yes |
| `agents/hostile.md` | full, last, on the fixed commit of a runnable CLI, API, worker or job | no |

Report contract, the end of every prompt:
```
## Report
Work silently: no narration, no questions to the user, never hiding work (list every change and command).
Use only file, shell and git tools (no session, task or messaging tools: this report is your only output),
in the shape your file asks for (with its status block, if it has one), then one last line
`done: <one-line result>`. Anything you can't settle goes in the report as a question with your
recommendation; never stop to ask.
```

Test agents write `zz_<agent>` files with the runner's default suffix (`zz_<agent>_test.go`,
`zz_<agent>_test.py`, `zz_<agent>.test.ts`) next to the existing tests, in your tree: no WIP
commit, no extra worktree (exception: warden's batch check gives them a fresh worktree of
`origin/<base>`). Copy the cases worth keeping into the repo's own test files and
naming, then delete every `zz_` file; never commit one as is. Rule tests (a pattern, a
prefix list, a token format) cover every variant the spec names, one case each.

**Holdout.** You run the adversary's file but never read it to write code (nor show it to the
implementer): fix from the finding (input, expected, actual). After the fixes, run it once on the
final tree: `smithy receipt run --claim holdout -- <single-file command from smithy stack>
<zz file>`. Failing it is blocking, unless its finding was declined as spec-ambiguous: then
one line in the PR's dispositions closes it. Copies of its cases are written only after that
run. Typecheck and lint receipts run with the `zz_` files moved out of the tree (or excluded).

## 3 · Read the reports
Show each report as returned (the reviewer's two stay two): never merge, dedupe or rerank.
- **Blocking:** spec-check `missing`, `partial` or `wrong`; a failing holdout, adversary or
  regression test (unless declined as spec-ambiguous, holdout rule above); a violation of a written repo rule. A **high security** finding is shown to the user.
- Everything else is advisory.
- Scripts first: coverage on changed lines (the coverage command `smithy stack` prints, if any),
  shuffle/repeat runs (`pytest -p randomly --count 3`, `playwright --repeat-each 3`). If the old
  version still runs, a differential check (same inputs, equal outputs) beats snapshots.
- Handle every finding by `references/RECEIVING.md`; reject any agent change that weakens an
  assertion, deletes or skips a test, or edits source. Give each a disposition in the PR body:
  `fixed`, `declined: <why>`, `false positive` or `not reproduced`.
- Hostile's findings close on the re-run of its repro; its fixes go back through build and review.

Someone else's PR (`smelt check <pr>`): same steps, report only; comment only when asked.
