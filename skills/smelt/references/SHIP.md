# Ship

Docs first: `smithy comments --diff <base>` on the changed files (API doc comments
only, the why goes to docs); with the ledger skill installed, also its `sync` mode on the diff,
else update the docs the change touched (`references/DOCS.md`). Then:
- **Branch:** (made in phase 0) still on the base? `git switch -c "$(smithy names branch <key|-> "<title>")"`
  (`<key>`: the ticket, issue or task `NN`). Already on a branch the harness or the user made
  for this work? Keep it. Never invent another naming scheme.
- **Integrate:** full suite on the tree you're about to ship (after rebasing or merging the
  base), VERIFY gate. Red → back to build.
- **Atomic commits** with `smithy commit` (review read the working tree, so this comes after it;
  the agents' `zz_` files, kept in the system temp dir during check, are deleted first; check
  each `smithy commit` exit code before running receipts): `smithy commit style` once, then one commit per
  concern (`smithy commit "<msg>" <paths>`), each building and passing on its own. Never
  `git add -A`, never `--no-verify`. Prove it before the PR, on a clean tree:
  `git rebase -x "<typecheck> && <test>" origin/<base>`, with the runner's no-tests flag in
  `<test>` (`--passWithNoTests` or its equivalent); run it only when more than one commit
  touches source, from the first commit that has a build file (a docs-only commit is exempt). A red commit gets a fixup, then re-run.
  A skeleton task lands as one buildable commit, so it passes too.
- **Scan** with `smithy scan --base <base>` (a branch name resolves to `origin/<base>`) before
  anything leaves the machine: a finding is removed (and the secret rotated if it was ever pushed)
  or marked `smithy:allow` with the user's yes. Never push past a finding.
- **Close, one of three.** (1) **PR**, the default, with `smithy pr create`: if `smithy pr
  template` finds one, fill every heading (N/A when one doesn't apply); otherwise what and why,
  tests added, commands run with results (paste `smithy receipt list --md`), agents run and what
  they found, agents not run and why, the Spec-check table (fast), "Decided without you" (from `rulings.md` if any), ADRs
  added. Title in the commit style: `smithy pr create --base <b> --title "<t>" --body-file <f>`;
  write the PR body file inside the worktree's `.smelt/` or the system temp dir, never next to
  the worktree. From a task file: once the PR exists, `smithy tasks set <file> status=done pr=<url>`
  and commit that change as a follow-up commit on the PR branch. A warden worker ends
  here (its brief). Alone, wait
  with `smithy wait pr:<pr-url>` (use-smithy's wait rule). Run `smithy verify` only after it ends: right after a push GitHub still shows the old head. A red CI whose log points at this change: fix it here, test-first. Otherwise, when it reports red CI, a
  conflict or a review comment, hand that PR to **tend** (`smithy skills tend` prints its path)
  as one silent **subagent**: "Read <tend-skill> and follow it for <pr-url>; never merge" plus the
  report contract; it fixes that and ends. Waiting never needs an agent. On merge, clean up.
  GitHub modes, one line each: with `gh`, `smithy pr create` opens the PR (best experience);
  without `gh` but with `GH_TOKEN`/`GITHUB_TOKEN`, it uses the GitHub API; with neither, it
  pushes the branch and prints the compare URL: end there, report the branch and URL, and skip
  the wait (the user opens the PR by hand). Merge with `smithy pr merge <pr>`, never
  `gh pr merge`.
  (2) **Merge** the PR or locally into the base only when the user asks (use-smithy's merge rule). (3) **Keep** the branch for later. Discard
  work only when the user explicitly says to.
- **No remote, or not GitHub** (`git remote` prints nothing, or `smithy pr create` exits 3:
  origin is another forge or a local repo): commit, branch, receipts and scan as above, then
  skip the PR and the wait, and say so in the final
  message (a warden worker: `state=ready note=no-github-pr`).
- **Clean up** once merged or discarded: `smithy cleanup <repo> <branch> [<worktree>]`,
  only for worktrees and branches smelt created. It keeps work that is on no remote and no other branch; never force it.
  The merge usually comes later, from the user: `smithy cleanup` then, and it never removes a
  worktree it isn't given (only the ones smelt created); end the test and review agents as soon as their
  reports are in, and if this session was started for the task, say in the final message that it
  can be archived once the PR merges.
- **Lesson.** Read the task dir's `rulings.md` (when present) and the review findings, then
  propose at most 3 lessons, each citing evidence: a receipt, a failing test, a review finding,
  a `file:line` or a ruling. No evidence → not proposed; your own opinion is not evidence. Skip what is already in the
  code, a test or the repo's docs.
  A lesson that can be a lint rule, a test or a script is proposed as that, not as prose. Each
  goes through the retro rules (`references/DOCS.md`) and nothing is written without the user's
  yes; with nobody to ask (hush included) they are listed in the report and "Decided without
  you", and nothing is written. Nothing with evidence → say
  "no lesson".
