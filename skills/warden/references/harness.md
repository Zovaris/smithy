# Harness adapters

The loop in SKILL.md uses five operations. `smithy harness` prints which adapter applies
(`desktop`, `cli` or `any`); if you clearly have tools it didn't detect, use the matching column.
State is always the batch dir and git, so the same batch can be run from any harness.

| Operation | Claude Code desktop | Claude Code CLI | Any harness |
|---|---|---|---|
| **spawn** worker | `start_session` (below) | `claude --bg -n "warden <batch> <id>" [--model <m>] "<brief>"` | `smithy agent --bg [--model <m>] --cwd <worktree> --log <batch-dir>/<id>.log <batch-dir>/<id>.brief.md` |
| **tell** worker | `SendMessage` to its `local_…` id | `SendMessage` to its address | append to `<batch-dir>/<id>.inbox.md`, then **respawn** it (keeps no context; a CLI's own resume, e.g. `codex exec resume <id>`, is untested here) |
| **wake** warden | `smithy wait batch:<batch>` with `run_in_background` | same | `smithy wait batch:<batch>` as a background job; if the harness can't wake on exit, run it in the foreground (it returns within `--max` minutes) |
| **end** worker | `archive_session` | `claude stop <id>` + `claude rm <id>` | `kill <pid>` if alive |
| auto-end on PR close | the worker turns on the app's auto-archive for its PR (if your harness supports it) | none: run warden cleanup | none: run warden cleanup |
| worker closing line goes to | the card's note | `SendMessage` to the warden's name | the card's note |
| **subagent** (short read-only job, or a worker in the fallback below) | Agent tool | Agent tool | the harness's own (Codex `spawn_agent`, OpenCode, Gemini, Cursor and DeepSeek subagents); Pi has none: `smithy agent --readonly [--model <m>] <prompt-file>` |

Always write the brief to `<batch-dir>/<id>.brief.md` before spawning, whatever the adapter: a
respawn reuses it.

## Claude Code desktop

The app refuses `start_session` in a turn that an event started (the watcher exiting, a worker's
message, CI): it only lets you propose a card there. A `PermissionRequest` hook removes the
approval click but not this rule. That is why the loop launches every worker in the user's turn
and lets dependents wait in their own session (`smithy wait-turn`). Work that appears later (a
follow-up item) is proposed as a card, or listed for the user. When the user starts a proposed
card, its session id doesn't come back to you: find it with `list_sessions {"linked": true}` and
record it in `batch.json`. A relaunch from an event turn (the third rung of the stuck ladder) is
proposed the same way; prefer the second rung (**tell**) there.

`start_session` with `initiation "user_asked"`, `context "fresh"`, `use_worktree false`, no
`cwd`, `name` = id, `model` = the item's routed model (omit when `-`), `wake_when_done true`, `prompt` = the brief's full text
(never just its path: a brief the worker has to go read loses to the app's reminders), `background` =
the batch goal; all launches in one message. Record each session id.
- A `PermissionRequest` hook (matcher `mcp__ccd_session__start_session`, allow) removes the
  approval card per session; doctor checks it.
- The app refuses `start_session` in a turn started by a worker message or an event: an item that
  unblocks there is proposed instead (`initiation "own_initiative"`, same brief).

## Claude Code CLI

Your name: `ListAgents` prints "This session is <name>"; put "Message the warden with
SendMessage, to: \"<name>\"" in the brief. Launch from the workspace root (a trusted folder),
`--permission-mode` = this session's mode. `claude agents --json` lists workers. No PR auto-fix:
`smithy verify` the `pr_open` items on each wake and tell a red one to fix CI.

## Subagent workers (fallback)

When **spawn** is refused (a desktop session another session started, or an event turn) and no
headless CLI is signed in (`smithy agent --bg` fails at start), run each worker as a background
**subagent** of this session with the brief's full text as its prompt. The cards stay the
channel; **tell** is the harness's message to that subagent, **end** its stop. Workers die with
this session, so say so in the plan line.

## Any harness (smithy agent)

`smithy agent` picks the installed CLI (or `AGENT_CLI`) and passes the flags it needs to work
unattended: `codex exec` with the git dir writable and network on, `opencode run --standalone
--auto`, `gemini --approval-mode yolo`, `agent -p --force` (Cursor), `pi -p`, `dsh headless
--approval-policy never`, `claude -p --permission-mode auto`. Workers get `WARDEN_HARNESS` set
to their CLI. **wake**: OpenCode and DeepSeek Harness resume the session when a background
command ends; Gemini only with `tools.shell.backgroundCompletionBehavior: "inject"`; Codex, Pi and
Cursor don't, so run `smithy wait batch:<batch>` in the foreground there (it returns within `--max` minutes).

Workers are one-shot headless runs of whatever agent CLI is installed (`AGENT_CLI` picks claude,
codex, opencode, gemini, cursor, pi or dsh; `AGENT_CMD` runs any other command, with no
unattended flags added). They can't receive messages mid-run, so:
- A worker that needs a decision writes its card and exits.
- To answer, append the answer to `<id>.inbox.md` and respawn it in the same worktree. The brief's
  relaunch line plus "Read <id>.inbox.md first" lets it continue from what exists.
- Record `pid` and `log` from `smithy agent --bg` in the item; `kill -0 <pid>` tells you if it is
  alive. A dead pid with a non-final card is `stuck`.
- The worker's closing line goes to its card's `note`; there is no message channel.
- No PR auto-fix: on each wake `smithy verify` the `pr_open` items; respawn a red one with
  "fix CI on <pr>" in its inbox.
