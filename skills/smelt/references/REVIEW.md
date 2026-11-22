# Review

1. Resolve the base: `git fetch origin <base>`, then `git merge-base HEAD origin/<base>` (base =
   PR target, else the default branch). Never the local `<base>`: it goes stale in worktrees.
   Reviewers diff the working tree against it, `git diff <merge-base>`, so committed and
   uncommitted work are both reviewed (`git add -N <new files>` first so new files show).
   Empty diff → say so and stop.
2. Find the spec: the task/spec path you were given, commit messages, the PR body, or
   `docs/specs/`. None → spec-check reviews against the PR description and says so.
3. Launch the reviewer in the same wave as the test agents (SKILL.md, check), read-only, its
   prompt ending in the report contract (`references/TESTING.md`). One subagent gets both
   `agents/spec-check.md` and `agents/standards.md` and returns both reports one after the
   other; at `fast` there is no reviewer subagent, you run the spec-check checklist yourself:
   - `agents/spec-check.md`, with the diff command and the spec path.
   - `agents/standards.md`, with the diff command and the paths of the
     repo's rules (AGENTS.md / CLAUDE.md, CONTRIBUTING.md, `docs/agents/STANDARDS.md`, lint config); with none of
     those, the ground rules in use-smithy section 3 and the language's formatter and linter.
     Lines that `smithy decide rules --base <merge-base>` prints (nothing when it is off) go
     with them as leads to check, not findings. When on, it sends the diff and rule lines to the
     endpoint the user configured.
   - `agents/security.md`, as its own subagent in the same wave, at `full` and at any speed when the diff touches auth, money,
     personal data, input parsing, file or shell access, or secrets. Its high findings are the
     user's to see before merge.
4. Show the reports separately, as returned (a combined reviewer's two reports stay two). Don't merge, dedupe or rerank them: they answer
   different questions. Blocking: a spec-check `missing`, `partial` or `wrong`; a failing holdout, adversary or
   regression test; a violation of a written repo rule (`docs/agents/STANDARDS.md`, else AGENTS.md /
   CLAUDE.md / CONTRIBUTING.md). A high security finding
   is shown to the user. Every other finding is advisory.
5. Handle each finding with the receiving-review rules: verify, then fix (bug findings: failing
   test first) or decline. Give every finding a disposition in the PR body (or the final message):
   `fixed`, `declined: <why>`, `false positive` or `not reproduced`.
6. **The reviewer closes, not you.** For a blocking finding or a high security finding you fixed,
   resume the same reviewer (your harness's message or resume to that subagent) with the fixes'
   diff and `smithy receipt list` output, asked only "is each finding resolved, and did the
   fixes add a new problem?". It already has the context, so this is cheap. Where the harness
   can't resume a subagent, or the resumed reply repeats its earlier report instead of judging the
   fixes, send one fresh subagent with the same files plus the fixes' diff. At most two such
   rounds: a finding still open after the second goes to the user, or, with nobody to ask (hush,
   a launched session), into the PR's "Decided without you" as an open risk with its repro, and
   you move on; a high security, money or data finding still stops with a `blocked:` line. A new problem the re-review finds in a fix is a new finding, not another round of
   the old one. Advisory findings and fixes for them need no look.

Reviewing someone else's PR (`/smelt review <pr>`): same steps, report only; post comments only
when the user asks.
