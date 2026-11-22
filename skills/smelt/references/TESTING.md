# Test agents

Launch each as a fresh **subagent**: prompt = the agent file's text (or its absolute path, for the agent to read first), a `## You get` block with
exactly what the file lists, and the report contract below. Agents that write tests work in
your tree, in new files named `zz_<agent>` plus the suffix the repo's test runner picks up by
default (`zz_<agent>_test.go`, `zz_<agent>_test.py`, `zz_<agent>.test.ts` for vitest or jest) next to the
existing tests: no WIP commit, no extra worktree (warden's batch check gives them a fresh worktree of the base). Their tests are input, not files to ship: move
the cases worth keeping into the repo's existing test files in its own naming, drop the rest and
any reference to the agent or its spec, and never commit an agent's file as is. The adversary's
file is the holdout (below): keep it untouched until its final run; cases you keep are copies
written after that run. Then delete the `zz_` files. Launch them together with the reviewer, in
one wave. Without a subagent tool: write each prompt to a file and run
`smithy agent --bg --log <file> <prompt-file>` per agent, then read the logs.
Read-only agents (spec-check, standards, security, builder) run as any fresh subagent (no
particular type needed), or with `smithy agent --readonly` without a subagent tool.
Give every agent the absolute path of this skill's `scripts/smithy` as `<smithy>`: agents run in the
repo, not in the skill folder.

Every prompt ends with this **report contract**, since agents see only their prompt:

```
## Report
Work silently: no narration, no questions to the user, never hiding work (list every change and command).
Use only file, shell and git tools (no session, task or messaging tools: this report is your only output). Your report is your only output, in the
shape your file asks for (its status block included), then one last line
`done: <one-line result>`. Anything you can't settle goes in the report as a question with your
recommendation; never stop to ask.
```

Rule tests (a pattern, a prefix list, a token format) must cover every variant the spec names, one case each: a bug that drops one prefix passes a test of two.

All agents of a speed run together in one wave with the reviewer (hostile runs after the fixes). Which ones run depends on the speed (table in SKILL.md); within it, use this to choose.

| Agent | Run when | Cost | Writes tests |
|---|---|---|---|
| `agents/adversary.md` | normal and full, for runtime logic (not docs, glue or test-only diffs) | medium | yes |
| `agents/regression.md` | every fix of a reported bug with a repro (a review that finds and fixes bugs gets the adversary, not both) | low | no (runs the fixer's test) |
| `agents/property.md` | full, for parsers, serializers, math, money, sorting, state machines (at normal you write the property tests yourself) | medium | yes |
| `agents/mutation.md` | full, after you run the mutation tool from `smithy stack` yourself: the agent reads only the surviving mutants | high CPU | proposes |
| `agents/e2e.md` | full, when UI flows or HTTP endpoints changed and the app can run locally | high | yes |
| `agents/hostile.md` | full, last, on a runnable CLI, API, worker or job that takes outside input, retries, writes state or calls dependencies | high | no (reports) |
| `agents/spec-check.md` + `agents/standards.md` | one reviewer subagent at normal and full; at fast you run the spec-check checklist yourself | low | no |

Hostile runs last, against a build of the fixed commit. Any fix
it triggers goes through build (failing test first) and review again; its findings close on the
re-run of its repro, not on your word.

Characterization: before a refactor of untested code, snapshot current behaviour first
(vitest/jest `toMatchSnapshot`, Python syrupy/approvaltests), refactor, require zero diffs.
Never update snapshots in the same pass.
When the old version still runs, a differential check is stronger: feed old and new the same
generated inputs and require equal outputs.

## What each agent must not get

Your conversation, plan, reasoning or opinion of the code. Give the spec/criteria, the public
interface (signatures, types, routes), the test commands from `smithy stack`, and the changed files
list only where the file says so. Independence is the point: an agent that knows how you thought
shares your blind spots.

## Handling reports

Every finding comes with a command and its output. For each:
1. Re-run it yourself. Not reproduced → drop it (note it).
2. Spec violation → keep the failing test, fix the code (the test is the red step).
3. Spec ambiguous → decide (or ask the user when it's theirs), update the spec, then 2 or drop.
4. Test is wrong → reject with one line.

Reject outright any change that weakens an assertion, deletes or skips a test, or edits source.

**The adversary's tests are a holdout.** They stay in the adversary's `zz_` file, which you
run but never read to write code (nor show the implementer or builder): you fix from the finding (input, expected, actual), not from
the test file. After the fixes, run the adversary's tests once more against the final commit and
record that run as its own receipt (`smithy receipt run --claim "holdout" -- <command>`). Passing
the visible tests but failing the holdout is a blocking finding.

## Deterministic gates (no agent needed)

Prefer scripts and hooks for what they can decide: typecheck, lint, full suite, coverage on
changed lines (`diff-cover` for Python or any lcov; `vitest --coverage --changed`), flaky checks
(`pytest -p randomly --count 3`, `vitest --sequence.shuffle`, `playwright --repeat-each 3`).
