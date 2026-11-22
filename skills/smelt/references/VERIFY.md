# Verify

No claim without fresh evidence ("should work" is not evidence). This gate closes build, check, ship.

1. **Name the claim** in one line.
2. **Pick the evidence** from the table.
3. **Produce it now**, in this context (earlier output or another agent's doesn't count). Test,
   typecheck and lint run as `smithy receipt run --claim "<claim>" -- <command>` on a committed
   tree. Red-step evidence may come from a
   dirty tree; that receipt is valid for the red step only.
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
| Ready to open a PR | `smithy scan` clean (each binary warning looked at), `smithy receipt list` fresh for HEAD, diff matches the task |
| PR ready to merge | the above, and `smithy verify <repo-dir> <branch> <pr-url>` says `green` |
| Docs updated | the docs diff, every symbol it names still exists |

| Thought | Reality |
|---|---|
| "It passed a minute ago" / "the receipt is stale but fine" | Code changed; rerun. |
| "The subagent reported success" | Read the diff, run the tests. |
| "That failure is unrelated / flaky" | Prove it: re-run, or show it fails on the base too. |
| "Typecheck is slow" | Then say it isn't verified. |
| "I'll verify after the PR" | CI is not your verifier. |
