# Autonomous mode

tend `auto` watches labeled GitHub and Linear issues and builds each one unattended through
warden, reporting progress back on the issue.

![How autonomous mode works](auto.svg)

## Setup

Run it under a dedicated OS user or machine (see [Security](#security)).

```
smithy config set auto.sources github,linear
smithy config set linear.team ENG              # required for Linear
smithy auth set linear < keyfile               # or LINEAR_API_KEY
smithy config set auto.max_per_day 20
smithy auto serve                              # Esc/q stops the loop; running agents keep going
```

Issues opt in with the `auto` label. `model:` and `speed:` labels pick the tier and smelt speed.
A daily cap and an optional hours window bound the spend. `smithy tui` shows the queue, running
agents and the cost of each issue, and edits settings in place. Every setting is listed in
[settings.md](settings.md).

## Notifications

```
smithy auth set telegram                       # bot token
smithy config set notify.telegram.chat <id>
smithy notify test
```

## Security

Issue text can carry injected instructions. Run unattended mode under a dedicated OS user or
machine, with a fine-grained, repo-scoped GitHub token and a test or least-privilege Linear key.

- **Allow-list.** Workers run with `smithy agent --profile auto`, which allows only explicit
  git/gh subcommands and denies config edits, credential and network commands, and secret files.
  This reduces the risk but cannot remove it.
- **Codex.** Codex runs in its workspace-write sandbox with network on (push needs it) and
  approvals off. Nothing stops it sending data out, so least-privilege tokens matter most there.
- **Not sandboxed.** The orchestrator itself is not sandboxed and may run repo tests.
- **Credentials workers keep.** Workers sign commits through your ssh-agent, so use a dedicated
  signing key. They still get your `GH_TOKEN`.

### Sandbox

`auto.sandbox` is read from your user config only. It fails closed, and `smithy doctor` checks
it.

| Mode | Isolation | Works with |
|---|---|---|
| `off` (default) | allow-list only | every harness |
| `native` | the harness's own OS sandbox: writes only to the worktree and smithy's plans | Claude Code, Codex (macOS, Linux) |
| `container` | docker or podman, read-only root, dropped capabilities | Claude Code, Codex, OpenCode |

Container mode notes:

- **Network:** it needs `auto.sandbox_network`. `bridge` is open egress with no domain allow-list;
  an egress proxy is planned for v0.17.
- **Credentials:** each worker gets only its own harness's credential from `smithy auth`:

  | Harness | Credential |
  |---|---|
  | Claude Code | `claude` (from `claude setup-token`) or `anthropic` |
  | Codex | `openai` |
  | OpenCode | none for free models; `anthropic` or `openai` for those providers' models |

  Credentials are visible to `docker inspect` and to anyone with the engine socket.
- **User:** on macOS the container runs as root inside the engine VM; on Linux it runs as your uid.

## Account pool

The pool is off until you add an account. Each account is your own, on its harness's official CLI,
signed in once in its own config dir.

| Harness | Config dir per account |
|---|---|
| Claude Code | `CLAUDE_CONFIG_DIR` |
| Codex | `CODEX_HOME` |

Other harnesses run with their normal login. They join the pool once smithy knows how to give
each account its own dir.

`smithy agent` picks the least recently used account that is not rate limited. When a run hits a
usage limit, that account is marked until its reset:

- **Clean run:** a read-only or untouched run reruns once on the next account.
- **Partial work:** the run stops (exit 75) and warden resumes it in the same worktree.

```
smithy pool add main <harness>       # ~/.smithy/accounts/main
smithy pool login main               # sign in once, then exit
smithy pool list                     # status, reset time, runs and cost today
smithy config set auto.agent pool    # optional: the orchestrator may use any pooled harness
```

When every account is limited, `smithy agent` exits 75 and serve waits for the earliest reset,
then notifies you on Telegram.

Use only accounts you own, through the official CLIs, with no proxy or relay in between: each
provider's terms apply to each account. Account dirs are hidden from workers in `native` and
`container` sandboxes; with the sandbox `off`, only Claude Code workers get a deny rule for them.
