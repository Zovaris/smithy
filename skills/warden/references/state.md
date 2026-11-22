# State

Batch dir `~/.smithy/warden/<batch>/` (`$WARDEN_HOME`; `<batch>` = `YYYYMMDD-HHMM` or the
feature slug). No secrets or client data in it. A dir that already exists (empty or not) is
never reused: add `-2`, `-3` to the name. `smithy batch init <tasks_dir> --repo <dir> [--base b] [--merge auto|manual] [--max n]`
writes it from a task dir as `<repo>-<task dir name>` (merge stays empty without `--merge`: set the user's plan-line answer); `smithy batch add <batch> --id --title --intent [--base] [--speed]` appends an item.

## batch.json (yours)

`{"batch","repo","max","stack","production","mode","base_sha","merge","cleanup","tasks_dir","items":[{"id","title","kind",
"intent","source","speed","model","base","prereqs","merge","branch","worktree","session","hold",
"launches","route","risky"}]}`

- `intent`: the user's own words for the task, verbatim, never rewritten or enriched (for a task
  file or board item: its path or key, which the worker reads). Later asks are appended verbatim.
- `base_sha`: `{"<base>": "<sha>"}`, each base's `origin/<base>` commit at launch; the batch
  check reviews everything after it.
- `speed`: the item's smelt speed as asked (`(full)`); workers still build at `fast`, and the
  batch check runs at the highest speed among the items.
- `stack`: boolean, default false (wait for the prereq to merge); see SKILL.md Defaults.
- `production`: the production branch name, from the user's answer to the plan-line question or
  an obvious repo convention; `""` when no base in the batch is production.
- `route`: `debug` for a bug report (`references/intake.md`), else absent; `risky`: true when the
  item is risky (smelt's SKILL.md), so its brief requires the security agent.
- `launches`: starts so far, first included; at most 3 (see `acting.md`).
- `hold`: set when the user says wait/stop for an item, cleared only by the user. A held item is
  never launched, relaunched or nudged.

## Cards (`<id>.json`, the worker's, through `smithy cards set`)

Fields: `state`, `phase`, `progress` (`3/7`, the worker's own task list), `branch`, `pr`, `ci`,
`decision_key`, `question`, `options`, `recommendation`, `impact` (`impact_by=decide` when the
decision model raised it), `tests_run`, `risks`,
`review_rounds`, `security` (risky items: `ran` or `not-run:<reason>`), `note`.

| State | Meaning |
|---|---|
| `waiting` | Launched, waiting in `smithy wait-turn` for prereqs or a running slot |
| `working` | Building |
| `needs_decision` | Asked a question (`decision_key`, `question`, `options`, `impact`); stays open until you close it |
| `blocked` / `failed` | Stopped; `note` says what it needs |
| `pr_open` | PR opened |
| `ready` | Green: the warden merges it (merge auto) or the user does (manual) |
| `merged` · `reported` · `dropped` | Final: PR merged, scout report written, nothing to do |

`smithy cards board` adds a **stage** per item (what you do with it): `waiting`, `working`, `decide`
(needs_decision), `verify` (pr_open, ready), `stuck` (blocked, failed, or working with no update
for 45 min), `merged`, `reported`, `dropped`, and `held` / `queued` / `starting` for items without
a running worker.

## Other files

- `decisions.md` (yours): one line per decision settled without the user. Read at batch end and listed in the final message
  (`references/acting.md`, batch end), together with the cards' `followup:` notes.
- `<id>.brief.md` (rendered by `smithy brief`), `<id>.inbox.md`, `<id>.log`: the worker's brief,
  messages for respawns, its log. `smithy wait batch:<batch>` watches the cards and their `pr`s;
  `.wait-seen` (its own, one key per line) keeps the PR events it reported, so a relaunched wait skips them.
- `<id>/report.md`: scout reports. `followups/`: task files workers write after a merge when there
  is no task dir.
