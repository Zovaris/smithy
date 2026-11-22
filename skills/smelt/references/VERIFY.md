# Verify

No claim without fresh evidence. "Should work", "looks right" and "the agent said it's done" are
not evidence. This gate closes build, test, review and ship.

## Gate

Before saying something is done, fixed, passing or ready:

1. **Name the claim** in one line ("the suite passes", "the bug is fixed").
2. **Pick the evidence** from the table below.
3. **Produce it now**, in this context: run the command, read the diff. Output from earlier in the
   session, or from another agent, doesn't count. Run test, typecheck and lint commands through
   `smithy receipt run --claim "<claim>" -- <command>` on a committed tree: it records the exit
   code, an output digest and the commit, so the PR can show the receipt instead of your word.
   Red-step evidence (a test failing before the code exists) may be taken on a dirty tree (an
   untracked file outside the task and spec dirs counts as dirty): the receipt is recorded as `dirty` and is valid for the red
   step only.
4. **Read the result**: exit code, failures, skipped tests, warnings you'd otherwise ignore.
5. **State the claim with its evidence** (command → result), or state what is still failing.

## Claim → evidence

| Claim | Required evidence |
|---|---|
| Tests pass | the full suite run just now, exit 0, no new skips |
| Bug fixed | the regression test failed before the fix and passes after (both runs shown) |
| Types / lint clean | typecheck and lint commands from `smithy stack`, exit 0 |
| Criterion met | a test that exercises it, named, passing |
| It runs | for a change to something that starts (server, CLI, worker), with the `dev` line of `smithy stack` or the binary's main entry (`go run ./cmd/…`, `node bin/…`): start it once and hit the changed path (one request or invocation), output shown. Green tests don't prove it boots |
| An agent or worker completed a task | the VCS diff it produced (`git diff`, `git log -p`), read, plus its tests passing when you run them |
| Refactor safe | suite green before and after, no snapshot or assertion changed |
| Ready to open a PR | `smithy scan` clean (secrets, conflict markers; each binary warning looked at), `smithy receipt list` fresh for HEAD, the diff matches the task |
| PR ready to merge | the above, and `smithy verify <repo-dir> <branch> <pr-url>` says `green` |
| Docs updated | the docs diff, and every symbol it names still exists |

## Red flags

| Thought | Reality |
|---|---|
| "It passed a minute ago" | Code changed since. Run it again. |
| "The subagent reported success" | Reports aren't diffs. Read the diff, run the tests. |
| "Only the one test file matters" | The full suite is the claim. Run it. |
| "That failure is unrelated / flaky" | Prove it: re-run, or show it fails on the base branch too. |
| "Typecheck is slow, it's fine" | Then it isn't verified. Say so instead of claiming it. |
| "I'll verify after the PR" | CI is not your verifier. Verify, then open the PR. |
| "The receipt is stale, but nothing important changed" | Then rerunning it costs nothing. Rerun it. |
