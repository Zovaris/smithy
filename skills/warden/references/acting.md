# Acting on cards

Contents: decide · verify · merged · batch check · batch end · reported/dropped · stuck.

One section per stage. Every final item goes through `smithy cleanup <repo> <branch> <worktree>`:
worktree and local branch removed, a clean local base fast-forwarded, unpushed work refused with `kept <branch>: …`, which goes in the
final message instead of being forced. With `cleanup ask`, list what you would clean and end, and
do it on the user's yes.

## decide (`needs_decision`)

By `impact`, with the levels of smelt's `references/EXECUTE.md` (Rulings); you may raise it, never
lower it; `impact_by=decide` means the decision model raised it:
- **low**: decide on the merits.
- **medium**: `smithy consult` with the worker's question and options (neutral, per use-smithy **Decisions**, step 3; fails → decide yourself).
  `NEED-INFO` goes back to the worker; `ESCALATE` makes it high.
- **high** (also: the same kind of finding recurring): the user's. One
  message with all of them; each gives the intent, the proposed change, the smallest alternative
  that still meets the intent, and your recommendation. Then use-smithy's **Decisions** (timer,
  consult, `smithy decisions add … --ref <batch>/<id>`); on its hard floor the item is held until
  the user answers.

Relay answers with **tell**, quoting the user's words when they decided, and close the question
yourself: `smithy cards set <batch> <id> state=working decision_key=`. Log the ones taken without
the user with `smithy decisions add … --ref <batch>/<id>`.

## verify (`pr_open`, `ready`)

Before merging a risky item, `smithy cards gate <batch> <id>` (exit 1 without `security=ran`): never merge it; unset → **tell** its worker to run the security agent; `not-run:<reason>` → list it for the user with the reason. Else `smithy verify <repo> <branch> <pr-url>` prints one verdict:
- `green`: merge auto, and either not the production branch or the user approved a production
  merge for this batch → `smithy pr merge <pr>` (workers never merge: the merge permission is
  the user's, given to you, and a permission check would refuse it from a worker). Use the repo's
  usual merge method; a rebase merge refused because the branch has merge commits → a merge
  commit, never a force-push. Not mergeable (the base moved) → **tell** the worker to merge the base and push.
  Several green PRs that conflict once the first merges: merge one; for each of the rest,
  `gh pr update-branch <pr>` when its worker has finished, else (or on a conflict) **tell** its worker to merge the base, then verify them again. Refused → list
  it with the doctor's permission hint.
  Production without that yes, or merge manual → card `state=ready`, listed for the user.
- `pending`: nothing to start; the running `smithy wait batch:<batch>` also wakes when a card's
  PR turns green, red or conflicting, then verify the ones it names.
- A `ready` card with `note=no-github-pr` has no PR to verify: list its branch for the user.
- `draft`: green but still a draft; list it for the user, never merge it.
- `conflict` (exit 4; this is the batch's one rule for conflicts): the PR can't merge into its
  base. A worker still running gets the merge-base line (**tell**); else hand it to `tend batch
  <batch>`, which syncs the base and stops at green (tend's `references/PR.md`, green); you merge
  after verify says `green` when merge is auto, otherwise the user does.
- `unpushed` or `red`: **tell** the worker in one line (a red check names its failing job); a
  worker that already ended gets the line in its inbox and is respawned.
- `merged`: next section.
- `closed` (closed without merge): `state=dropped note="PR closed without merge"`, `smithy cleanup`,
  **end** the worker, set the source to `dropped`, name it in the final message.
- `no-pr`: the worker said `pr_open` without a PR; back to the worker.

## merged

Only after `smithy verify` says `merged` (never chain merge, cleanup and `cards set merged` without that check): `smithy cleanup`, **end** the worker, update the source. Its
dependents are already waiting and start on their own. A `followup: <file>` in the note is work
found after the merge: small and inside the intent → add it as an item (spawn it if this harness
can from here, else propose it); bigger → a scope question for the user, with the other decisions (decide stage).

## batch check (every ship item `merged` or `ready`)

Workers build at smelt `fast`, so the batch is reviewed once, as a whole. When the highest speed among
the batch and its items is `fast`, or mode is `plain`, skip this. Otherwise, before the final message, run smelt's check phase at the
batch's speed (read the SKILL.md `smithy skills smelt` prints: its speed table and
smelt's `references/CHECK.md`) in one wave of parallel fresh-context agents on:
- the diff `git diff <base_sha>..origin/<base>` (`base_sha` from `batch.json`; when a foundation
  task such as a skeleton shipped before the batch, `base_sha` = the commit before it), plus each
  `ready` item's `gh pr diff`;
- the spec: the task dir's spec, else every item's intent.
Add the security agent when any item is risky (smelt's SKILL.md). The reviewer's prompt includes the full `<smithy>` path so it can run `<smithy> comments`. Test agents work in a fresh worktree of `origin/<base>`, never in a
worker's.
A whole batch is never a cheap review: route each agent with `smithy route --agent <agent> --text
"<batch goal>" --speed <batch speed> --batch` (never below `mid`); every prompt
names the repo's test command (from AGENTS.md / CI) so findings come with a failing run.
Findings follow smelt's `references/RECEIVING.md`. The blocking ones become one new `fix` item: one worker
(same brief, speed `fast`, intent = the findings verbatim, branch from `smithy names branch -
"<batch> review" fix`), one PR, merged like the others; then smelt's re-review (smelt's `references/RECEIVING.md`, step 5)
at the fix's commit. Before merging the fix PR, run any `zz_` holdout test in a fresh
worktree of `origin/<fix-branch>`. You write the fix PR's dispositions (every finding, including declined
ones and why) in its body. A high security finding also goes to the user (decide stage). Non-blocking ones
go under **Followups**.

## batch end (final message)

When every item is final, read `smithy decisions list` (this batch's `--ref <batch>/…` entries), `smithy decisions pending` and every card's `followup:` note (cards
set `followup:` in the note of a merged item), then end the final message with one section, one
line each, grouped as **Decisions** and **Followups**, each with the PR it came from
(`- <decision or followup> (PR <url>)`; no PR → the item id).
- A decision that reads like a reusable rule (it would apply to future tasks, not just this one)
  gets a trailing `candidate standard: <rule> | evidence: <the PR or finding that showed it>`.
  This is a proposal only: write nothing to STANDARDS.md or any repo doc without the user's yes;
  on yes, hand the accepted ones to smelt `retro` (or ledger).
- One group empty → leave it out. Both empty → the single line `no decisions or followups`.
- The user drops what they don't want; unpromoted entries stay in `smithy decisions`.

## reported · dropped

`reported`: read the report's first lines, **end** the worker, `smithy cleanup`. `dropped`:
`smithy cleanup`, **end** the worker.

## stuck (`blocked`, `failed`, or no card update for 45 min)

A session (background) worker whose closing line is `done: <id> waiting` is parked in `smithy wait-turn`: its stage is
`waiting`, not stuck; nothing to do until its turn comes.

Recovery ladder, one rung per wake:
1. Answer from the intent if the note asks something it settles.
2. One corrective line to the worker (**tell**).
3. Relaunch in the **same worktree**, only while the item's `launches` is below 3 (first launch
   included): bump `launches`, then `smithy brief <batch> <id>` again (it adds the relaunch line).
4. At the cap, set the card `state=blocked` and tell the user
   with the `note`; the item stays listed as blocked until they act.

Never a fresh worktree for the same task.

Takeover data: every stuck report, and the final message for any item not merged, gives per item
the worktree path, branch, session id or pid, and log path (`<batch-dir>/<id>.log`), so the user
can take over by hand.
