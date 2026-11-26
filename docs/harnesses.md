# Harnesses

Nothing depends on one vendor. Per harness, three things change in daily use:

- how you start a skill;
- how warden starts workers;
- whether a session waiting on a background command resumes on its own. If it doesn't, it waits
  in the foreground.

Unattended runs also differ in their guard, sandbox support and limit detection; see
[autonomous mode](autonomous.md).

| Harness | Skills folder (global) | Start a skill | Warden workers | Resumes after background wait |
|---|---|---|---|---|
| Claude Code desktop | `~/.claude/skills` | `/smelt` | app sessions (`start_session`) | yes |
| Claude Code CLI | `~/.claude/skills` | `/smelt` | `claude --bg` | yes |
| Codex CLI | `~/.agents/skills`, `~/.codex/skills` | `$smelt` | `smithy agent` → `codex exec` | no: foreground watcher |
| OpenCode | `~/.config/opencode/skills`, `~/.agents/skills` | `@smelt` | `smithy agent` → `opencode run --standalone --auto` | yes |
| Pi | `~/.pi/agent/skills`, `~/.agents/skills` | `/skill:smelt` | `smithy agent` → `pi -p` | no: foreground watcher |
| DeepSeek Harness | `~/.dsh/skills`, `~/.agents/skills` | `/smelt` | `smithy agent` → `dsh headless` | yes |
| Antigravity CLI (`agy`) | `~/.gemini/skills`, `~/.agents/skills` | ask: "use smelt" | `smithy agent` → `agy -p` | no: foreground watcher |
| GitHub Copilot CLI | `~/.copilot/skills`, `~/.agents/skills` | ask: "use smelt" | `smithy agent` → `copilot -p` | no: foreground watcher |
| Grok Build | `~/.grok/skills`, `~/.agents/skills` | ask: "use smelt" | `smithy agent` → `grok -p` | no: foreground watcher |
| Cursor CLI | `~/.cursor/skills`, `~/.agents/skills` | `/smelt` | `smithy agent` → `agent -p` | no: foreground watcher |

## Headless workers

`smithy agent` runs workers headless, with each CLI's flags for writing files, running commands
and opening PRs without prompts.

| Variable | Does |
|---|---|
| `AGENT_CLI` | picks the CLI |
| `AGENT_ARGS` | adds flags |
| `AGENT_CMD` | plugs in any other command |
| `AGENT_PROTOCOL=acp` | drives Antigravity (`agy acp`), Copilot (`copilot --acp`), Grok (`grok agent stdio`), OpenCode or an ACP `AGENT_CMD` over the Agent Client Protocol: tool calls, permission answers and usage land in smithy's events; under `--profile auto` each permission request is checked against the auto allow-list and anything unclassified is rejected |

Every skill can be run by an agent. Only warden is kept out of implicit invocation: Codex reads
`allow_implicit_invocation: false` from `skills/warden/agents/openai.yaml`. Warden still asks for
your yes before it starts sessions.

## Daemon

The release binary of smithy (`smithy-<os>-<arch>`, the harness build) adds `smithy daemon`: the
autonomous loop plus a local API any client can follow (SSE events, first-answer-wins
decisions). It is the same for every agent CLI; see [autonomous.md](autonomous.md#daemon-harness-build).

## How the skills behave

- **One task list.** smelt and warden share it: `NN-<slug>.md` files with status, blockers and PR,
  kept in the repo or in `~/.smithy/plans/`.
- **Lessons.** After a PR, smelt proposes up to 3 lessons, each citing evidence. What you accept
  lands in `docs/agents/STANDARDS.md` or `scripts/SCRIPTS.md`, which every later session reads
  first. ledger keeps both small, and `smithy drift` flags stale lines.
- **Quiet workers.** Agents work silently and send their questions to the session you talk to.
- **Your repo's conventions.** Commits follow your repo's style, PRs follow your template, and
  branches come from `smithy names branch`. Finished worktrees are always removed.
