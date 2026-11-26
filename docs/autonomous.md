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
smithy config set auto.max_cost_per_day 15             # optional USD cap on reported spend per day
smithy auto serve                              # Esc/q stops the loop; running agents keep going
```

Issues opt in with the `auto` label. `model:` and `speed:` labels pick the tier and smelt speed.
A daily issue cap (`auto.max_per_day`), an optional daily cost cap (`auto.max_cost_per_day`, USD
of reported cost; running agents finish) and an optional hours window bound the spend; reaching
either cap records a `cap_reached` event (and notification) once that day. `smithy tui` shows the queue, running
agents and the cost of each issue, a History tab of past days' work with PR, cost and outcome, and
edits settings in place. Every setting is listed in
[settings.md](settings.md).

## Daemon (harness build)

In the harness build, the serve loop runs inside `smithy daemon`, which also serves a local API
and writes every event to `~/.smithy/smithy.db`. `smithy auto serve` starts it in the foreground
(same flags, same view) or, when one already runs, attaches and prints its events.

Bare `smithy` is the one command: it attaches to the local daemon, or starts it in the
background first. The daemon outlives the chat so work and the web keep going, and exits on its
own after `daemon.idle_exit` (default `30m`, `0` never) with no clients and no running work;
`smithy stop` stops it. `smithy --standalone` runs a private daemon inside the chat process that
stops with it (tests, CI, one-off). `smithy daemon` stays the foreground server for hosts
(systemd) and never idle-exits unless given `--idle-exit`. `GET /v1/state` reports the daemon's
`version`; the chat warns when it differs from its own.

```
smithy                                         # attach, or start the daemon in the background
smithy stop                                    # stop the background daemon
smithy daemon --bg                             # prints "<pid> <url>"; log ~/.smithy/daemon.log
curl -H "Authorization: Bearer $(cat ~/.smithy/daemon.token)" localhost:7477/v1/state
```

- `GET /v1/state`, `GET /v1/events?since=<seq>` (SSE, resumes with `Last-Event-ID`),
  `POST /v1/decisions` (`{"id","answer"}`: the first answer wins; later ones get the outcome, 409).
- Sessions: `POST /v1/sessions` (`{"agent","model","dir","message"}`; agent is a CLI name, `acp:<cli>`
  or `api:<provider>/<model>`, `""` the last used, else `session.agent`; `model` goes to a CLI's
  `--model`; `dir` an absolute existing folder),
  `POST /v1/sessions/{id}/messages` (`{"text"}`, queued behind a running turn),
  `POST /v1/sessions/{id}/stop` and `GET /v1/sessions` (status `working`, `needs_you`, `done`,
  `failed`). Their events (`session_message`, `session_text`, `session_tool` with args and output,
  `session_diff`, `session_usage`, `session_status`) stream on `/v1/events` with the session id.
  An `api:` session runs smithy's loop inside the daemon: a tool call the auto rules refuse becomes
  the decision `perm:<id>/<n>` (allow/deny) and the session shows `needs_you` until someone
  answers; the secret rules are never asked. Other agents run `smithy agent` once per turn with
  the conversation so far in the prompt. A reply in a session's chat thread is its next message.
  At most 16 sessions working at once and 8 queued messages each (429 past that); a permission question shows the whole call, and tool output and text are redacted before they are stored.
- Setup: `GET /v1/setup` lists every agent CLI and API key as ready or missing with its fix, the
  default agent and model (`chosen` is false until one is picked or used: first run), the kit
  (`kit.mcp`, `kit.skills`) and the skills and MCP servers each harness has installed;
  `PUT /v1/setup/default` (`{"agent","model"}`), `PUT /v1/setup/kit` (`{"mcp","skills"}`, skills
  must be installed smithy skills) and `POST /v1/setup/skills/install` (`{"harness"}`, empty for
  every detected one) change them. `GET /v1/agents/{id}/models[?refresh=1]` lists the models
  agent `id` (a CLI or `api:<provider>`) reports, default first, cached for a day. The TUI's first run, `ctrl+l`, `/agent`, `/model`, `/skills`
  and the web's Settings screen use these.
- It tails `~/.smithy/events.jsonl` and the proxy's log into its store (once per line, across
  rotation and restarts) and turns them into decisions: a proxy `secret_ask` is `secret:<id>`
  (approve/deny, handed back as `smithy proxy approve|deny` would), a ready item is
  `merge:<batch>/<item>` (merge/hold) and a `needs_decision` item `decide:<batch>/<item>/<key>`;
  warden answers land on the card as `answer`, `answer_id` and `answered_by`. Telegram alerts
  show the same id.
- It binds `127.0.0.1` (plus `daemon.listen` when set, below). Every request needs the token (`~/.smithy/daemon.token`, 0600) or a
  `Tailscale-User-Login` listed in `daemon.allow`; anything else gets 401. The token works only
  with a loopback `Host`, the login only with a `*.ts.net` one, so a DNS-rebound web page can't
  use the API.
- Remote access: `tailscale serve --bg 7477` puts it on your tailnet over HTTPS with the
  identity header; smithy itself only reads `tailscale status --json` (for `smithy servers discover`). Any local process can send that header, so set
  `daemon.allow` only on a machine where every local user is trusted (the dedicated OS user above).
- Another machine without tailscale serve: set `daemon.listen` to this machine's Tailscale
  `ip:port` (100.64.0.0/10 or fd7a:115c:a1e0::/48; the listener is plain HTTP, so any other address
  is refused, and off a tailnet you put a TLS proxy such as `tailscale serve` in front of the
  loopback port instead), run `smithy pair` here and `smithy servers add <name> http://<ip>:<port>
  --code <code>` there. That listener takes only paired devices' bearer tokens (revoked with `smithy
  pair revoke`), never the daemon token or the login header. Settings routes (`/v1/guard/suggested`,
  `/v1/setup/kit`, `/v1/setup/skills/install`) and `/v1/servers` need the daemon token, so a device
  cannot change them. `smithy servers` lists saved daemons with their status,
  `smithy servers discover` finds Tailscale peers running smithy, and `/connect` in the chat or
  the web sidebar switches between them; `--server` / `SMITHY_SERVER` pick one for a run.
- `smithy attach [url]` follows it in a terminal UI: live events, the decisions waiting on you
  (keys 1-9 answer; the first answer wins, a later one shows the recorded outcome), and on a lost
  connection it retries and resumes after the last event seen, so a daemon restart neither
  repeats nor drops events. Without a url it uses the local daemon and its token; a non-loopback
  url sends no token (`daemon.allow` decides).
- `smithy doctor` reports whether it runs, a stale `daemon.json`, and a token readable by others.

### Chat, intake, report and web UI

- Connectors: with Telegram (`notify.telegram.chat`), Slack (`connect.slack.channel`) or Discord
  (`connect.discord.channel`) set up, the daemon gives each session its own topic or thread,
  posts decisions with one button per option, and records a reply in that thread as the
  session's input. It only listens to the users in `connect.<name>.users`; unset, it only sends.
  Setup per chat app: [settings](settings.md).
- `idea:` intake: write `idea: <what you want>` in the main Telegram chat and smithy plans it and
  posts the plan back. Nothing runs without your yes; on yes the tasks land on the board with the
  `intake` label and wait there for your review.
- Weekly report: `smithy report week` prints what shipped, what failed, time and cost per harness
  and the decisions still waiting. The daemon posts it on Mondays only when you add the `report`
  kind to a connector's kinds.
- `smithy web [open]` prints (or opens) the link to the web UI the daemon serves: sessions,
  decisions, agents, the board and the Bench screen (`smithy bench` runs and comparisons) in the
  browser.

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

Every flag and subcommand: `smithy help board` (or `smithy help board <sub>`).

With `board.linear_sync true`, `smithy board sync` (and every board poll) syncs with Linear by
field ownership: Linear owns title, body, priority and labels; the board owns state, claims, PRs
and branch. Issues in `linear.states` under the `linear.*` filters are imported as `todo`; later
edits are read with an `updatedAt` cursor (no webhooks), and state changes and new PR links are
pushed back. A board task linked to Linear refuses local edits to the fields Linear owns.

### Tools for the agents (smithy mcp)

In the harness build, `smithy agent` gives every driven CLI a `smithy` MCP server through its
own per-run config (never your global one): `search` over the working folder, `board_list`,
`board_show`, `board_claim` and `board_set`, `secret_call` to call an API with a key you bound
with `smithy auth bind` (the agent never sees the key; writes follow the bind's methods and asks),
and `ask`, which puts a decision on the run's warden card and waits for your answer from the TUI,
web or chat. `--readonly` runs get search and board reads only.

## Notifications

```
smithy auth set telegram                       # bot token
smithy config set notify.telegram.chat <id>
smithy notify test
```

For the harness build's Telegram connector the chat must be a supergroup with topics on and the
bot an admin with the Manage Topics right; a bot that is not an admin needs privacy mode off
(BotFather `/setprivacy`) to hear topic replies and `idea:` messages.

## Security

Issue text can carry injected instructions. Run unattended mode under a dedicated OS user or
machine, with a fine-grained, repo-scoped GitHub token and a test or least-privilege Linear key.

- **What each harness gets under `--profile auto`:**

  | Harness | Allow-list |
  |---|---|
  | Claude Code | smithy's allow-list: explicit git/gh subcommands; config edits, credential and network commands, and secret files denied |
  | Codex | its workspace-write sandbox, network on (push needs it), approvals off; `auto.sandbox native` turns its network off |
  | OpenCode | smithy's allow-list as `OPENCODE_PERMISSION` rules: bash allow-list, web fetch and outside directories denied, secret files denied for read and edit (also for `--readonly` runs such as the intake planner) |
  | Any CLI over ACP (`AGENT_PROTOCOL=acp`) | each permission request goes through the same allow-list: a command must match an allowed rule and no denied one (no `;`, `&&`, pipes, redirects or `$`), edits stay inside the worktree and off denied paths, reads off secret paths; fetch, delete, move and anything smithy can't classify are rejected |
  | Others (gemini, pi, cursor, dsh) | their own headless mode, no smithy allow-list |

  Only Claude Code and OpenCode can deny reads of `~/.smithy/auth.json` and the other secret
  files. Codex, gemini, pi, cursor and dsh have no per-path read deny, so they can read
  them; `smithy doctor` says so for the configured CLI. Even for Claude Code and
  OpenCode the shell deny rules match command text (globs such as `~/.s*`, `$HOME` and key file
  names included), so a command that builds the path another way can slip past: only the `native`
  sandbox (Claude Code, the API backend's shell) fully keeps secrets out of reach.

  None of these removes the risk. Nothing stops Codex or the others sending data out, so
  least-privilege tokens matter most there.
- **Guards.** Workers run without permission prompts by default; the guard is the only thing
  that asks. The dangerous-command guard is on by default (`guard.enabled`): a shell command
  matching its built-in list or `guard.block` (`rm -rf /`, a force push to main,
  `git reset --hard`, a disk format, `curl | sh`, `chmod -R 777`, deleting `~/.smithy`) asks you
  in the TUI, web or chat to run it once, always allow the pattern (added to `guard.allow`) or
  deny. With no daemon, or no answer within `ask.timeout`, the command is denied. The fetch
  guard leaves fetch open (HTTPS only) except for domains in `guard.fetch_block`, which ask the
  same way; `guard.fetch_allow` skips the question. `smithy guard check` tests a command,
  `smithy guard fetch-check <host>` prints the `guard.fetch_block` glob a host matches (exit 1) or nothing, `smithy guard list`
  shows the rules and `smithy guard suggest --add` allows common hosts. `guard.enabled`,
  `guard.allow` and `guard.fetch_allow` are read from your user config only.
- **Not sandboxed.** The orchestrator itself is not sandboxed and may run repo tests.
- **Credentials workers keep.** Workers sign commits through your ssh-agent, so use a dedicated
  signing key. They still get your `GH_TOKEN`.

### Sandbox

`auto.sandbox` is read from your user config only. It fails closed, and `smithy doctor` checks
it.

| Mode | Isolation | Works with |
|---|---|---|
| `off` (default) | the allow-list above and the dangerous-command guard only, no OS sandbox | every harness |
| `native` | the harness's own OS sandbox: writes only to the worktree and smithy's plans | Claude Code; Codex runs with network off here (on under `off`), so push and `gh` fail; Gemini CLI is refused (its sandbox can't limit hosts) |

The API backend (`auto.agent api:<provider>/<model>`) runs its tools on the `--profile auto`
rules only while `auto.sandbox` is `off` (the default); with `auto.sandbox native` its shell tool
runs inside the OS sandbox.

The container mode (`auto.sandbox container`, docker or podman) was removed in v0.23; a setting
left on it stops workers until you set `native` or `off`. The egress proxy that bound secrets to
hosts runs natively: `smithy proxy serve` listens on `127.0.0.1:8877`, and a worker with the
smithy MCP kit uses its secret tool:

```sh
smithy auth set linear < key.txt
smithy auth bind linear api.linear.app GET,POST
# a worker: curl http://127.0.0.1:8877/s/linear/graphql, or the secret tool
```

Writes are refused unless bound; `smithy auth ask <name> on` holds them until
`smithy proxy approve <id>`.

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
smithy config set pool.order claude,codex  # optional: harness order for a pool run
```

With `pool.order` set, a pool run stays on the first listed harness that has a free account. When
every account of that harness is limited, it falls back to the next one (logged as a
`pool_fallback` event) and goes back once the limit resets.

When every account is limited, `smithy agent` exits 75 and serve waits for the earliest reset,
then notifies you on Telegram if it is set up.

Use only accounts you own, through the official CLIs, with no proxy or relay in between: each
provider's terms apply to each account.

- **Hiding the account dirs:** `native` hides them from workers. With the sandbox `off`, only
  Claude Code workers get a deny rule for them.
