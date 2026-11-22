# Verify

No claim without fresh evidence ("should work" is not evidence). This gate closes build, check, ship.

1. **Name the claim** in one line.
2. **Pick the evidence** from the table.
3. **Produce it now**, in this context (not earlier or another agent's output). Test,
   typecheck and lint run as `smithy receipt run --claim "<claim>" -- <command>`; before
   ship they may run on the working tree, and "Ready to open a PR" needs them re-run on the
   committed HEAD (`references/SHIP.md`).
4. **Read the result**: exit code, failures, skips, warnings.
5. **State claim → command → result**, or what still fails.

| Claim | Evidence |
|---|---|
| Tests pass | full suite just now, exit 0, no new skips |
| Bug fixed | regression test failing before the fix, passing after (both runs) |
| Types / lint clean | `smithy stack` typecheck and lint, exit 0 |
| Criterion met | a named, passing test that exercises it |
| It runs | something that starts (server, CLI, worker): start it once (`dev` line of `smithy stack`, or `go run ./cmd/…`, `node bin/…`) and hit the changed path, output shown |
| An agent did it | its VCS diff, read, plus its tests passing when you run them |
| Refactor safe | suite green before and after, no snapshot or assertion changed |
| Ready to open a PR | `smithy scan` clean (each binary warning looked at), `smithy receipt list` fresh for HEAD (the holdout receipt may be behind, `references/CHECK.md`), diff matches the task; a bug fix also `smithy receipt check-red` (repro red on an earlier commit, green now) |
| PR ready to merge | the above, and `smithy verify <repo-dir> <branch> <pr-url>` says `green` |
| Docs updated | the docs diff, every symbol it names still exists |

| Thought | Reality |
|---|---|
| "It passed a minute ago" / "the receipt is stale but fine" | Code changed; rerun. |
| "The subagent reported success" | Read the diff, run the tests. |
| "That failure is unrelated / flaky" | Prove it: re-run, or show it fails on the base too. |
| "I'll verify after the PR" | CI is not your verifier. |
