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
agents and the cost of each issue, a History tab of past days' work with PR, cost and outcome, and
edits settings in place. Every setting is listed in
[settings.md](settings.md).

## Daemon (harness build)

In the harness build, the serve loop runs inside `smithy daemon`, which also serves a local API
and writes every event to `~/.smithy/smithy.db`. `smithy auto serve` starts it in the foreground
(same flags, same view) or, when one already runs, attaches and prints its events.

```
smithy daemon --bg                             # prints "<pid> <url>"; log ~/.smithy/daemon.log
curl -H "Authorization: Bearer $(cat ~/.smithy/daemon.token)" localhost:7477/v1/state
```

- `GET /v1/state`, `GET /v1/events?since=<seq>` (SSE, resumes with `Last-Event-ID`),
  `POST /v1/decisions` (`{"id","answer"}`: the first answer wins; later ones get the outcome, 409).
- It binds `127.0.0.1` only. Every request needs the token (`~/.smithy/daemon.token`, 0600) or a
  `Tailscale-User-Login` listed in `daemon.allow`; anything else gets 401. The token works only
  with a loopback `Host`, the login only with a `*.ts.net` one, so a DNS-rebound web page can't
  use the API.
- Remote access: `tailscale serve --bg 7477` puts it on your tailnet over HTTPS with the
  identity header; smithy has no Tailscale code. Any local process can send that header, so set
  `daemon.allow` only on a machine where every local user is trusted (the dedicated OS user above).
- `smithy attach [url]` follows it in a terminal UI: live events, the decisions waiting on you
  (keys 1-9 answer; the first answer wins, a later one shows the recorded outcome), and on a lost
  connection it retries and resumes after the last event seen, so a daemon restart neither
  repeats nor drops events. Without a url it uses the local daemon and its token; a `*.ts.net`
  url sends no token (`daemon.allow` decides).
- `smithy doctor` reports whether it runs, a stale `daemon.json`, and a token readable by others.

## Built-in tracker (harness build)

`smithy board` keeps tasks in `~/.smithy/smithy.db`; `auto.sources board` makes the watcher
read it like any source (the `auto` label and filters apply). A task is new work while it is
`todo`, unclaimed and every task in `blocked_by` is done or canceled.

```
smithy board add "Fix login" --labels auto --priority 2
smithy board list --state todo
smithy board claim B-1 --by me --ttl 30m
smithy board set B-1 --state review --pr https://github.com/o/r/pull/7
```

With `board.linear_sync true`, `smithy board sync` (and every board poll) syncs with Linear by
field ownership: Linear owns title, body, priority and labels; the board owns state, claims, PRs
and branch. Issues in `linear.states` under the `linear.*` filters are imported as `todo`; later
edits are read with an `updatedAt` cursor (no webhooks), and state changes and new PR links are
pushed back. A board task linked to Linear refuses local edits to the fields Linear owns.

## Notifications

```
smithy auth set telegram                       # bot token
smithy config set notify.telegram.chat <id>
smithy notify test
```

## Security

Issue text can carry injected instructions. Run unattended mode under a dedicated OS user or
machine, with a fine-grained, repo-scoped GitHub token and a test or least-privilege Linear key.

- **What each harness gets under `--profile auto`:**

  | Harness | Guard |
  |---|---|
  | Claude Code | smithy's allow-list: explicit git/gh subcommands; config edits, credential and network commands, and secret files denied |
  | Codex | its workspace-write sandbox, network on (push needs it), approvals off |
  | Others | their own headless mode, no smithy allow-list: use `container` with `proxy` (needs your own `auto.sandbox_image`) |

  None of these removes the risk. Nothing stops Codex or the others sending data out, so
  least-privilege tokens matter most there.
- **Not sandboxed.** The orchestrator itself is not sandboxed and may run repo tests.
- **Credentials workers keep.** Workers sign commits through your ssh-agent, so use a dedicated
  signing key. They still get your `GH_TOKEN`.

### Sandbox

`auto.sandbox` is read from your user config only. It fails closed, and `smithy doctor` checks
it.

| Mode | Isolation | Works with |
|---|---|---|
| `off` (default) | the guard above only | every harness |
| `native` | the harness's own OS sandbox: writes only to the worktree and smithy's plans | Claude Code; Codex runs with network off, so push and `gh` fail; Gemini CLI is refused (its sandbox can't limit hosts) |
| `container` | docker or podman, read-only root, dropped capabilities; with `auto.sandbox_network proxy`, egress allow-list and secrets | Claude Code and OpenCode in the default image; others with your own `auto.sandbox_image` |

Container mode notes:

- **Network:** it needs `auto.sandbox_network`. `proxy` puts workers on an internal network whose
  only exit is the `smithy-proxy` sidecar: GitHub, the model APIs, package registries and
  `auto.sandbox_domains`, nothing else. `bridge` is open egress with no domain allow-list.
- **Secrets (proxy only):** the worker never gets the key. Bind it to its host, then the worker
  calls the proxy, which adds the key and scrubs it from the response:

  ```sh
  smithy auth set linear < key.txt
  smithy auth bind linear api.linear.app GET,POST
  # inside the worker: curl http://smithy-proxy:8877/s/linear/graphql
  ```

  Writes are refused unless bound; `smithy auth ask <name> on` holds them until
  `smithy proxy approve <id>` instead. The sidecar needs a linux smithy binary
  (`auto.sandbox_proxy_binary`, see [settings](settings.md)).
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

| Harness | Config dir per account | Limit detected |
|---|---|---|
| Claude Code | `CLAUDE_CONFIG_DIR` | yes |
| Codex | `CODEX_HOME` | yes |
| OpenCode | `XDG_DATA_HOME` | yes |
| Gemini CLI | `GEMINI_CLI_HOME` | yes |
| Cursor CLI | `CURSOR_CONFIG_DIR` | no |
| Pi | `PI_CODING_AGENT_DIR` | yes |
| DeepSeek Harness | `DSH_HOME` | no |

Every harness rotates across its accounts. Where the limit is not detected yet, a limited run fails
like any other error, and the next run picks the next account.

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
then notifies you on Telegram if it is set up.

Use only accounts you own, through the official CLIs, with no proxy or relay in between: each
provider's terms apply to each account.

- **Hiding the account dirs:** `native` hides them from workers. With the sandbox `off`, only
  Claude Code workers get a deny rule for them.
- **Container:** a pooled container worker mounts only its own account's dir and gets its
  config-dir variable (`CODEX_HOME`, `XDG_DATA_HOME`, ...). A claude account's Keychain login never
  reaches a container, so each needs its own token: run `claude setup-token` signed in as that
  account, then `smithy auth set claude:<account>`. `smithy doctor` fails until every claude account
  in the pool has one.
