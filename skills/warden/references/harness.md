# Harness adapters

The only file in these skills that names vendor tools. The loop uses five operations;
`smithy harness` prints the adapter (`any`, `cli` or `desktop`). If you clearly have tools it
didn't detect, use the matching row. State lives in the batch dir and git, so any harness can
pick up a batch. Always render the brief (`smithy brief <batch> <id>`, writing `<batch-dir>/<id>.brief.md`)
before spawning: a respawn reuses it; a relaunch renders it again (`references/acting.md`, stuck).

## Default: any harness

| Operation | How |
|---|---|
| **spawn** worker | `smithy agent --bg [--model <m>] --cwd <worktree> --log <batch-dir>/<id>.log <batch-dir>/<id>.brief.md` |
| **tell** worker | append to `<batch-dir>/<id>.inbox.md`, then respawn it in the same worktree |
| **wake** warden | `smithy wait batch:<batch>` as a background job; if the harness can't wake on exit, in the foreground (returns within `--max` minutes) |
| **end** worker | `kill <pid>` if alive |
| auto-end on PR close | none: the final message says to run warden cleanup |
| worker closing line | the card's `note` |
| **channel line** (the brief's `<warden channel>`) | "Put it in the card's note." |
| **subagent** (short read-only job, or a fallback worker) | the harness's own subagent tool; none: `smithy agent --readonly [--model <m>] <prompt-file>` |

`smithy agent` picks the installed CLI (`AGENT_CLI`, or `AGENT_CMD` for any other command, with no
unattended flags added) and adds its unattended flags. Workers get `WARDEN_HARNESS` set.
Headless workers are one-shot and can't receive messages mid-run:
- A worker that needs a decision writes its card and exits. To answer, append to
  `<id>.inbox.md` and respawn; the brief's relaunch line ("Read <id>.inbox.md first") continues
  from what exists.
- Record `pid` and `log` in the item; `kill -0 <pid>` tells if it is alive. A dead pid with a
  non-final card is `stuck`.
- No PR auto-fix: on each wake `smithy verify` the `pr_open` items; respawn a red one with
  "fix CI on <pr>" in its inbox.

## Subagent workers (fallback)

When **spawn** is refused and no headless CLI is signed in (`smithy agent --bg` fails at start),
run each worker as a background subagent of this session with the prompt `Read
<batch-dir>/<id>.brief.md and follow it exactly.` Cards stay the channel; **tell** is a message to that subagent, **end** its stop.
Workers die with this session: say so in the plan line. Such a worker can't be woken from the
background, so render its brief with `smithy brief <batch> <id> --subagent` (wait-turn in the foreground).

## Per-harness details

| Harness | spawn · tell · end | wake | channel line | Notes |
|---|---|---|---|---|
| Claude Code CLI | `claude --bg -n "warden <batch> <id>" [--model <m>] "<brief>"` · `SendMessage` to its address · `claude stop <id>` + `claude rm <id>` | `smithy wait` with `run_in_background` | "Send it to the warden with SendMessage, to: <name>." | Your name: `ListAgents` prints "This session is <name>"; render briefs with `smithy brief <batch> <id> --to <name>`. Launch from the workspace root with this session's `--permission-mode`. `claude agents --json` lists workers. Subagent: Agent tool. No PR auto-fix (verify on each wake). Unattended flag: `claude -p --permission-mode auto` |
| Claude Code desktop (optional) | `start_session` · `SendMessage` to its `local_…` id · `archive_session` | `smithy wait` with `run_in_background` | "Put it in the card's note." | Below |
| Codex | `codex exec` (git dir writable, network on) · resume (`codex exec resume <id>`) untested | foreground | "Put it in the card's note." | subagent: `spawn_agent` |
| OpenCode | `opencode run --standalone --auto` | resumes on background exit | "Put it in the card's note." | has subagents |
| Gemini CLI | `gemini --approval-mode yolo` | only with `tools.shell.backgroundCompletionBehavior: "inject"` | "Put it in the card's note." | has subagents |
| Cursor | `agent -p --force` | foreground | "Put it in the card's note." | has subagents |
| DeepSeek Harness (dsh) | `dsh headless --approval-policy never` | resumes on background exit | "Put it in the card's note." | has subagents |
| Pi | `pi -p` | foreground | "Put it in the card's note." | no subagents: `smithy agent --readonly` |

### Claude Code desktop

| Rule | Detail |
|---|---|
| Launch only in the user's turn | The app refuses `start_session` in a turn an event started (watcher exit, worker message, CI); a `PermissionRequest` hook removes the approval click, not this rule. So every worker launches in the user's turn and dependents wait in `smithy wait-turn`. |
| Later work | A follow-up item or a third-rung relaunch from an event turn is proposed as a card (`initiation "own_initiative"`, same brief); prefer **tell**. When the user starts it, find its id with `list_sessions {"linked": true}` and record it in `batch.json`. |
| Loading `start_session` | It may be a deferred tool: load it with the harness's tool search first. Can't load it → the subagent fallback above. The plan line says which spawn path is used. |
| `start_session` fields | `initiation "user_asked"`, `context "fresh"`, `use_worktree false`, no `cwd`, `name` = id, `model` = routed model (omit when `-`), `wake_when_done true`, `prompt` = `Read <batch-dir>/<id>.brief.md and follow it exactly.` (full text if the harness can't read files), `background` = the batch goal; all launches in one message; record each session id. |
| Approval hook | A `PermissionRequest` hook (matcher `mcp__ccd_session__start_session`, allow) removes the approval card; doctor checks it. |
| Auto-end | The worker turns on the app's auto-archive for its PR; workers still `ready` at finish end when the PR closes. |
| Closing line | the card's note |
| Effort | app sessions take no effort flag: they run at the harness default; say so once. |
