# PR recipe

## Loop

Read the state first; a worktree is only for changing code. When a step needs one, use the PR
branch's existing worktree if `git worktree list` shows one. If the branch is checked out in the
user's main checkout, work there only when it is clean and on that branch; otherwise stop with
"needs you" rather than switching it. Else add one at `smithy names worktree <pr-branch>` (never
inside the repo). Then repeat:

1. `smithy pr-state <pr>` → `{state, checks, mergeable, review, unresolved, behind, action}`;
   `checks` is `green`, `red`, `pending` or `none`, the same words `smithy wait` and `smithy verify` use.
2. Do what `action` names (it is the first row below that applies; the rows say how):
   - **merged / closed** (`cleanup`) → stop this PR. Remove the worktree you created, and the branch and
     worktree the launcher names as its own (`smithy cleanup <repo> <branch> [<worktree>]`,
     never forced); report `merged <url>, cleaned <what>` so the launcher can end its sessions.
   - **checks red** (`fix-ci`) → `gh run view <run> --log-failed` when `gh` is installed (else open the failing check's page from the PR). Same test passes on a plain rerun and
     nothing in the diff touches it → flaky: rerun once (`gh run rerun <run> --failed` when `gh` is installed) and note
     it. Otherwise it is a real failure: hand it to the fix model, failing test first when it is
     a bug, and push the fix. Red again after a second real fix → "needs you".
   - **conflicting or behind base** (`sync-base`) → follow the repo: rebase if its history is linear (no merge
     commits on the base in `git log --merges -5`), else merge the base. A rebase needs a force
     push: `--force-with-lease` only on branches of a warden batch you were given, or of a PR the
     user asked you to tend by name; on anyone else's branch, merge the base instead or stop
     with "needs you". Conflicts you can't resolve from both sides' intent → "needs you".
   - **unresolved review threads** (`answer-review`, human or bot) → `references/RECEIVING.md`. At most 3 bot rounds
     per PR (a person you disagree with twice → "needs you"); the count lives in the card's `review_rounds` (`smithy cards set <batch> <id>
     review_rounds=<n>`); without a card, in `~/.smithy/tend/<owner>-<repo>-<n>.rounds`.
   - **checks pending** (`wait`) → `smithy wait pr:<pr-url>` in the background (foreground where the
     harness can't wake on exit); it returns one line when checks finish, the PR merges or closes,
     or a review lands. Exit 0: read the state again. Exit 4 (heartbeat): wait again. Exit 2 or 3
     (bad target, no gh and no token, or signed out): "needs you". Never sleep-then-check turns.
   - **changes requested or review still required with no open thread, or blocked** (`needs-you`)
     → "needs you <url>: <why>" (a card: `state=blocked`).
   - **green, mergeable, approved or no review required** (`done`) → `green` (`smithy verify` says `draft`
     for a draft PR: report `green (draft)` and leave it a draft). Merge (`smithy pr merge <pr>`) only if whoever launched
     you said `merge auto`, never a draft, and into the production branch only when the launch
     says the user approved it (use-smithy: merges); otherwise stop at green (or keep waiting when
     told to stay until merged).
3. Commit fixes with `smithy commit` (style once, one concern per commit); push without force
   unless the rebase rule above allows it.

## When a PR doesn't get green

Tend fixes red PRs itself; it hands a PR back only when it hits something it must not decide:

| Situation | Card (warden launched you) | Without warden |
|---|---|---|
| Red twice after real fixes, or a conflict it can't resolve safely | `state=blocked note=<why>` | `needs you <url>: <why>` |
| A technical choice inside the PR's scope (which fix, which approach) | settled with `smithy consult` (the user's judge model (else strong)), noted in the commit or reply; `ESCALATE` → next row | same |
| A decision the user owns (scope, API, data, security) | `state=needs_decision` with question, options, recommendation, `impact=high` | `needs you <url>: <question>` |
| 3 review rounds used, or a force push it isn't allowed to do | `state=blocked note=<why>` | `needs you <url>: <why>` |
| Green | `state=pr_open pr=<url>` (or `ready` for manual merge), `ci=green` | `green <url>` |
| Merged / closed | `state=merged` / `state=dropped` | `merged <url>` / `closed <url>` |

Warden picks the card up on its next wake and escalates it to the user with the others; tend
moves on to the next PR instead of waiting.
