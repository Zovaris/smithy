# Changelog

One version for the whole repo, tagged `vX.Y.Z` (semver). Every change to a skill lands under
**Unreleased** first and moves into a version when it is tagged. Each version opens with short,
plain notes for people (`### Highlights`, `### New`, `### Fixed`): what changed for them, in a
sentence each. The technical bullets go under `### Details`. The GitHub release shows only the
part above `### Details`.

## Unreleased

### New

- **Milestone review in warden.** After a milestone batch merges, warden offers a cheap-model coherence review in rounds (contradictions, duplicated rules, size, docs vs code, TUI vs CLI gaps, or a topic you name); each round's findings go to one fix item.

### Fixed

- `smithy auto serve` no longer prints `poll: github: failed to run git` into the chat when it runs outside a git checkout: it skips the github source there quietly.
- **Faster CI.** Go and bun caches; the harness tests split into two jobs (TUI and web, the rest); the demo, every cross-build target and the Windows launcher run only on `release/*` pull requests and on `main`; Blacksmith 4vcpu runners for test, build and demo, 2vcpu for lint, integration, web and the check gate.

## v0.23.1 — 2026-11-25

### Highlights

- **One install, everything in it.** `install.sh` no longer asks harness or skills: it installs the full `smithy` binary and copies the skills into every agent it finds, without Node. `--no-skills` skips the skills.

### New

- `smithy update` updates the binary and the skills to the latest release; `--check` only compares versions.

### Details

- install: `install.sh` always installs `smithy-harness-*` and then runs `smithy skills install` (`--no-skills` skips it); `--harness` and `--skills` are accepted no-ops with a stderr note; `~/.smithy/kind` is gone. ADR 0014 supersedes the skills build as an install choice (ADR 0009); release assets are unchanged.
- update: new `internal/selfupdate` downloads `install.sh` from `main` over HTTPS (30s timeout, 1 MiB cap) to a temp file and runs `sh <file> --update [<version>]`, streaming output and returning its exit code. `--check` reads `releases/latest` (with `GH_TOKEN` when set). A binary other than `~/.local/bin/smithy` (`$XDG_BIN_HOME`) is refused unless `--force`.

## v0.23.0 — 2026-11-25

### Highlights

- See it run: the [demo](https://github.com/Zovaris/smithy/blob/main/docs/demo.md) walks first run, a session, tool calls, a decision and the web on a phone.
- Pick a model from a list instead of typing its name: each agent reports its own models (OpenCode and Gemini over ACP, Claude Code's aliases, each API provider's model list with OpenRouter prices). The web's Model field is a searchable select with the agent default first; `/model` and `ctrl+l` in the TUI show the same list with a filter. "Other" keeps any name you type.
- **One sandbox: the OS one.** The Docker/podman container sandbox is gone. `auto.sandbox` is `off` (default) or `native` (Seatbelt on macOS, bubblewrap on Linux) for Claude Code, Codex and the API backend's shell.

### New

- First run shows every agent CLI and API key on the machine as ready or missing, with the one command that fixes a missing one; Claude Code is found even off PATH. Pick the agent and model new sessions start with (`ctrl+l`, `/agent`, `/model` in the TUI; Setup in the web), and choose what goes into every session: smithy's MCP server and smithy skills (`/skills`) (harness build).
- **Dangerous-command guard, on by default.** An agent's `rm -rf /`, force push to main, `git reset --hard`, disk format, `curl | sh`, `chmod -R 777` or deletion of `~/.smithy` stops and asks you (run once, always allow, deny) in the TUI, web or chat. Nothing else asks. Fetch stays open (HTTPS only); domains you put in `guard.fetch_block` ask the same way, and `smithy guard suggest --add` allows GitHub, model APIs, package registries and docs sites in one step.
- The daemon runs agent sessions: start one with an agent, a folder and a first message, keep talking to it, stop it, and see every session with its status (harness build).
- **One command.** `smithy` (harness build) starts the daemon in the background when none runs and attaches; it keeps working after you quit, exits on its own after 30 idle minutes, and `smithy stop` ends it. `smithy --standalone` runs a private daemon that stops with the chat.
- API-backend costs now come from OpenRouter's public price list, refreshed daily and kept for offline use, so new models are priced without a smithy release.

### Fixed

- A permission question shows the whole call, and secrets in session output are hidden before they are saved.
- The background daemon no longer exits while a session is still working.
- Stopping `smithy auto serve` waits for the cycle it is running.

### Details

- daemon: stopping a session waits (up to 10s) for its runner goroutine to finish its last store writes, so a stopped turn can no longer write after the stop returns or overwrite the status of the turn that resumes it.
- lint: golangci-lint v2.15.0 (`.golangci.yml`: errcheck, govet, staticcheck, gosec, revive, gocritic, ineffassign, unused, misspell, bodyclose, unconvert; both builds) runs in the CI `lint` job and in `tests/run.sh` when installed; each disabled rule carries its reason in the config. The CI `web` job adds `bun check` (type check) and Oxlint (`npm run lint`, warnings fail). Every finding is fixed or marked with a reason.
- models: new `internal/models` discovers each agent's models (ACP `session/new` config option or `models` field; `opencode models` as fallback; Claude Code aliases plus `model` from `~/.claude/settings.json`; `/v1/models` of Anthropic, OpenAI, DeepSeek, OpenRouter and Ollama, Gemini `v1beta/models` with generateContent) and caches each list in `~/.smithy/cache/models/<agent>.json` for a day, keeping the last good list when a refresh fails. `GET /v1/agents/{id}/models[?refresh=1]` serves it per server; `acp.Models` opens a session without prompting to read the list.
- demo: `smithy daemon --demo` runs every session on a scripted agent (read, edit, test run, one decision; no CLI or key). `demo/run.sh` records the TUI with VHS (`demo/tui.tape`) and the web with Playwright (`web/scripts/demo-shots.mjs`, `DEMO_CHROME` for an installed browser) on a throwaway home; the CI `demo` job uploads the tape and screenshots as the `demo` artifact. `docs/demo.md` shows the flow.
- tui: the chat composer is three rows tall by default, grows with its text up to ten rows then scrolls, and shrinks back after sending; shift+enter, alt+enter or ctrl+j break the line (footer hint), pasted newlines are kept, and the footer drops its last hints before the agent and cost when narrow.
- tui: in 256-color terminals the steel dither ramp uses the grey indices 233–239 instead of downsampled hex, so the header no longer turns navy.
- daemon: claude sessions run with `--output-format stream-json --verbose`; each tool call (name, args, output, error, time) becomes a `session_tool` event and the result line gives the reply and usage, instead of raw JSON as text. `ToolEvent` gains `took`.
- tui: chat rebuilt on the Claude Design canvas (Steel tiers, anvil header that moves only while a session works, decision strip with ember button, minimal / expandable / verbose blocks, agents side panel, floating ctrl+k palette). The composer is a textarea; `/` opens a filterable popup with every command and its description, `@` completes files, `?` opens help. The first message starts a session on the last used agent (`POST /v1/sessions`), later ones go to it; session messages, tools, diffs and cost render per session, tab switches between them. New commands: /new, /sessions, /stop, /connect, /web, /remote, /log. Daemon events (daemon_up, poll errors) go to a status line and /log, never the transcript; esc clears the input instead of quitting. Screenshots at 160x48 and 100x30 in `docs/design/tui-v023/`.
- web: Temper design for approval (Sephiro theme `web/src/theme/temper.css`, light/dark, mock and screenshots in `docs/design/web-v023/`).
- web: rebuilt on Temper. Sessions sidebar with status per session, centered conversation (markdown, folded tool calls, inline decisions with ↵ for the safe answer), one composer with `/` commands and skills, right rail with Changes (diff, line comments sent with the next message), Decisions and Agents; new-session page picks folder, agent and model and starts from a skill. Phone: one column, sessions and changes as bottom sheets, composer pinned. Light/dark follows the system with a toggle. Talks to `/v1/sessions`, `/v1/decisions`, `/v1/guard`; old `#/sessions?s=` links still open. Screenshots of every flow at 1440 and 390 px in `docs/design/web-v023/built/`.
- remote: several daemons at once. `smithy servers [list|add|rm|use|discover]` keeps saved servers in `~/.smithy/servers.json` (0600) with status per server (online, version, ready agents, sessions); `discover` reads `tailscale status --json`, probes each online peer in parallel (2 s) and caches 5 min. Bare smithy and `smithy chat` take `--server <name|url>` / `SMITHY_SERVER`, else the saved default; `smithy attach <url>` stays. `/connect` in the TUI is a picker: local, saved servers with their agents and their sessions grouped under each, Tailscale peers running smithy, and Enter host; the pick is saved as default and the chat reconnects there (a session picked on another server opens after the switch). The web sidebar lists the other servers with status and sessions and switches the tab to one through the daemon's new `/v1/servers` and `/v1/servers/<name>/v1/...` proxy (daemon token only).
- daemon: `daemon.listen` adds a second listener for other machines (see the security line below for what it accepts). `smithy pair` makes a one-time code (5 min, one use) that `smithy servers add <name> <url> --code` trades at `POST /v1/pair` for a device token; `smithy pair list|revoke` manage devices (`~/.smithy/daemon.devices.json`, hashes only). `GET /v1/hello` is the one public route (smithy, version) so discovery can tell a daemon. No mDNS, no QR yet.
- daemon (security): `daemon.listen` accepts only a Tailscale IP (100.64.0.0/10, fd7a:115c:a1e0::/48) since the listener is plain HTTP; other addresses (LAN, `0.0.0.0`) are refused with a pointer to a TLS proxy such as `tailscale serve`. The remote listener takes paired device tokens only, never the daemon token. `POST /v1/guard/suggested`, `PUT /v1/setup/kit` and `POST /v1/setup/skills/install` need the daemon token, so a paired device cannot widen `guard.fetch_allow` or change settings. Wrong pairing codes lock out only their own address for 15 minutes (5 tries) instead of burning every open code after 50 failures anywhere.
### Highlights

**One sandbox: the OS one.** The Docker/podman container sandbox is gone. `auto.sandbox` is `off` (default) or `native` (Seatbelt on macOS, bubblewrap on Linux) for Claude Code, Codex and the API backend's shell.

### Details

- New `internal/guard` and `smithy guard check|hook|list`: blocklist regexes (defaults plus `guard.block`), `guard.allow` skips, `guard.enabled` (default true). The API loop's shell tool runs it after its rules; Claude Code gets it as a PreToolUse hook through the run's kit settings. Questions go to the daemon as `guard:<key>` asks (`POST /v1/asks` now accepts that id); no daemon or no answer within `ask.timeout` denies; "always allow this pattern" appends the pattern to the user's `guard.allow`.
- New `internal/guard` and `smithy guard check|hook|list|fetch-check|suggest`: blocklist regexes (defaults plus `guard.block`), `guard.allow` skips, `guard.enabled` (default true). The API loop's shell tool runs it after its rules; Claude Code gets it as a PreToolUse hook through the run's kit settings. Questions go to the daemon as `guard:<key>` asks (`POST /v1/asks` now accepts that id); no daemon or no answer within `ask.timeout` denies; "always allow this pattern" appends the pattern to the user's `guard.allow`.
- Fetch guard: the API loop's fetch is open outside the OS sandbox (`auto.sandbox_domains` still bounds sandboxed runs) and checks `guard.fetch_block` / `guard.fetch_allow` domain globs, redirects included; the egress proxy runs the same check on allowed tunnels. `GET /v1/guard` returns the lists and suggested hosts, `POST /v1/guard/suggested` adds them. `guard.enabled`, `guard.allow` and `guard.fetch_allow` are read from the user config only, so a repo can't loosen the guard; an allow rule cancels only the block match in its own command part; an unreadable hook event denies.
- Removed: `auto.sandbox container`, `auto.sandbox_image`, `auto.sandbox_network`, `auto.sandbox_memory`, `auto.sandbox_proxy_binary`, the `smithy-proxy` sidecar and its networks, docker/podman detection, and `sandbox/Dockerfile`. A user config still on `container` stops workers with a message to set `native` or `off`; `smithy doctor` fails on it.
- `smithy proxy serve` runs natively and now listens on `127.0.0.1:8877` by default (was every interface), so bound keys stay on this machine.
- `smithy cleanup builds` no longer lists `smithy-*` images; `smithy uninstall` still removes the images and networks older versions left, when docker or podman is on PATH.
- `internal/wait`: `TestMainEventsOption` no longer fails when the first poll starts after the test's update; the fake `gh` marks served calls and the test writes the change only after the baseline poll.
- `GET /v1/setup` (detected agents, default agent/model, kit, installed skills and MCP servers per harness), `PUT /v1/setup/default`, `PUT /v1/setup/kit`, `POST /v1/setup/skills/install`. Sessions take a `model` (`AGENT_MODEL`, refused if it starts with `-`); `GET /v1/sessions` adds `last_model`. New settings `session.agent`, `session.model`, `kit.mcp`, `kit.skills`; the CLI runner adds the folders of CLIs found off PATH (`~/.claude/local`, `~/.local/bin`, Homebrew…) to the agent's PATH. Screenshots in `docs/design/setup-v023/`.
- `POST /v1/sessions`, `POST /v1/sessions/{id}/messages`, `POST /v1/sessions/{id}/stop` and `GET /v1/sessions`; session events (messages, live text, tool calls with args and output, diffs, usage, status) stream on `/v1/events` with the session id. `api:` sessions run in the daemon and turn a refused tool call into a `perm:<id>/<n>` decision; other agents run `smithy agent` per turn. Chat-thread replies reach the session; sessions left running by a stopped daemon show as failed.
- `smithy chat`/bare `smithy` auto-start the daemon (`daemon.StartBackground`) and show "background, smithy stop ends it" in the footer; `--standalone` runs `daemon.Standalone` in-process on a free port without `daemon.json`.
- New `daemon.idle_exit` (default `30m`): a `--bg` daemon exits with no open event streams, no API requests, no orchestrator session and no planning for that long; `smithy daemon --idle-exit <d>`.
- New `smithy stop` (SIGTERM, waits 10s). `GET /v1/state` reports `version`; the chat warns on a mismatch, below the replayed history.
- `apiloop.Cost` reads `https://openrouter.ai/api/v1/models` (cached at `~/.smithy/cache/model-prices.json`, refetched after 24h, last good cache kept when offline or the answer is bad) instead of the hand-kept table; an unlisted model still has no cost, never a guess.
- daemon: a permission question shows the whole tool call, and session tool output and text are redacted before they are stored.
- daemon: a working session counts as running work, so the idle exit never stops the daemon under it.
- auto: `smithy auto serve` waits for the cycle in flight before it stops.

## v0.22.0 — 2026-11-25

### Highlights

**Your own API keys.** smithy can run its own agent on your Anthropic, OpenAI, Gemini, OpenRouter, DeepSeek or Ollama key, with no CLI needed (harness build).

**Stored keys are encrypted.** `~/.smithy/auth.json` is now encrypted at rest, and older files are converted on first use without losing keys.

**Code mode, off by default.** The API agent can run one small JavaScript program that calls its tools, once you turn it on.

**A benchmark.** `smithy bench` runs smithy's own task set and compares runs.

### New

- `smithy bench run --agent <cli>` runs smithy's own 20-task benchmark and records pass rate, time, tokens and cost.

- `smithy bench compare <a> <b>` shows how one run did against another; the web UI has a Bench screen with the same view.

### Details

- `~/.smithy/auth.json` is sealed with AES-GCM under `~/.smithy/auth.key`, kept next to it; an existing plaintext `auth.json` is encrypted in place on first use, keys kept. No OS keychain, no password prompts. Under `--profile auto` the claude and opencode allow-lists deny reading both files, but any process running as the user can read them unless the optional OS sandbox (`auto.sandbox`, off by default) is on.

- `smithy agent --backend api --model <provider>/<model>` (and `auto.agent api:<provider>/<model>`, harness build) runs smithy's own agent loop (`internal/apiloop`) on the key from `smithy auth set <provider>`. Provider adapters: Anthropic (prompt caching), OpenAI (Responses API), Gemini, and OpenRouter, DeepSeek or Ollama through openai-go's Chat Completions. Text streams to the log; tool, permission and usage events match the CLI paths; 429/5xx are retried with backoff; cost comes from list prices or the provider's reported cost (ADR 0012).

- Code mode (`agent.code_mode`, off by default): the API loop can offer `run_code`, one JavaScript program calling the loop's tools through typed `tools.<name>(args)` bindings, each call still permission-checked. It runs in QuickJS compiled to WebAssembly (fastschema/qjs on wazero, no cgo) with a 64 MB memory cap, a 30 s time cap and no files or network (ADR 0013).

- `internal/apiloop/tools`: the API loop's core tool set: read (numbered lines, offset/limit paging, 2000 lines / 50 KB cap), write, exact-match edit, apply_patch for OpenAI models, shell (timeout, output over 50 KB saved to a file), glob and grep (`internal/search`, 100 results), fetch (https, `auto.sandbox_domains` hosts and redirects only). Every call passes the `--profile auto` rules (AutoTools allowed, AutoDenied and SecretDenied first, symlinks resolved, shell lists and pipelines checked part by part, substitutions refused); a refused call becomes a decision. The rules are a best effort: without a sandbox the agent's shell can read anything the user can.

- `internal/bench`: 20 fixture Go repos (bugs, features, refactors, docs) with tests as the oracle and a reference solution each; every task runs in a fresh git repo through `smithy agent --profile auto`, tests are restored before grading, results go to `~/.smithy/bench/<id>.json`. `api:` agents wait for the api backend.

- `smithy bench runs` lists saved runs; runs are named by id, unique prefix, `latest~N` or file path; `compare --json` gives the machine form. The daemon serves `GET /v1/bench` and `GET /v1/bench/compare?a=&b=` (ids only, no paths) to the web UI's plain Bench screen.

- API backend `--readonly` offers only read, glob and grep, and `Permit` refuses every other tool.

- API backend under `auto.sandbox native` runs shell commands under the OS sandbox (Seatbelt on macOS, bubblewrap on Linux; same writable and secret paths as claude workers, no network); `auto.sandbox container` refuses `api:` workers. Off (default) is unchanged.

- API loop shell: commands run in their own process group (Windows: `taskkill /T`), so a timeout or cancel kills background children too. Long output goes to a per-run temp dir removed when the run ends.

- API loop `read` refuses non-regular files (devices, fifos) and reads at most 64 MB; `fetch` dials through `HTTPS_PROXY` when set, otherwise refuses hosts resolving to loopback, private, link-local or CGNAT addresses.

- API loop: a retried reply no longer repeats text already streamed; cost needs an exact model id or one with a snapshot date (`-20250929`, `-2025-08-07`, `-latest`), adds mini/nano rows, and is unknown (nil) for other models.

- Code mode: the WebAssembly memory cap is the documented 64 MB (was twice that); inner tool calls report tool and permission events through the loop's sink.

- `auth.json` and `auth.key` writes fsync the file and its folder; an empty `auth.json` holds no secrets and no longer blocks `smithy auth set` (a `.bak` beside it is left alone).

- Config lock: a stale lock is taken over by an atomic rename and a pid recheck, so two writers can't both proceed.

- `smithy bench`: SIGINT/SIGTERM kills the running agent and oracle process trees and removes the temp repos (unless `--keep`); the oracle kills its whole process group on timeout.

## v0.21.0 — 2026-11-25

### Highlights

**Same tools in every CLI.** `smithy mcp` gives every agent smithy drives the same board, search, secret calls and questions, whatever the CLI (harness build).

**More agents.** Gemini CLI, OpenCode and other agents that speak ACP run under smithy with their tool calls, usage and permission requests shown like the others.

### New

- When every account of one harness is limited, a pool run carries on with the next harness in `pool.order`.
- A failed `smithy auto serve` issue gets one more try on a stronger model.
- The weekly report and History show what happened to each auto PR.
- Releases bundle ripgrep, and `smithy search --code` searches the whole repo fast.
- smithy installs to `~/.local/bin`, and `smithy uninstall` removes it cleanly.

### Fixed

- OpenCode runs follow the same allow-list as Claude and can't read your keys; `smithy doctor` warns when a CLI can't block those reads.
- Help is clearer, and a mistyped command or option suggests the right one.
- The web UI is built in CI instead of committed to the repo.

### Details

- `smithy help` teaches the tool: a short intro, then the commands people run grouped as Start here, Daily, Accounts and secrets and Maintenance (harness-only commands marked "(harness build)" in the skills build); `smithy help --all` adds the commands the skills call. `smithy help <command> [sub]` and `<command> --help` show one layout: summary, usage, examples, see also and exit codes, wrapped to 80 columns, colored only on a terminal without `NO_COLOR`. An unknown command or option suggests the closest one (`did you mean …?`) instead of dumping the list, and a usage mistake prints only that command's usage line.

- `pool.order` (e.g. `claude,codex`): when every account of one harness is limited, a pool run continues on the next pooled harness, logs the switch as a `pool_fallback` event and goes back once the limit resets.

- The built web UI is no longer committed (ADR 0011): `npm run build` in `web/` writes the git-ignored `internal/web/dist/app`; without it the harness build serves a committed placeholder page saying how to build it, so a clean clone builds without Node. CI and the release build the UI first, the release fails if the placeholder would ship, and the stale-dist check is gone. `.gitignore` covers coverage, test and editor output.

- `internal/acp`: a stdlib JSON-RPC client for one ACP prompt turn (initialize, session/new, session/prompt); it offers no fs or terminal capability and refuses those requests.

- `smithy agent` with `AGENT_PROTOCOL=acp`: `gemini --experimental-acp` or `opencode acp`; message chunks go to the log, completed or failed tool calls and permission answers to `tool` and `permission` events, usage to the sidecar and a `usage` event; read-only runs reject permissions and log a decision; the agent gets 5 s to exit after its turn before its group is killed.

- `decisions.Add` and `usage.Write` exported for that path.

- Install layout (`internal/layout`): versions in `$XDG_DATA_HOME/smithy/versions/<v>` (default `~/.local/share`), dev builds in `.../smithy/dev`, the command at `$XDG_BIN_HOME/smithy` (default `~/.local/bin`); Windows `%LOCALAPPDATA%\smithy\versions` and `%LOCALAPPDATA%\Programs\smithy`. `install.sh` migrates `~/.smithy/bin/<v>` (leaving links there), offers the PATH line on a terminal (default no) and records the file in `~/.smithy/path-added`. The launchers (`shared/smithy`, `smithy.cmd`) look in the new layout first, then the old one; `smithy setup path add`, `smithy cleanup builds` and `smithy doctor` follow.

- `auto.sandbox_proxy_binary` unset now defaults to `~/.local/share/smithy/smithy-linux-<arch>` (`$XDG_DATA_HOME`), falling back to the legacy `~/.smithy/bin/smithy-linux-<arch>` when only that one exists.

- `smithy uninstall [--yes] [--purge [--confirm-purge]]` and `uninstall.sh`: remove only what smithy installed. `smithy skills install` now records the skill folders it writes in `~/.smithy/installed-skills`, so uninstall never touches other skills.

- `smithy auto serve` checks the PRs of launched items each cycle and records an `outcome` event per change; `smithy report week` lists them under "Auto PR outcomes" and the TUI History view shows them as results.

- opencode runs under `--profile auto` follow the same allow-list as claude, and opencode (auto and the intake planner) can no longer read `~/.smithy/auth.json` or other secrets. `smithy doctor` says when the configured CLI can't deny those reads.

- `internal/agents/guard.go`: `OpenCodePermission` builds ordered `OPENCODE_PERMISSION` rules from `AutoTools`/`AutoDenied` plus read/edit denies on the secret paths (`--readonly`: edit denied, bash denied, secret reads denied); `SecretGuarded` names the CLIs that can deny secret reads (claude, opencode). codex, gemini, pi, cursor and dsh keep running unchanged; the limit is documented in `smithy agent` help and reported by `smithy doctor`.

- `smithy mcp [--readonly]` (harness build, `internal/mcp`): a stdio MCP server, newline-delimited JSON-RPC 2.0 (protocol 2024-11-05 to 2025-06-18), with `search`, `board_list`/`board_show`/`board_claim`/`board_set` (claims held by the run's card), `secret_call` (the egress proxy in process: bound hosts and methods, asks held for approval, key scrubbed from the response) and `ask` (records the question on smithy daemon, shows it on the run's warden card as needs_decision and waits for the daemon's answer). Unknown arguments, wrong types, oversized or multi-line values are refused; output is capped at 64 KB and cleaned of escapes. `smithy agent` injects it into every run through the kit (`SMITHY_MCP_CARD` from the run's card; `--readonly` runs get search and board reads only; not into container runs or a custom `AGENT_CMD`), and `--profile auto` allows `mcp__smithy`.

- Added: `auto.retry_stronger` (default `true`): the first `smithy auto report <ref> blocked` for a launched issue requeues it at the head of the queue on the next tier (cheap → mid → strong; unlabeled → strong), prints `auto retry <ref> on <tier>` and records a `retry_stronger` event; a second failure or one on strong is reported blocked.

- Injection kit (`agents.Inject`): per run, smithy can give a driven CLI MCP servers, skills, an appended system prompt and settings without touching the user's global config. Claude gets `--mcp-config`/`--strict-mcp-config`, `--plugin-dir`, `--append-system-prompt` and `--settings` (merged with smithy's own note and sandbox settings); codex a `CODEX_HOME` under the run dir with a `smithy` profile (auth linked from the user's home); opencode `OPENCODE_CONFIG_DIR`; gemini a settings file through `GEMINI_CLI_SYSTEM_SETTINGS_PATH`; pi `--skill` and `--append-system-prompt`. Pieces a CLI can't take are returned as unsupported.

- `smithy search --code [--regex] "<query>"` searches the whole repo (ignored, hidden and binary files skipped) through the new `internal/search`: the ripgrep shipped next to the binary, rg on PATH, `git ls-files` in a repo or a stdlib walk, with caps on matches, line length (220) and bytes (64 KB); `SMITHY_RG=<path>|off` overrides. Docs search is unchanged.

- Releases ship ripgrep 14.1.1 per platform as `smithy-rg-<os>-<arch>` plus `ripgrep-LICENSE.txt` (MIT or Unlicense): `scripts/fetch-ripgrep.sh` downloads each archive against pinned sha256 sums, and the build attestation covers them. `install.sh` installs it next to smithy after the same checksum and attestation checks, best effort.

- `smithy mcp`'s `ask` records its question on smithy daemon (new `POST /v1/asks`, `decide:<batch>/<item>/<key>` ids only) and reads the answer back from the daemon (new `GET /v1/decisions/{id}`), never from the warden card, which a worker can write; with no daemon running, `ask` refuses. `--profile auto` denies Edit/Write on warden card JSON and reads of `~/.smithy/daemon.token` (Read, shell, and the native sandbox's `denyRead`).

- `smithy proxy approve|deny` refuses under `SMITHY_AGENT_PROFILE=auto`, and `--profile auto` denies `smithy proxy approve|deny`, `smithy chat`, `attach`, `web` and `tui`: answers come only from the user's channels.

- ACP runs under `--profile auto` check each permission request against the auto rules: commands must match an `AutoTools` Bash rule and no `AutoDenied` one (compound, piped, redirected or `$` commands rejected), edits stay inside the worktree and off denied paths, reads off secret paths; fetch, delete, move and unclassified requests are rejected and logged as decisions. `acp.Permission` carries the tool kind, command and paths.

- `SecretDenied` adds glob and variable forms (`~/.*`, `$HOME`, `${HOME}`, `.smithy/auth*`, `*.s*/id_*`, bracket globs, `id_rsa`/`id_ed25519`/`id_ecdsa`); only the native or container sandbox fully protects secrets.

- `smithy search --code` (git and walk engines) reads only regular files, so a tracked symlink to a file outside the repo is skipped.

- An ACP run stops its child after a protocol error too, not only after a clean turn.

- `smithy agent` removes the temp `smithy-kit-*` dir (MCP config, a `CODEX_HOME` linking `auth.json`) after a run without `--log`.

- The launcher no longer re-points `~/.local/bin/smithy` at a checkout's dev build; only versioned installs are linked.

- `smithy uninstall --purge` and `uninstall.sh --purge` remove the smithy home only when it is smithy's (a `.smithy-home` marker, written by `install.sh` on a fresh home, or smithy's own files and nothing else); otherwise they refuse and say why.

- `install.sh`'s migration from `~/.smithy/bin` never deletes: a version or dev build already at the target stays, and the old copy is kept.

- Uninstall removes a copied (not linked) `~/.local/bin/smithy` when it matches an installed version byte for byte, and `uninstall.sh` on Windows (Git Bash) removes `%LOCALAPPDATA%\smithy` and `%LOCALAPPDATA%\Programs\smithy`.

- Warden workers get `SMITHY_WARDEN_ITEM=<batch>/<item>`, and `smithy auto report` refuses when it is set: only the orchestrator reports, so a worker can't trigger `auto.retry_stronger` itself.

- `TestWebScreensServed` skips when only the placeholder web UI is embedded, and fails instead when `SMITHY_REQUIRE_WEB_BUILD` is set (CI's harness job sets it after building the UI).

## v0.20.0 — 2026-11-25

### Highlights

**A web UI.** In the harness build, `smithy web open` shows your sessions, decisions, agents and board in the browser.

**Answer from chat.** The daemon posts each session in its own Telegram topic, Slack thread or Discord thread, with buttons to answer decisions, and your reply there goes to that session.

### New

- **Ideas from chat:** write `idea: <what you want>` in the main Telegram chat and smithy plans it, then adds the tasks to the board only after you say yes.

- **Weekly report:** `smithy report week` says in plain words what shipped, what failed, the time and cost per harness, and which decisions wait on you; the daemon posts it every Monday if you turn on the `report` kind.

- **Who may answer:** the chat connectors only listen to the users you list in `connect.telegram.users`, `connect.slack.users` or `connect.discord.users`.

### Details

- Chat intake: a main-chat message from a listed user that starts with `idea:` or `/idea` is planned by a read-only `smithy agent` (smelt plan, idea fenced as untrusted data, at most 12 `TASK:` lines kept, 20 min timeout) in the daemon's working directory; the plan is recorded (`intake_plan`) and asked as decision `intake:<id>` (yes/no) in the main chat; yes adds the tasks to the board as todo with label `intake`, no adds nothing.

- `internal/connect`: one notification layer (each enabled connector gets a note once, kinds per connector via `connect.<name>.kinds`), a `Connector` interface (thread, send, update, buttons, receive) and the Telegram connector (forum topics, inline keyboards, `getUpdates` long polling, token only from `smithy auth`).

- Kinds `pr_merged` and `ci_red` are new and on by default for connectors.

- `internal/report` and `smithy report week`: the last full week (Monday to Sunday) from the events log; open decisions are items whose latest stage is `needs_decision` or `ready`. Time per harness counts `working` spans of items that reported usage. `smithy daemon` posts it once per week (kind `report`, opt-in: add it to `connect.<name>.kinds`) from Monday 09:00 local.

- `smithy daemon` feeds it from its store (cursor `notes:connect`, no replay of history), answers decisions from button presses through the same path as `/v1/decisions`, records topic replies as `user_input` events (`via: chat`); inbound is off until `connect.telegram.users` lists who may answer, and a button counts only on the message that asked it, and turns off serve's own Telegram flush when a connector is set up.

- Discord connector: a public thread per session in `connect.discord.channel`, button components for decisions (five per row, every press acknowledged), the Gateway (GUILD_MESSAGES and MESSAGE_CONTENT intents) for thread replies and presses, only from `connect.discord.users` in the channel's own server; token only from `smithy auth set discord`, origins fixed in code, mentions off, markdown escaped. It shares the stdlib websocket client in `internal/connect/ws.go` with Slack, which now also rejects masked server frames, oversized or fragmented control frames and stray continuations.

- Slack connector (`internal/connect/slack.go`): Socket Mode over a stdlib websocket client (`ws.go`, only `wss://*.slack.com` URLs), a thread per session rooted at a message naming it, Block Kit buttons for decisions, `chat.update` to show the answer; tokens `slack` (`xoxb-`) and `slack_app` (`xapp-`) only from `smithy auth`; inbound only from `connect.slack.channel` and users in `connect.slack.users`, bots and edited or system messages ignored, every envelope acknowledged.

- Web UI shell (harness build): `web/` (Preact, `@zovaris/sephiro` 1.2.0, Vite) with smithy's
  Steel theme, hash routing for sessions, decisions, agents and board (alt+1–4), and an auth
  gate. Built into `internal/web/dist`, embedded with `go:embed`; the daemon serves it at `/`
  (loopback, or `*.ts.net` for a login in `daemon.allow`) with a strict CSP. ADR 0010.

- `smithy web [open]` prints or opens the daemon's web link with the token in the URL fragment.

- CI `web` job: Vitest, typecheck, the 30 KB gzip first-load budget, and a stale-dist check.

- Web UI screens over `/v1/events` (fetch-streamed SSE, resumes after the last seq): sessions
  with the chat transcript in minimal, expandable and verbose modes (`v`), virtualized for 10k
  messages; decisions answered through `/v1/decisions`; agents; a board by session state; and
  the ctrl+k command palette. Each screen is its own chunk.

- Connector kinds: with no `connect.<name>.kinds` and no user-set `notify.kinds`, a connector now sends the daemon defaults (notify kinds plus `pr_merged` and `ci_red`); before, the `notify.kinds` default silently dropped them. `report` is no longer on by default.

- `POST /v1/decisions` refuses cross-site requests: the body must be `Content-Type: application/json` (415 otherwise), `Sec-Fetch-Site: cross-site` and an `Origin` whose host is not the request host get 403; the daemon client sends the JSON type.

- Intake review: the approval message lists each task with its body (first 300 characters, line breaks kept, up to 3500 characters in chat), and the board source of `smithy auto` skips tasks labelled `intake` until someone removes the label after reviewing them.

- Every `--readonly` claude run (the intake planner among them) carries the same secret deny rules as `--profile auto` (`agents.SecretDenied`: `Read` and shell access to `~/.smithy/auth.json`, pool accounts, ssh, gh, aws, netrc).

- The secret scan (`smithy proof`, `proof.Redact`) also knows Telegram and Discord bot tokens and Slack app tokens (`xapp-`); `proof.Secret` names the rule a string matches.

- Chat intake drops a plan whose task title or body matches a secret scan rule (`proof.Secret`), and logs only the rule name.

- The events log masks secret-looking strings in `detail` before writing (`proof.Redact`), and the weekly report clips an orchestrator error to 80 characters and redacts its whole text.

- Connector sends that fail are queued in `connect.json` (at most 256, oldest dropped with a log line) and retried by the hub with backoff (5 s doubling to 10 min) until they land, so a chat outage no longer loses decision prompts after the notes cursor moves on.

- Telegram: the `getUpdates` offset is kept in `~/.smithy/telegram.offset` so a restarted daemon does not reprocess presses and replies.

- Connectors honour HTTP 429: Telegram `parameters.retry_after`, Slack `Retry-After` and Discord `retry_after` are waited out (at most a minute, three tries) before the send fails into the retry queue.

- The intake planner runs under the daemon context (stopped on shutdown) and keeps at most 1 MiB of its stdout and 4 KiB of stderr.

- Discord Gateway: a full 64-event buffer now makes the reader wait up to 10 s for `Receive` before dropping an event, and a drop is logged through the hub instead of happening silently.

- `smithy doctor` warns when `daemon.allow` is set: the daemon trusts `Tailscale-User-Login` on `*.ts.net` requests, so it trusts any local user on this machine; the setting description says so.

## v0.19.0 — 2026-11-24

### Highlights

**A chat for the harness.** Run `smithy` in the harness build and you get a fullscreen chat with your agents. It stays fast even on long sessions, works with the mouse, and keeps anything waiting on you pinned at the top.

**Install without Node.** `smithy skills install` puts the skills in every agent CLI you have. No `npx` needed.

**The installer asks what you want.** `install.sh` asks "harness or skills?" and remembers your answer on `--update`.

### New

- **Chat modes:** press `v` to switch between minimal, expandable and verbose.
- **Agents and commands:** `tab` shows your agents, `ctrl+k` opens a command palette, and `/` runs commands.
- **`smithy doctor --fix`:** fixes the problems doctor finds, asking before each one.
- **`smithy cleanup builds`:** frees space from old smithy versions, images and unused worktrees. It shows what it would delete first.

### Fixed

- `install.sh --update` works without Node.
- `doctor --fix --yes` never grants permissions without asking you.

### Details

- Added: `smithy doctor --fix` lists each fixable check with its exact change (remove a stale `daemon.json`, `chmod 600` the daemon token, add `Bash(smithy pr merge:*)` to `permissions.allow` in `~/.claude/settings.json`) and applies it on `y`; `--yes` applies all except the permission grant, which needs an explicit `y`, and without a terminal it applies none. It only touches your own files and prints what changed.
- Added: `smithy cleanup builds` lists old smithy builds in `~/.smithy/bin` (keeps the current one and one previous; per-checkout dev builds after 14 days), stale `smithy-*` container images other than `auto.sandbox_image`, and unlocked worktrees of the current repo with no changes, no untracked and no ignored files, with no commits beyond the base branch, with the space each takes; `--yes` deletes them. Worktrees with uncommitted changes are never touched.
- `install.sh` asks "harness or skills" on a terminal (default skills), takes `--harness` / `--skills`, installs the skills build without a terminal (and prints how to install the skill files), and `--update` keeps the installed kind and updates the skills with `smithy skills install` when Node is missing.
- Added: `smithy skills install [--harness <name>...]` installs the skills without Node: it downloads the release's `smithy-skills.tar.gz`, checks it against `checksums.txt` and copies the skills into each detected harness's skills folder (symlinked skills are left alone; installed skills are replaced, local edits included). The release publishes the archive; `install.sh` offers it after installing the binary (a hint without a terminal); `npx skills add` still works.
- Added: `smithy chat [url]` (harness build; bare `smithy` opens it on a terminal, else prints usage): a fullscreen chat over a local or remote daemon with the input pinned at the bottom. Events stream from `/v1/events` and resume after the last seq on reconnect. The transcript is virtualized: only visible rows are wrapped and drawn, and a benchmark test keeps a frame of a 10k-message session under 5 ms.
- `smithy chat` interaction: wheel scroll and click to focus; a click opens or closes a tool block. `v` (empty input) cycles minimal (a run of tools on one line), expandable (one header per tool) and verbose (every tool open, thinking and turn stats). `tab` opens the agents overlay, `ctrl+k` a command palette (waiting on you, actions, sessions, settings), and `/mode`, `/agents`, `/palette`, `/answer`, `/help`, `/quit` work in the input. Decisions waiting on you stay pinned at the top; an option's key or a click posts the answer to `/v1/decisions`.

## v0.18.0 — 2026-11-24

### Highlights

**A board and a daemon.** In the harness build, `smithy board` is a built-in tracker that can sync with Linear, and `smithy daemon` runs the autonomous loop behind a local API.

**Attach from a terminal.** `smithy attach` connects a terminal to the daemon, shows live events and lets you answer waiting decisions with the number keys.

### New

- The `smithy tui` dashboard has a History tab (key `0`) with the finished work of the last 30 days.
- The `smithy tui` dashboard has a new steel look, and a setting keeps its animation still.

### Fixed

- The `smithy tui` header draws the anvil again.

### Details

- CI runs lint, tests (one job per build), integration and builds as parallel jobs behind a `check` gate; pull requests build one target per OS, main builds every release target.
- `smithy tui` header draws the anvil again, shaded in Steel with Bayer dither and ember sparks.
- Added: `smithy board` (harness build), a built-in tracker in the SQLite store: `add`, `list`, `show`, `set`, `claim` (with expiry) and `sync`. `auto.sources board` lets the watcher read it. With `board.linear_sync true` it syncs with Linear by field ownership: Linear owns title, body, priority and labels, the board owns state, claims and PRs; edits are polled with an `updatedAt` cursor.
- Added: a harness build (`go build -tags harness ./cmd/smithy`, released as `smithy-harness-<os>-<arch>`) beside the stdlib-only skills build, with a SQLite store (`~/.smithy/smithy.db`, WAL, append-only events, migrations) on pure-Go `modernc.org/sqlite` (ADR 0009). In the skills build, `smithy daemon`, `attach` and `board` say how to get it.
- `smithy tui` restyled to Steel (TK-28): steel background tiers instead of boxes (a border only on the focused block), dim/base/strong text tiers, one ember for what runs or has focus, `?` for what waits on you, a half-block gradient header with ember sparks, a Bayer shimmer on running items and a dithered progress edge. Motion runs at 15 fps only while something moves; the new `tui.reduce_motion` setting keeps every frame still.
- Added: a History tab in `smithy tui` (key `0`): past days' finished work from the last 30 days of `~/.smithy/events.jsonl`, grouped by day with each day's count and known cost; each row shows the issue, PR, outcome, cost and finish time.
- Added: `smithy daemon` (harness build): the auto serve loop plus a loopback-only API (`GET /v1/state`, `GET /v1/events` as resumable SSE, `POST /v1/decisions` where the first answer wins) with every event in the SQLite store. Requests need `~/.smithy/daemon.token` or a `Tailscale-User-Login` in the new `daemon.allow`; `daemon.port` picks the port. In the harness build `smithy auto serve` starts the daemon or attaches to it; `smithy doctor` checks it.
- Added: `smithy daemon` tails `events.jsonl` and the proxy log into its store (rotation-safe, once per line across restarts) and turns proxy `secret_ask`s, ready items and `needs_decision` items into decisions (`secret:<id>`, `merge:<batch>/<item>`, `decide:<batch>/<item>/<key>`); an answer from any client goes back to the asker once (the proxy's approve/deny event, or `answer` on the card). Telegram alerts show the decision id.
- Added: `smithy attach [url]` (harness build), a terminal client of the daemon: live events, pending decisions answered with keys 1-9 (first answer wins), reconnect with backoff and resume from the last seq without repeating events. `smithy auto serve` attached to a daemon now also reconnects.

## v0.17.0 — 2026-11-24

### Highlights

**Safer container workers.** `auto.sandbox_network proxy` lets container workers reach only GitHub, the model APIs, package registries and the domains you allow. `smithy auth bind` lets a stored key be sent to one host without the worker ever seeing it.

### New

- Container workers can use the account pool, and each one mounts only its own account.
- Usage limits are now detected for OpenCode, Gemini CLI and Pi, so a limited account waits for its reset instead of only rotating.

### Fixed

- The proxy restarts when your stored keys change, so a new `smithy auth bind` takes effect.
- OpenCode workers can fetch their models under the default allow-list.
- Container workers are refused with host networking, and with harnesses the default image does not include.
- Stopping `smithy auto serve` in the middle of a cycle no longer leaves a stale heartbeat.

### Details

- Container refuses `auto.sandbox_network host` and, on the default image, harnesses it does not carry (only Claude Code and OpenCode); pool hints and help list every harness; docs match the proxy allow-list and restart rule.
- Fixed: the proxy sidecar restarts when `auth.json` changes, so a new `smithy auth bind` reaches it (a single-file mount kept the old file after a rewrite); `*.opencode.ai` is in the default allow-list so OpenCode workers can fetch their models.
- Added: the account pool works with `auto.sandbox container`: a pooled worker mounts only its own account dir and gets its config-dir variable; claude accounts use their own token (`smithy auth set claude:<account>`). Doctor allows the combination when every pooled account can run.
- `auto.sandbox native` now refuses Gemini CLI workers with the reason: its seatbelt profiles either open every host or need a proxy smithy doesn't ship, and on Linux it only offers docker/podman. Use `auto.sandbox container`.
- Account pool: usage limits are now detected for OpenCode, Gemini CLI and Pi too (each from its own error text, last lines of a failed run), so a limited account is marked until its reset instead of only rotating.
- Added: `auto.sandbox_network proxy` for container workers: an internal network whose only exit is the `smithy-proxy` sidecar (`smithy proxy serve`), which tunnels only to GitHub, the model APIs, package registries and `auto.sandbox_domains`, and logs what it refuses.
- Added: secrets for container workers that never see them: `smithy auth bind <name> <host> [METHODS]` lets the proxy send a stored key to that host (`http://smithy-proxy:8877/s/<name>/<path>`), dropping the worker's own auth headers and scrubbing the key from responses; writes are refused unless bound, or held for `smithy proxy approve <id>` with `smithy auth ask <name> on`.
- Fixed: stopping `smithy auto serve` mid-cycle could leave a stale heartbeat (`~/.smithy/auto/serve.json`) written by the abandoned cycle.

## v0.16.3 — 2026-11-23

### New

- The account pool now takes OpenCode, Gemini CLI, Cursor CLI, Pi and DeepSeek Harness accounts, each with its own settings folder.
- Usage limits are detected for Claude Code and Codex; the other harnesses only rotate between accounts.
- The README is rewritten to be easier to scan, with a diagram of autonomous mode.

### Details

- Account pool: OpenCode, Gemini CLI, Cursor CLI, Pi and DeepSeek Harness accounts, each with its own config dir (`smithy pool add <name> <harness>`). Usage limits are detected for Claude Code and Codex; the others rotate.
- README: autonomous mode diagram (`docs/auto.svg`) and the TUI's cost and settings views.
- README rewritten to be scannable: highlights, short tables, one-line examples. Details live in `docs/install.md`, `docs/harnesses.md` and `docs/autonomous.md`.
- Docs: per-harness guard under `--profile auto` (smithy allow-list is Claude Code only), native sandbox codex runs with network off.

## v0.16.2 — 2026-11-23

### Fixed

- `smithy config list` and the TUI say that `auto.sandbox_network` is required, and that `bridge` means open network access.

### Details

- Fixed: the `auto.sandbox_network` description in `smithy config list` and the TUI says the setting is required (empty or `none` refuses to start container workers; `bridge` is open egress).

## v0.16.1 — 2026-11-23

### New

- The sandbox image now includes OpenCode 2.4.0.

### Fixed

- Container workers get the credentials their harness needs, and a worker without one is refused with the fix shown by `smithy doctor`.
- Container mode refuses to start unless `auto.sandbox_network` is set.
- With no Docker host set and a dead Docker context, smithy finds the container engine socket on its own.

### Details

- Fixed: `auto.sandbox container` workers get their credentials, scoped to the harness (claude:
  `CLAUDE_CODE_OAUTH_TOKEN`/`ANTHROPIC_API_KEY`; codex: `OPENAI_API_KEY`; opencode: none unless
  its model names that provider). When the env lacks them they come from `smithy auth`, passed by
  name, never on argv; container env is visible to `docker inspect` and the engine socket. A
  worker with no credential its harness needs is refused and doctor fails with the fix (`claude
  setup-token`, then `smithy auth set claude`). On macOS the container runs as root inside the
  engine VM; on Linux as your uid.
- Fixed: container mode needs `auto.sandbox_network`; unset or `none` now refuses to start (claude
  couldn't reach its API, git couldn't push) and doctor fails. `bridge` is open egress with no
  domain allow-list; an egress allow-list proxy is planned for v0.17.
- Fixed: with `DOCKER_HOST` unset and a dead docker context, the engine socket is found (Kiri,
  OrbStack, Colima, Docker Desktop, `/var/run/docker.sock`), and docker is found at
  `~/.kiri/bin/docker` or `~/.orbstack/bin/docker` off PATH. Doctor prints the engine and socket.
- Added: the sandbox image ships OpenCode 2.4.0 (release tarball, digest-checked) next to
  claude-code. OpenCode has no smithy allow-list, so the container is its isolation; live-tested
  with `opencode/big-pickle` under Kiri.

## v0.16.0 — 2026-11-23

### Highlights

**Sandboxed unattended workers.** `auto.sandbox` can run unattended workers in a native or container sandbox, and a worker whose sandbox cannot start is not started.

**An account pool.** `smithy pool` lets `smithy agent` use several of your own Claude Code or Codex accounts, and it moves to the next one when a usage limit is hit and the run can safely be repeated.

### New

- You can change settings in the `smithy tui` System view, and each change shows the old and new value before it is saved.
- Claude Code runs report token use and cost, shown in the TUI; other CLIs show a dash rather than an estimate.
- `auto.max_cost_per_day` stops new launches for the rest of the day once reported spend reaches it.
- Telegram messages are richer, with the item, cost and buttons to open its PR or issue, and `notify.style` picks plain or emoji headers.

### Fixed

- Tests no longer write to your real smithy home or send real Telegram alerts.
- Under the native sandbox, git reaches GitHub over HTTPS, so sandboxed workers can push.
- `smithy agent` maps the cheap, mid, strong and judge tiers to your models for that CLI.
- A warden worker that dies at start no longer hangs `smithy wait batch:`.

### Details

- Added: `notify.style` (`plain`, default, or `emoji`) for Telegram messages.
- Fixed: tests no longer write to your real `~/.smithy` (test `stage` events reached Telegram as
  real alerts). Under `go test` the smithy home is a temp dir unless a test sets one, and
  `tests/run.sh` exports a temp `SMITHY_HOME`.
- Changed: Telegram notifications are rich HTML: an emoji header per kind, the item key and title,
  repo, stage, model · speed, known cost and duration, the reason in a quote, the pending question
  and how to answer it for decisions, and Open PR / Open issue buttons (http(s) only); every value
  is escaped and link previews are off. `smithy notify test --all` sends a sample of every kind.

- Fixed: under `auto.sandbox native`, git reaches GitHub over HTTPS with `gh auth git-credential`
  (SSH remotes and `~/.ssh` stay blocked inside the sandbox), so sandboxed workers can push.
- Fixed: `smithy agent` resolves the tiers `cheap`, `mid`, `strong` and `judge` to your models for
  that CLI's harness (`models.<harness>.<tier>`); an unset tier uses the CLI's default with a warning.
- Changed: the container sandbox image (`sandbox/Dockerfile`) is on Go 1.27.4, pinned by digest.
- Fixed: a warden worker that died at start (for example on an invalid `--model`) no longer hangs
  `smithy wait batch:<batch>`. `smithy agent --bg` with a `<batch-dir>/<id>.log` writes the card
  at launch (`working`, `last_beat` now, the worker's `pid`), and the batch wait turns a card whose
  pid is gone before it reported into `failed` (`note=worker exited: <last log line>`) and wakes.

- Added: `auto.sandbox` (off | native | container) isolates unattended workers on top of the
  `--profile auto` allow-list and fails closed: a worker whose sandbox can't start is not started,
  and serve launches nothing (log, `orchestrator_failed` to the TUI and Telegram). native runs
  Claude Code with `--settings` for its sandbox: strict host allow-list (GitHub, Anthropic, Linear
  plus `auto.sandbox_domains`), shell writes only to the worktree, its git dir, `~/.smithy/plans`,
  `~/.smithy/warden` and the Go cache, secrets unreadable, no unsandboxed retry; `--add-dir` is
  narrowed to match. Live-tested on macOS. codex runs with network off. container wraps the worker
  in `docker|podman run --rm` with the worktree and plans mounted and a scrubbed env (image:
  `auto.sandbox_image`, `sandbox/Dockerfile`); unit-tested only, live test pending. `smithy doctor`
  checks each mode (sandbox-exec, bwrap + socat, docker/podman) and the TUI System view shows it.
  `auto.sandbox*` are read from the user settings only; an unreadable file fails closed; native
  needs claude 2.1.331 or newer. Containers run with `--network none` by default, a read-only root,
  tmpfs /tmp, pid and memory limits (`auto.sandbox_memory`) and read-only git hooks and config.
  `auto.sandbox_weaker_net` turns off macOS's weaker network isolation (gh then fails inside).

- Added: account pool (`smithy pool add|login|list|remove|clear`), off until an account is added.
  Your own claude (CLAUDE_CONFIG_DIR) and codex (CODEX_HOME) accounts in `~/.smithy/pool.json`;
  `smithy agent` runs on the least recently used one that is not limited (`AGENT_CLI=pool` or
  `auto.agent pool`: any harness), records it on the warden card and the usage event, and on a
  usage-limit result marks it limited until the reset it states (5h otherwise) in
  `~/.smithy/pool-state.json`. It reruns once on the next account (log appended) only when the run
  was read-only or left the worktree untouched (same HEAD, clean, nothing pushed); otherwise it
  exits 75 and blocks the warden card (`account <name> limited until <t>`) so warden relaunches it
  in the same worktree. A pooled run drops `ANTHROPIC_API_KEY`, `CLAUDE_CODE_OAUTH_TOKEN` and
  `OPENAI_API_KEY` so the account's login is used. Serve holds while every account is limited and
  says the earliest reset; Telegram kind `limited`; the TUI System view shows a pool table.
  Unattended workers are denied every account dir and `smithy pool`.
- Added: edit settings from `smithy tui` System view. Every known key plus the harness's models,
  grouped, with value, source and meaning; enter opens a typed input (bool/enum choice, number,
  list, text), a confirm step shows old → new and the scope (tab: user or repo), `u` unsets back to
  the default. Writes go through `config.Apply`/`config.Remove`, the same path as
  `smithy config set|unset`; secret-valued keys are locked (auth stays in `smithy auth`); demo mode
  only says the command it would run.
- Changed: `smithy config set` validates typed keys the way the code reads them: booleans
  (`auto.paused`, `decide.enabled`), whole numbers (`warden.max`, `auto.every` at least 1), non-negative
  numbers (`watch.every`, `auto.session_hours`, `auto.max_cost_per_day`, …) and choices (`auto.mode`, `warden.merge`,
  `warden.mode`, `tasks.location`, …); comma lists are trimmed.

- Added: token use and cost per run. `smithy agent` runs Claude Code with `--output-format json`,
  then turns the log back into the plain answer and writes `<log>.usage.json` (input, output,
  cache read/write tokens, `cost_usd` from `total_cost_usd`) plus a `usage` event. `smithy cards
  board --json` shows each item's `usage` and the batch total. An error result keeps a readable
  `error: <subtype>: <message>` line in the log; runs without `--log` (consult) still count. Other CLIs report nothing yet and
  show `-`: never an estimate.
- Added: the TUI shows a cost column in Agents and Done, and "spend today" with a 7-day sparkline
  in the Overview's Daily cap card.
- Added: `auto.max_cost_per_day` (unset by default): when today's reported spend reaches it,
  `smithy auto serve` launches nothing until tomorrow, logs why and records one `cap_reached`
  event per day.
- Added: Telegram notifications, sent by `smithy auto serve` (never an agent) from events.jsonl
  and the cards: PR ready, item blocked, stuck or waiting on a decision, orchestrator failed,
  serve stopped, daily cap reached. Bot token in `smithy auth set telegram`, chat in
  `notify.telegram.chat`, kinds in `notify.kinds`. Once per (item, kind), at most 20 a minute
  (the overflow folded into one summary), no issue bodies or keys; a failed send is logged once,
  retried each cycle for 24 h (then dropped, logged) and never stops serve; unknown kinds are
  ignored with a warning in `smithy notify status`. `smithy notify test|status`; the TUI System view shows the setup.
- Added: events `orchestrator_failed`, `cap_reached` (once a day) and `serve_down` in events.jsonl.

## v0.15.0 — 2026-11-23

### Highlights

**Unattended work from issues.** `smithy auto` takes GitHub issues labeled `auto`, or Linear issues, and works them within the hours window and daily cap, then reports the result on the issue.

**A live dashboard.** `smithy tui` is a live view with tabs for agents, decisions, finished work and more, and it reads only local files.

**Telegram alerts.** `smithy auto serve` sends Telegram messages when a PR is ready, an item is blocked or needs a decision, or the daily cap is reached.

### New

- `smithy auto serve` runs the unattended loop, with `--once` for a single pass and `--max-hours` to stop after a set time.
- `smithy decisions reject` sends a decision back, so the agent decides again.
- Press `p` in `smithy tui` to pause automatic intake.
- `smithy auth set` stores a service's key in a file only you can read, and an environment variable for the same service still wins.
- `smithy <command> <subcommand> --help` prints only that subcommand's usage.

### Fixed

- A serve run that starts nothing puts its issues back in the queue, at most twice per issue.
- One misconfigured issue source no longer stops the others.
- Unattended workers can no longer use curl or wget, or edit hook and settings files.
- Auto tasks use the issue's own key in their file names and branch names.

### Details

- Added: `smithy worktree add <repo> <branch> <from>` creates a task worktree at its standard
  path; warden uses it, and unattended agents (which can't run `git worktree add`) can too.
- Added: `smithy cleanup … --drop-holdout` deletes untracked `zz_*` holdout tests before the clean
  check, so a batch check's worktree can be removed.
- Fixed: serve no longer requeues a dir whose warden batch already started (a card past `queued`).
- Fixed: when a serve orchestrator ends with every task file of its dir still `todo`, serve puts
  those issues back in the queue (`requeued N issue(s): orchestrator started nothing (log …)`), at
  most twice per issue (`requeued` in state.json); after that it logs `giving up on <id>`.
- Added: the TUI Agents view and Overview "Agents now" show the running serve orchestrator as its
  own row (kind orchestrator: agent, pid, elapsed, task dir, session log; `l` opens its log), read
  from `serve.json` or `orchestrator.json`.
- Added: `smithy auto serve [--agent <cli>] [--once] [--max-hours <n>]`, the unattended loop
  outside the LLM: polls every `auto.every`, and when `auto take` yields a task dir starts one
  headless orchestrator (`smithy agent --bg --profile auto`) that runs `tend auto once <dir>`; one
  at a time (`~/.smithy/auto/orchestrator.json`), logs in `~/.smithy/auto/sessions/`, heartbeat in
  `~/.smithy/auto/serve.json`. In a terminal a compact live view (Esc, q or Ctrl+C asks before
  stopping); otherwise one line per event. Stopping never stops the orchestrator or its workers.
  `smithy auto status`, the TUI header and System view (`l` opens the session log) show serve.
- Added: `smithy agent --profile auto`: Claude Code with permission mode `dontAsk` and an
  allowed-tools list (Read, Edit, Write, Glob, Grep; Bash for git, gh, go/npm test, smithy);
  Codex with `approval_policy=never` in its workspace-write sandbox. Never a full bypass.
- Security: the auto profile reaches every worker (`SMITHY_AGENT_PROFILE`); its allow-list names
  git and gh subcommands (push only to origin) and smithy by absolute path only, and a deny list
  blocks `git -c`/`config`, fetch/push exec and URL options, shell redirects and tee, `gh api|auth|gist|alias|extension`, `smithy auth|config`, curl, wget,
  secret files and hook/rc/settings edits. `auto.agent` takes an absolute path; doctor runs its
  `--version` and states Codex's network limit. Serve's locks are O_EXCL with atomic rewrites,
  pids carry their start time (a reused pid reads as dead), a failed launch puts the issues back
  in the queue, SIGTERM no longer waits for a poll, and `--once` keeps the view for 3 s.
- Added: settings `auto.agent` and `auto.session_hours` (default 3); `smithy doctor` warns when
  `auto.agent` is missing or can't run headless. tend `auto once <dir>`; warden ends an `auto-*`
  dir `blocked` when only the subagent fallback can launch workers.
- Changed: `smithy cards board --changed-since` exits 4 (not 3) when nothing changed;
  `smithy auto` exits 1 (not 2) when a source fails to open for a reason other than access.
- Added: `smithy <cmd> <sub> --help` prints only that subcommand's usage.
- Fixed: every error `smithy auto` and the Linear source print goes through one cleaner
  (`internal/text`, also used by the event log and decisions); `events.jsonl` stays 0600 after
  open and rotation.
- Fixed (`smithy tui`): copied commands shell-quote every value; markdown links show their
  target (no OSC 8 hyperlinks); `--demo` never opens a URL, it says what it would open.
- Changed (skills): the merge and production rule lives once in use-smithy **Merges** (tend never
  merges a warden batch's PR); the decision procedure and the heartbeat have one home each (the worker
  brief keeps its own copy, since it is all a worker reads); warden points to `smithy --help` for the full command list and has workers merge the base
  instead of `gh pr update-branch`.
- Docs: `docs/settings.md` lists every `auto.*` and `linear.*` key and the real model key names;
  README says Linear needs `linear.team` and a key; tend AUTO.md notes `auth.json` is readable by
  same-user processes.
- Added: `smithy tui`, a live dashboard (Bubble Tea v2): a dithered animated anvil header with
  the watcher, daily cap, agents and PRs; full-size views behind a tab bar (Overview charts of
  work per hour, agents by stage, the cap, CI health and how long work takes; Agents with a
  timeline and log preview; Incoming with ignored issues and source status; Needs you, where `a`
  accepts and `x` rejects a decision; Done; Activity; Worktrees; Plans, whose specs and tasks
  open in a markdown reader; System, a read-only health report), details, logs, copy, filter,
  sort and mouse; reads local files only;
  `smithy tui --demo` shows a rich fake world; `--no-anim`, `SMITHY_REDUCE_MOTION` and
  `NO_COLOR` are honored.
- Added: `~/.smithy/events.jsonl`, a bounded event log (rotates at 5 MB, keeps one old file)
  written by `smithy auto` (poll, queued, skipped with the reason, launched, report) and
  `smithy cards set` (stage, CI, merged); never issue bodies or keys. The Linear source records
  its rate-limit headroom in the auto state.
- Added: `smithy decisions reject <n> "<why>"`: the entry stays pending with `rejected: <why>`,
  so the agent sees it and decides again.
- Added: `auto.paused` (`p` in `smithy tui`): `smithy auto take` launches nothing while true.
  `internal/tui` is the one package allowed non-stdlib deps (Charm v2, latest).
- Added: `smithy auto poll|take|report|status` and `smithy wait auto`: GitHub issues labeled
  `auto` (or all, `auto.mode`) are queued once, `model:`/`speed:` labels set the task's `Model:`
  and `Speed:` headers, and `take` writes the ones the daily cap and hours window allow into an
  `auto-<date>-<n>` task dir (`Source:` header, issue text quoted as data). Settings `auto.*` and
  `linear.*`; state in `~/.smithy/auto/state.json`.
- Added: Linear source for `smithy auto` (`auto.sources=linear`): GraphQL over net/http with the
  key from `linear.key_env`, never printed; `linear.team` is a required allow-list (filtered in
  the query and re-checked per issue); `report` comments, moves the workflow state and attaches
  the PR. Missing team or key exits 3. The last poll time is now the fetch start.
- Added: `smithy auth set <service>` (key on stdin only), `list` (masked) and `remove`; keys live
  in `~/.smithy/auth.json` (0600, refused if others can read it); the `<service>.key_env` env var
  wins. Child agents never inherit the Linear key env var.
- Fixed: auto task headers and quoted issue bodies are cleaned of control characters and line
  separators; one misconfigured source no longer stops the others; Linear polls cap pages, read
  only the last 7 days on the first poll, overlap the last poll by 2 minutes, uppercase team keys
  and reject unknown state types.
- Fixed: `smithy auto take` writes its task dir to `~/.smithy/plans/<repo>/` whatever
  `tasks.commit` says, so intake never leaves files in the target repo.
- Fixed: auto task files use the issue's human id: file `NN-tes-1-<slug>.md`, `Key: TES-1`
  (so branches read `feat/TES-1-…`); the stable id moves to `Ref: <source>:<uuid>`.
  `smithy auto report` takes `linear:TES-1`, `linear:<uuid>` or a bare id it launched.
- Fixed: `smithy batch init` on an `auto-*` task dir takes merge from `auto.merge`, each item's
  `model` from its `Model:` header and `speed: route` from the mode, and records `production`
  (the base when it is the default branch or the default can't be found from origin/HEAD, gh or
  ls-remote, else `false`). `""` production blocks a merge like a production base.
  The gh and ls-remote lookups time out after 10 s (then production=base).
  `batch.json` writes are atomic; `batch set` and `batch add` lock it and checks `production` is `false` or an
  existing branch and `base_sha`'s base is in the batch. Card writes and beats take the card lock.
  A bare id in `smithy auto report` that two sources launched exits 2 naming both refs.
- Added: `smithy batch set <batch> key=value…` sets `merge`, `production`, `cleanup`, `mode` and
  `base_sha` with validation; warden's docs use it instead of hand edits.
- Added: worker heartbeat: `smithy cards beat <batch> <id>` (and every `cards set`) stamps
  `last_beat`; the board shows a `beat` column and `wait batch`'s `stale:` line its age. A working
  item is stuck after `warden.stuck_minutes` (default 15, was 45 min of no update) without a beat.
- Changed: tend `AUTO.md` says unattended runs need workers that outlive the session (CLI
  `claude --bg`, Codex, `smithy agent` CLIs or desktop sessions, not the subagent fallback), and
  reports issues by their `Ref:` or `linear:TES-1`.

- Added: tend `auto` mode (`references/AUTO.md`): waits on `smithy wait auto`, takes the queue
  into a task dir, runs warden on it with batch values pre-answered and reports each issue
  started, completed or blocked. use-smithy routes "build issues unattended" to it; README
  "Autonomous mode".

## v0.14.1 — 2026-11-23

### New

- The skills no longer download smithy itself. Install it once with `install.sh`, and the agent offers to run that for you when it is missing or older than the skills.

### Fixed

- `hush` installs again.

### Details

- Fixed: hush failed to install (unquoted `: ` in its YAML `description`).
- Changed: the skills never download the smithy binary. Install it once with the new
  `install.sh` (checksum + build attestation, as before); the launcher only runs an installed
  binary. When it is missing or older than the skills, the agent asks whether to run install.sh
  for you (on your yes) or you will; `smithy doctor` and unknown-command errors name the mismatch.
  `install.sh --update` updates the skills and the binary together.

## v0.14.0 — 2026-11-23

### New

- Decisions taken without asking anyone are labeled as taken by the agent itself.
- `smithy tasks import` splits one task list file into separate task files.
- `smithy batch init --risky` marks risky items, which then need a security check.

### Fixed

- `smithy wait pr:` says when a PR never got a check run, so "pending" and "no CI" read differently.
- `smithy consult` escalates only for the hard floor: production, deleting user data, real credentials and moving or charging real money.
- The launcher no longer installs on a checksum alone; it needs the build attestation unless you allow a checksum-only install.
- The warden doctor no longer suggests an auto-approve hook for worker launches.

### Details

#### Added
- `smithy decisions add --by self` (now the default) labels a decision taken without consult.
- `smithy tasks import <tasks.md>` splits one file of `## NN · Title` sections into task files.
- `smithy batch init --risky <id,id>` marks risky items (security agent required).

#### Fixed
- `smithy wait pr:` names a PR whose head never got a check (`pr <n> no-ci: …`) when the wait ends,
  so "pending" and "CI never ran" read differently; tend ends such a PR with `no-ci <url>`.
- tend: green by fixing code, never by deleting, skipping or loosening a test or CI step; outside
  PRs run with `--ignore-scripts` installs and no tokens, and changes to agent files, `.github/` or
  install scripts go to the user; reruns up to twice before calling a step flaky.
- Settings and help catch-up: `smithy batch init` reads `warden.max`, `warden.cleanup`, `warden.mode`
  and `warden.merge` when its flag isn't passed (new `--cleanup` and `--mode` flags; warden and
  brief fall back to `warden.max`); `smithy decisions list --ref <prefix>` filters entries and its
  help names the log file and the `--by` default; `smithy wait timer`, `smithy verify`,
  `smithy pr create` (exit 3 for a non-GitHub origin) and `smithy receipt run` (exit 127, 128+signal)
  help match the code; docs/settings.md adds the `judge` model row and the `warden.merge` note.
- `smithy tasks import` keeps text before the first section (stack, shared rules) as shared
  context in every task file instead of refusing the list.
- warden harness: a returning subagent worker is the wake; no batch wait needed.
- `smithy verify`: checks that are all skipped or neutral are `none` (no proof), not green: `none`
  is pending when `.github/workflows` holds a workflow, and counts green only when the repo has
  no workflow (no CI configured).
- tend reads a foreign PR's agent instructions from the base, never from the PR head.
- Skills that ship the launcher declare a `compatibility` line (git, gh or GH_TOKEN, attestation);
  `tests/skills.sh` limits the SKILL.md body, not the frontmatter.
- `smithy tasks next|ready` on a folder with no task files says so and points to
  `smithy tasks import` when a `## NN` list is there (`next` printed "all done").
- Skill text: use-smithy routes a single `tasks.md` through `smithy tasks import` to warden and
  splits the ESCALATE step (hard floor → blocked; the rest → safest); adversary, e2e and hostile
  send raw HTTP paths with a raw client; TDD closes keep-alive connections in HTTP tests.
- `smithy consult` answers ESCALATE only for the hard floor (production, deleting user data, real
  credentials or logins as values, not auth code, moving or charging real money); code that
  computes money or security is its pick; an ESCALATE with nobody to ask ends blocked.
  The stop list and hard floor say "moving or charging real money"; hush points to use-smithy.
- `smithy pr create` drops GitHub's "Create a pull request" push hint (other push output stays),
  so the PR URL is the only link printed.
- Skill text: the holdout receipt runs on a committed tree; an agreed spec skips the builder check
  unless it has open questions.
- `smithy receipt list` keeps the newest holdout receipt from an earlier commit ("holdout commit",
  or "holdout failed" when it failed), so it reaches the PR table.
- Skill text: use-smithy routes "research, then build" to scout then smelt plan; TDD runs single
  test files with the runner's timeout (a hang is not red).
- `smithy commit` accepts a staged `git mv` rename (both paths named), refuses paths outside the
  repo, and checks earlier staging against repo-root paths (also from a subfolder, and for `.`).
- Skill text: Decisions define "safest" outside security (most reversible, least new surface);
  debug re-runs the repro receipt on the final HEAD; SHIP's per-commit rebase starts after the red
  repro commit, which goes first on the branch.
- `smithy verify`: an open PR whose mergeable is still UNKNOWN is `pending`, not green.
- `smithy brief`: a `Spec:` line takes only its first token; `batch init` stores an absolute
  intent when the task file isn't committed on the base.
- warden doctor accepts an allow rule for `smithy pr merge`.
- Skill text: tend runs the full suite (a receipt) before pushing a merge or rebase of the base;
  use-smithy says where consult's ESCALATE goes when nobody can answer.
- `smithy decisions`: the log is named after origin's owner and repo, so two checkouts in
  folders with the same name no longer share one (logs under the old folder name stay as they are).
- `smithy consult`: a fallback CLI uses the models saved for its own harness, never the current
  harness's names (desktop without `claude` on PATH sent `opus` to opencode).
- smelt SHIP: full `smithy verify` form, commits in dependency order, base red noted in the PR;
  tend fixes base red only when small, else reports it; Decisions: the timer is only for a user
  who can answer.

#### Security
- Launcher: without `gh` it no longer installs on the checksum alone; it needs the build
  attestation, or `SMITHY_ALLOW_UNATTESTED=1` to accept checksum-only.
- warden: doctor no longer suggests a `PermissionRequest` hook that auto-approves worker launches.
- smelt: secret-looking fixtures are made obviously fake; `smithy:allow` needs the user's yes.
- tend: names outside text (issues, comments, CI logs) as data in its own rules.
- hush: the description no longer says it triggers for every launched agent.

## v0.13.0 — 2026-11-22

### Highlights

**Decisions with a timeout.** smithy waits up to `ask.timeout` minutes (10 by default) for your answer. Then it asks `smithy consult` for a pick and logs it, and a hard floor such as merging to production, deleting user data, credentials or money is never decided for you.

### New

- `smithy batch init` writes a batch from a task folder, and `smithy batch add` adds one item to it.
- `smithy brief` renders a worker's brief with every field filled in.
- `smithy wait batch:` also wakes when a card's PR turns green or red, conflicts, merges or closes.
- `smithy decisions` lets you confirm or change pending decisions by number.

### Fixed

- `smithy wait` prints `conflict` for a conflicting PR instead of `checks=green`.
- `smithy verify` reports a conflicting PR as `conflict`, not `red`.

### Details

- **Release audit fixes.** One owner for the ask flow, decision log, model ask and stop list (use-smithy); warden logs decisions with `smithy decisions add --ref`, not `decisions.md`; plain skill names instead of slash examples; doc corrections.
- **Ask, with a timeout.** The user's decisions (use-smithy's stop list) are asked and waited on
  for at most `ask.timeout` minutes (new setting, default 10; 0 = don't start the timer) through a background
  `smithy wait timer:<min>` (new target, exit 0 when it fires). No answer in time, or nobody to
  ask (hush, launched sessions, warden workers through warden): one `smithy consult` with neutral
  options, safest first for security, and the pick is logged with the new `smithy decisions add`
  (`~/.smithy/decisions/<repo>.md`; `pending` numbers the unconfirmed ones and `confirm <n>|--all` / `change <n>` take that
  number; `list` shows all; locked, atomic writes; fields can't contain `·` or `→`). A hard
  floor is never auto-decided: merging or deploying to the production branch, deleting or irreversibly rewriting user data, credentials, secrets or logins, money. use-smithy owns the rule and shows pending
  decisions at the start of each reply (simple too, as a statement under hush); hush, smelt (Always, EXECUTE rulings) and
  warden (decide stage, batch end, worker brief) point to it.

- **R15 warden risky-task-list frictions.** New `smithy batch init <tasks_dir> --repo <dir>
  [--base b] [--merge auto|manual] [--max n]` writes `batch.json` from a task dir (ids, titles,
  paths as intent, speed, prereqs from unfinished blockers, branch and worktree names, risky and
  route from the task text; named `<repo>-<task dir>`, `-2` when taken; refused while another
  batch has unfinished items from the same task dir or branches), and `smithy batch add` appends
  one item (the batch check's fix; refuses a duplicate id, title or branch; speed from the mode). `smithy brief` copies every line of the task dir's `rulings.md` for any task
  file, committed or not; a task file not on the base is read by absolute path. Risky workers set
  card `security=ran` (or `not-run:<reason>`); `smithy cards gate` fails without `security=ran`
  and the warden never merges such an item (`not-run` goes to the user with its reason). `smithy cards set` names the
  unknown `state=` value. `smithy route` usage says it routes the task when `--agent` is omitted;
  `backfill`, `stored data`, `stored records` and `in place` count as risky, and smelt's Risky list names data migrations and
  in-place rewrites of stored data. smelt's RECEIVING.md: a pre-existing finding outside every
  task's scope is a follow-up (a high security one goes to the user).
- **R14 warden bug-fix frictions.** Intake marks bug reports `route: debug` and risky items
  `risky: true` in `batch.json`; `smithy brief` then tells the worker to take smelt's debug path
  (a failing `smithy receipt run --claim repro` committed before the fix) or run the security
  agent. New `smithy receipt check-red` passes only when the `repro` receipt failed on an earlier
  commit and passes on HEAD; `receipt list` shows that red receipt as `red before fix`. Board items
  render as `Board item <owner/repo>#<n>: read it with gh issue view …` (no "Board item board").
  `smithy route --board <owner/repo#n>` reads the issue so its words count, `--batch` floors the
  batch check at mid (without gh, `--board` warns and routes on the text); its risky words follow smelt's
  Risky list (specific injection kinds, personal data, untrusted input, file or shell access) and
  `auth` no longer matches `author`. Overlap needs the same
  top-level function, not just the same file. Under the subagent fallback, dependents launch only
  after their prereq merges. `smithy wait batch:` ends with `relaunch: smithy wait batch:<b>`
  while a card is open. `smithy cleanup` fast-forwards a clean local base branch.
- **R13 warden + tend frictions.** `wait-turn` takes `--max <min>` (the positional still works) and
  a `--subagent` brief calls `wait-turn … --max 1` per foreground call, so no harness moves it to
  the background. `smithy brief` says the intent is the spec for source `none` (no "Board item
  none") and prints a task-file item's path once. `smithy wait` prints `conflict` instead of
  `checks=green` for a conflicting PR; `smithy verify` has its own `conflict` verdict (exit 4),
  no longer `red`, and it wins over `pending` and `red`. Tend never merges a PR with a warden card
  (batch input, a batch's PR under `watch prs`, or `--card`): it stops at green and updates the
  card only via `smithy cards set`; the warden merges when merge is auto, otherwise the user does. Append-only shared files (CHANGELOG, API lists, index
  files) don't count as overlap; their conflicts go to tend. Workers ask (`needs_decision`) before
  editing CI config or check scripts outside their task's files. smelt plan waits for the builder
  check's report before writing tasks. `smithy task-dir <slug> --default` takes unset defaults
  instead of exiting 3.
- **R12 warden frictions.** `smithy brief` reads a task-file path given as the intent, puts the
  task's `Spec:` file in Spec, includes every line of the task dir's `rulings.md` for a task-file
  item, and exits 2 when the task file or its `Spec:` file doesn't exist. `smithy wait batch:`
  reports PRs already green, red or conflicting at the start for merge-auto cards that aren't
  `ready`, skips the warden's own `merged` cards, and keeps reported PR events in the batch's
  `.wait-seen` so a relaunched wait doesn't repeat them (ready or manual cards: red and conflicts
  only). `wait-turn` exits 5 when a prereq was dropped. A `--subagent` brief runs one `wait-turn`
  per foreground shell call, again on exit 4 or a killed call; session workers parked in
  `wait-turn` close with `done: <id> waiting`. Template paths to smithy are quoted.
  `smithy doctor` says `workers: subagent fallback; they end with this session` when no spawn path
  exists. warden's batch check runs the reviewer on at least mid with the test command, runs a
  `zz_` holdout against the fix branch before merging it, and says tend starts only on red CI,
  conflicts or review comments.
- **warden trial frictions.** `smithy brief <batch> <id> [--to <name>] [--subagent]` renders a
  worker brief from `batch.json` and the template block in `warden/references/worker-brief.md`
  (the single source, read from the installed skill), filling every slot (stack, task file,
  wait-turn, relaunch, rulings, the harness's channel line from `harness.md`, hush and smelt
  paths) and refusing with exit 2 when a field is missing or the worktree isn't on the item's
  branch; warden renders briefs instead of writing them. `smithy wait batch:<batch>` also wakes
  when a card's PR turns green or red, conflicts, merges or closes (PRs polled every 30 s), so
  one waiter covers a batch. `smithy doctor` warns when an installed smithy skill isn't in the
  current harness's skills folder, with the link or `npx skills add` fix.
- **A/B trial frictions (r10 warden, r11 smelt from a vague idea).** `smithy stack` reports
  `pytest -q` for Python only when pytest is a dependency or configured (`pytest.ini`,
  `conftest.py`, `[tool.pytest`), else `python3 -m unittest discover`; `smithy route` with
  neither `--file` nor `--text` prints its usage. warden: `start_session` may need loading
  through the tool search (else the subagent fallback, named in the plan line), an item is
  `merged` only after `smithy verify` says so, a committed task dir must be pushed to the base
  before launch, the batch reviewer gets the `<smithy>` path, and briefs carry a `<rulings>` slot.
  smelt: task files default to `Speed: fast` with one stated rule, "nobody to ask" opens
  brainstorm and grilling, DEBUG needs a red reproduction first, TDD stops on a third identical
  failure or a diff outside the task's files, the reviewer skips behaviour-free files and style
  preferences, unittest repos name adversary and property tests `test_zz_*.py`, and ship's rebase
  guard covers languages without a build file, commits status changes first and stops background
  waits. The v0.12 trial doc gains a Skills column and R10-R11.

- **A/B trial frictions (r9, Haiku at every speed).** `smithy tasks add --speed` works (the flag
  was missing from the CLI's flag table, so every call failed with "unknown option"). `smithy
  stack` reports `node --test <file>` when the test script runs `node --test` after a build
  step. `smithy route --domain` is accepted too, and a test now checks that every flag a command's
  usage documents is in its flag table. smelt's ship step switches the main checkout off the
  branch before `smithy cleanup`.

- **A/B trial frictions (r8) and skill-writing practices.** `smithy stack` reports
  `gofmt -l` as Go's lint. Skills: smelt's "Nobody to ask" and commit rules are one instruction
  per bullet, property tests at `normal` are yours (no agent), intent-to-add entries are reset
  before committing, docs-only commits are exempt from the rebase check, a test of existing
  behaviour is seen failing once, a failed `smithy consult` falls back to your own decision,
  only exported API gets doc comments, a refused to-do tool isn't retried; the longest
  references open with a contents line.

## v0.12.0 — 2026-11-22

### New

- Skills load fewer tokens: a normal build reads about 5,500 tokens of skill text instead of about 9,500.
- `smithy tasks add` defaults new tasks to `Speed: fast`.

### Fixed

- `smithy wait pr:` reports checks as pending when you have pushed a commit that GitHub has not shown yet.
- `smithy wait-turn` uses the default of 5 workers when the setting is missing, as the docs say.

### Details

- **Full audit fixes.** `warden.max` falls back to 5 in `smithy wait-turn`, matching the
  config default and docs. Skills: use-smithy owns the stop list (smelt points to it; hush keeps a copy since it installs alone),
  impact levels, merge and wait rules live in one place each, VERIFY allows working-tree
  receipts before ship, smelt README phases and speeds match SKILL.md, ADR format points to
  ledger, tend's inbox is `smithy task-dir inbox`, scout reports go to `<batch-dir>/<id>/report.md`,
  use-smithy's `references/SCRIPTS.md` is now `SAVE-SCRIPTS.md`; `route.domain` is documented.

- **A/B trial frictions (r7, full speed).** `smithy wait pr:` reports checks as pending while
  this checkout's `origin/<branch>` is strictly ahead of the PR head GitHub shows (a push it
  hasn't seen).
  Skills: the property agent writes a seeded generator loop when there is no PBT library, a red
  CI caused by this change is fixed in place, a table of cases for one rule is one red test, the
  holdout receipt may sit behind later fixes.

- **A/B trial frictions (r6).** `smithy stack` reports `python3 -m unittest` for stdlib-only
  Python repos (test files importing unittest, no packaging file). Skills: a finding is declined as spec-ambiguous
  only by quoting both readings (with the declined test ids), a refused to-do tool falls back to
  the final message.

- **A/B trial frictions (r5).** `smithy stack` reports `node --test <file>` as the single-file
  command when the test script is `node --test`. Skills: create the work branch in phase 0, the
  security agent comes on top of the speed's agents, rulings state their exact boundary, a spec
  at the repo root stays there, the holdout receipt may sit one commit behind, `verify` runs only
  after `smithy wait pr:`. README: the speeds section is gone.

- **A/B trial frictions (r4).** `smithy tasks add` defaults to `Speed: fast`. Skills: re-review
  covers every spec-check row that was missing, partial or wrong plus Standards blocking, reads
  the exact commit sha, and closes a departure recorded in `rulings.md` as "deferred to user";
  a user-answer `partial` with nobody to ask becomes a ruling; read-only agents ignore `zz_*`
  files, which move to the system temp dir for typecheck and lint; adversary expected values
  are re-derived; declined holdout tests may be dropped; another ref is reviewed in a detached
  worktree; rebase-proof uses the no-tests flag and runs only for more than one source commit;
  `status=done pr=` lands in a follow-up commit; check `smithy commit`'s exit code; single-file
  test runs skip pretest hooks. `docs/trials/2026-11-v012-rounds.md` summarises the v0.12 rounds.
- **A/B trial frictions (r3).** `smithy verify` counts `checks=none` as pending when the repo
  has a workflow file; `smithy route --domain` (config `route.domain`) takes the product's nouns,
  which never count as risk; `smithy task-dir`'s ask line prints the default it would take;
  `smithy tasks add` writes the full template and takes `--speed`. Skills: `smithy:allow` markers
  pass the comment policy; read-only agents never switch branches (`git show <ref>:<file>`);
  re-reviews answer only the blocking findings; the batch check diff starts before a foundation
  task and warden writes the fix PR's dispositions; conflicting green PRs merge one at a time
  with `gh pr update-branch`; workers get a "Read <brief> and follow it" prompt; any existing
  batch dir gets a suffix; an agreed spec's folder is the task dir; split self-review catches
  stub-output tests and a shared test file; `rebase -x` exempts skeleton, docs and tooling commits.
- **A/B trial frictions (r2).** smelt: fast lists a security agent when risky and puts its
  Spec-check table in the PR body; e2e means UI or served HTTP endpoints; an agreed spec still
  gets the builder check; SHIP proves each commit with `git rebase -x`; CHECK names each agent's
  `smithy route` tier, full's seven strong contexts, own-`zz`-file runs, mutation as a second step and holdout moves; hostile scratch
  goes in the system temp dir; adversary and property write no explanatory comments; TDD stubs
  the export for red; a 3-link chain is fine after a skeleton PR. use-smithy shows
  `smithy names branch <key|-> "<title>" [type]`. `smithy names branch`
  rejects a type word as key; `smithy task-dir` excludes `.smithy/config.json` before it asks.
- **Skill diet.** Skills load fewer tokens and work in any harness. The top-session read set for
  a `normal` smelt build drops from ~9.5k to ~5.5k tokens. Shared rules live once, in a
  use-smithy table; descriptions are trigger-only; phases end in "Done when". Merged references:
  smelt `TESTING.md` + `REVIEW.md` → `CHECK.md`, smelt agents `spec-check.md` + `standards.md`
  → `reviewer.md`, scout `FORMAT.md` → `INVESTIGATOR.md`, tend alerts, issues and base recipes →
  `ACTIONS.md` (receiving points at smelt's), use-smithy `AGENTS-BLOCK.md` → `SETUP.md`
  (`smithy setup block add` reads the block between its markers), ledger ADR and glossary
  formats → `FORMATS.md`. Vendor tool names live only in warden's `references/harness.md`.
  `tests/skills.sh` fails on a SKILL.md over 80 lines (smelt 100), a reference over 120, or a
  vendor tool name outside harness.md.
- **A/B trial frictions (r1).** smelt: one "nobody to ask" rule (hush or a launched session takes
  its recommendation for list approval, how to run it and lessons, and records it); the exact
  `smithy commit "<msg>" <path>...` shape; rerun `smithy stack` once a new repo has a skeleton; a
  skeleton/CI task may ship as its own PR first; holdout runs as a single-file receipt, and a
  spec-ambiguous decline closes it in one line. The reviewer treats the agreed spec as the
  contract (task-file drift is advisory), defines `partial`, and skips split advice on `build
  all`. Property tests may follow unit-tested code. `smithy comments` in CI only where smithy is
  installed.
- **smithy.** `route --agent` runs the adversary and security on at least mid; `task-dir` adds an
  untracked `.smithy/config.json` to `.git/info/exclude`; `stack` prints coverage only when a
  vitest coverage provider is installed; `receipt list` shows a one-string command bare; a failed
  `smithy commit` ends with its error line.

## v0.11.0 — 2026-11-21

### Highlights

**Small changes skip the process.** A typo, copy edit, config value or one-line fix goes straight to the change and its check, with no skill or agents.

**One review per batch.** Warden's workers build without review agents, and once the items merge, one review wave runs on the combined change.

**Tend watches the whole repo.** Besides PRs, tend reacts to red workflow runs, a moved base branch and new Dependabot alerts.

### New

- `smithy names branch` drops filler words and keeps at most four words.
- `smithy cleanup` also deletes the merged remote branch.
- `smelt plan` skips the brainstorm, grill and builder check for a spec you already agreed.
- `smithy decide` is an optional decision model, off until you set it up.

### Fixed

- `smithy comments` no longer reads a JavaScript regex as a comment.
- `smithy wait batch:` wakes once per card change, instead of repeatedly.
- `smithy route` ignores negated words, so "no secrets needed" is not read as a risk.
- `smithy agent --bg` reports an agent that dies at start instead of printing a process id.

### Details

- **Trial 7 frictions.** `smithy comments` no longer reads a JS regex literal (`/^b\//`) as a
  comment. The combined reviewer answers under `## Spec-check` and `## Standards`; the re-review
  prompt asks only about the listed findings and the fix diff. Agents use only file, shell and
  git tools. `build all` ships as one PR.
- **Trial 6 frictions.** Re-review is at most two rounds; a resumed reviewer that repeats its old
  report is replaced by a fresh one with the fix diff; a finding still open goes to the user, or
  with nobody to ask into "Decided without you" (high security, money or data findings still
  stop with a `blocked:` line). Risky work adds the security agent without
  raising the speed (`full` is for money, auth, data loss, public APIs or on request).
  `smithy comments` ignores `smithy:allow` markers. Every `build all` commit builds. A task's
  `Spec:` may point at an agreed spec where it already lives.
- **Trial 5 frictions.** `smithy tasks set` keeps the `PR:` key; `smithy receipt run` runs one
  quoted command line through the shell (zsh doesn't split `"$cmd"`); Markdown edits (a task file
  marked done) no longer make receipts dirty or stale; `smithy pr template` prints `none:` instead
  of failing; the batch-check fix branch is `fix/…`; workers install dependencies first; batch
  dirs are never reused; domain nouns are not risk when routing; launched sessions answer
  `task-dir` themselves.
- **Small changes skip smithy.** use-smithy sends a typo, copy, config value or one-line fix straight
  to the change and its check, with no skill or agents.
- **Shorter branch names.** `smithy names branch` drops filler words, single letters and words
  already in the key, and keeps at most four.
- **One check wave per batch, not per task (trial 4).** Warden workers build at smelt `fast` with
  no agents; once the items merge, warden runs one wave at the batch's speed on the combined diff
  and turns blocking findings into one `fix` item. `warden.mode` now defaults to `smelt normal`
  and names that wave's speed. `build all` does the same on the branch, and builds three tasks or
  fewer without implementer subagents.
- **Fewer, bigger tasks; build here unless parallel pays.** TASKS.md splits only where it buys
  parallel time, adds a `Speed:` line, and recommends building here unless two independent tasks
  are each over ~15 minutes of agent work.
- **Agent test files use the runner's suffix** (`zz_adversary.test.ts`), so vitest and jest pick
  them up; receipts ignore them too.
- **`wait-turn` runs in the background for session workers** and in the foreground only for
  subagent workers.
- **`route` no longer sends lockfile work to the strong tier.** `lock` matched `lockfile`; it is now
  `lock contention`, `deadlock` and `mutex`.
- **`smithy cleanup` deletes the merged remote branch**, including when the local branch is already gone.
- **`smelt plan` skips brainstorm, grill and the builder check for a spec the user already agreed.**
- **The smelt adversary skips implementation bodies in round one** when its brief includes a diff.
- **Warden's subagent workers run `wait-turn` in the foreground** (9 minutes, the tool limit).
- **Rule tests cover every variant the spec names**, one case each.
- **Three speeds that differ, and agents only where they earn their cost.** Each subagent costs
  ~60k tokens and minutes however short its prompt (trials 1–3), so: `fast` runs scripts only
  (tests, typecheck, lint, scan) and checks the diff against the spec itself, no subagent unless
  the change is risky; `normal` adds one parallel wave, the adversary and one reviewer, with
  property tests written by the worker; `full` adds a second adversary, property, security, the
  mutation tool run as a command with an agent reading the survivors, and hostile last. Test
  agents write `zz_` files in the tree (no WIP commit, no extra worktree); a fixed blocking
  finding is confirmed by resuming the same reviewer; no "another look by
  judgment". Plans aim for a short chain (width over tidiness). Workers end at `pr_open`; warden
  waits on every PR's CI in one `smithy wait` and merges; no tend session per batch or PR (red CI
  goes back to the worker; tend runs when the user starts it). `warden.max` defaults to 5.
- warden merges every green PR itself; workers stop at `ready` and never merge. A permission
  classifier refused every worker's merge in trial 3, because the merge permission came in a
  brief another agent wrote.
- Fixes from trial 2 on claude-mods (`docs/trials/2026-11-trial-2.md`): `smithy wait batch:`
  wakes once per card change of stage, state, CI or PR (tend mirroring no longer loops it);
  `pr_open` needs `pr=`; board notes are cut at 160 characters; `smithy route` skips negated
  words ("no secrets needed"), reads "token" only as a credential, takes small-change words from
  the title and shows the matched word; `stack.<key>` in the repo config sets a command
  `smithy stack` can't detect. Skills: a subagent-worker fallback in warden's harness, a CI item
  runs first, specs out of git go by absolute path, test agents' worktrees are allowed inside
  workers, regression only for reported bugs, and a blocking finding closes on a look at the
  last fix.
- Fixes from the 2026-11 trial (`docs/trials/2026-11-trial.md`): `smithy pr create` stops with
  exit 3 and a plain message when origin isn't GitHub, and `doctor` warns; `smithy agent --bg`
  reports an agent that dies at start (no login, bad flag) instead of printing a pid; repo
  settings saved in a worktree go to the main checkout, and receipts ignore that file and `zz_*`
  agent tests; `smithy cleanup` removes branches whose commits another branch has; `smithy route`
  sends prose-only tasks to cheap; `smithy names` types docs and stops calling "split" a refactor.
- Fewer agents: at normal, spec-check and standards run as one reviewer; one second look per
  round covers every fixed finding; the builder check runs only for specs over 8 criteria or with
  open questions. Worker briefs list the card states.
- tend watches the repo, not just PRs: `smithy wait` gains `actions[:<branch>]` (red workflow
  runs, push, schedule or manual), `base[:<branch>]` (the branch moved) and `alerts` (new
  Dependabot alerts, skipped with a note without access); `watch:` takes a comma list or `all`.
  Each event has a short recipe in `tend/references/`, read only when it fires; recipes report by
  default and open fix PRs only with `--fix`.
- `smithy decide` (off until `decide.url` and `decide.model` are set): an optional decision
  model; the URL picks the wire format (Respan `/scores` behaviors, else System One: Jev, Kev,
  Clef, span-02). `decide rules` gives the standards reviewer leads on likely rule breaks;
  `smithy cards set` raises a new question's impact with it, never lowers it. Three failed calls
  in a row disable it until `decide.enabled` is set true again. Answers go to `~/.smithy/decide.log`.
- `smithy pr-state` prints `action` (cleanup, fix-ci, sync-base, answer-review, wait, done); tend
  follows it.
- smelt VERIFY: an "it runs" check (start it once, hit the changed path) for changes to something
  that starts. Trimmed always-loaded text in smelt, worker briefs and use-smithy (setup moves to
  `references/SETUP.md`).
- Optional `judge` model tier: the smartest model you have, used only by `smithy consult`
  (falls back to `strong`). ADR 0006 records why decision models and a watcher session stay out.
- Fixes from the release/next audit and worker feedback: one definition of a blocking review
  finding (REVIEW.md), holdout tests stay untouched until their final run, `smithy scan` prints
  binaries as warnings and counts a bare `=======` only next to a conflict start, the attempt cap
  is a fixed 3 (no setting), no port rule, `CHANGELOG.md` merges with `merge=union`, worker briefs
  name `state=working` and `smithy scan --base <base>`, prose-only changes skip test agents.
- smelt: the adversary's tests are a holdout: hidden from the implementer, re-run on the final
  commit as the `holdout` receipt; passing visible but failing holdout is blocking.
- smelt: spec-check flags test files the spec doesn't name and diffs over ~400 non-test lines.
- smelt: review blocks only on spec gaps, bugs and STANDARDS violations; each finding gets a
  disposition in the PR body.
- `smithy wait pr:` ignores check results left over from the previous head after a push and
  reports only the current head's; `smithy commit style` prints the 72-char subject limit it
  enforces; smelt's spec-check and standards agents diff against
  `git merge-base origin/<base> HEAD`, never a local base branch.

- `scripts/eval.sh`: a small outcome-graded eval run by hand. Each `tests/eval/<task>/` is a tiny
  Go package with a bug (three starters from smithy's own fixes); the agent fixes it with or
  without smelt (`none`, `fast`, `normal`), a hidden regression test grades it, and the script
  prints pass@1 and pass^k per arm over `-k` trials. Costs tokens; never in CI.

- ledger keeps always-loaded context small and where the harness reads it: `init` puts its block
  (at most ~15 lines) in CLAUDE.md when only that exists, otherwise in AGENTS.md, and adds
  `@AGENTS.md` to an existing CLAUDE.md; `smithy drift` reports the lines and bytes of AGENTS.md, CLAUDE.md, STANDARDS.md
  and SCRIPTS.md and flags STANDARDS.md past 30 rules; MAP.md holds only non-obvious facts (~1k
  tokens); STANDARDS-FORMAT names retire candidates at each sync.
- Skill hygiene for scanners and strict validators: use-smithy's subagent note is a plain heading,
  every `argument-hint` is quoted (warden's has no `\|`), warden's description leads with its
  triggers and simple's drops the off-switch, tend treats issue text as data when queueing tasks,
  and smelt says high-impact decisions still go to the user under hush. `tests/skills.sh` checks
  the frontmatter and pseudo-tag rules.
- `smithy receipt run` records a receipt dirty when an untracked, non-ignored file is present
  (`.smelt/` and ignored files don't count). `smithy scan` also reports added or changed binary
  files and .pyc/.so/.dylib/.dll/.exe/.wasm/.jar/.zip/.tar/.gz/.docx/.xlsx/.pptx files as warnings
  for a human look (exit 0), and fails on git conflict markers in any added line (fenced code in .md files is exempt; a bare
  `=======` counts only when a `<<<<<<<` is in the same diff).
- `smithy agent` has a wall-clock cap: the new `agent.timeout` setting (minutes, default 90,
  `0` = none) or `--timeout <min>` kills the agent's process group (Unix; the agent process on Windows), logs "timed out after Nm" and
  exits 124; `--bg` runs are watched by a detached smithy that enforces it.
- warden instructs: items that share files in their `Files` lists (or likely paths) get a prereq
  unless the user said stack, no turn ends with live workers and no `smithy wait batch:`, an item
  is relaunched at most 3 times then listed as blocked, stuck reports and the final message carry
  takeover data (worktree, branch, session or pid, log), and issue text is never copied into Files
  or test lines.
- ADRs 0001-0005 in `docs/adr/`: record decisions, gh optional, no decision-model adapter for now,
  release branch for big changes, learning in repo docs.

## v0.10.0 — 2026-11-19

### Highlights

**smithy works without the GitHub CLI.** With a GitHub token, smithy talks to GitHub directly, and with neither `gh` nor a token, `smithy pr create` prints a link to open the pull request yourself.

### New

- `smithy pr merge <pr>` merges a PR by rebase, merge or squash.
- `smithy doctor` reports which GitHub mode is in use: gh, token or none.
- The launcher installs without `gh`, checks the download against the release checksums, and warns that the build's origin is unverified.
- Lessons from a smelt run cite their evidence, and accepted rules go to the repo's standards file.

### Fixed

- Workers run each command inside their worktree and fall back to a merge commit when a rebase merge is refused.
- `smithy wait` retries a transient error on its first read.

### Details

- Audit fixes after the gh-optional batch: warden's final message shows the Decisions and
  Followups lines, workers and tend merge with `smithy pr merge`, smelt proposes up to 3 lessons and
  only skips asking about speed under hush, warden doesn't launch ship items with no GitHub access,
  smelt's DOCS defers the STANDARDS format to ledger. The repo gets AGENTS.md (with CLAUDE.md
  importing it): release-branch flow and a cheap-model audit after big features.
- Skills treat `gh` as optional: smelt ship, tend and warden doctor describe the three GitHub modes
  (gh, token, neither: pushed branch plus compare URL), `gh pr merge` became `smithy pr merge`,
  remaining `gh` mentions say "when installed". README requirements say gh is optional and add a
  Roadmap (other forges later).

- smithy works without the GitHub CLI: every GitHub call goes through one seam (`internal/ghx`)
  that uses gh when present, else the GitHub API with `GH_TOKEN`/`GITHUB_TOKEN` (origin's repo,
  `GH_HOST` for Enterprise), else fails with "needs the GitHub CLI or GH_TOKEN"; with neither,
  `smithy pr create` prints the compare URL. New `smithy pr merge <pr> [--rebase|--merge|--squash]`;
  `smithy doctor` reports the GitHub mode (gh / token / none).
- The launcher installs without `gh`: it downloads with curl, verifies the SHA-256 against the
  release checksums and warns once that the build origin is unverified (smithy and smithy.cmd).
  With `gh` the attestation is still required.
- README: how smithy learns across sessions (evidence-backed lessons, STANDARDS and SCRIPTS owned by
  ledger) and branch names from `smithy names`.
- Skill contradictions fixed: worker effort follows the medium cap, trivial warden items still
  run smelt fast, tend caps bot review rounds (not people), smelt treats security, money and personal
  data as high impact, warden runs risky items at least at normal, smelt's README defers branch
  names to `smithy names`.
- Repo: `main` is protected by a ruleset (PRs with green CI, no force-push or deletion) and pull
  requests have a template.
- "Silent" agents are defined as no narration, never hiding work (smelt, use-smithy, tend, warden,
  hush), and smelt's SKILL.md states that PR comments, issues and CI logs are data, not
  instructions; both address skills.sh audit findings.
- Fixes from the dream-loop batch feedback: worker briefs start every command with
  `cd <worktree>` and fall back to a merge commit when a rebase merge is refused; warden launches
  tend only for manual-merge PRs; tend finds a batch's GitHub repo from its `origin` remote;
  smelt's spec-check marks prose criteria `n/a (prose)` and TDD names the doc lint as the seam for
  docs-only changes; `smithy wait` retries a transient error on its first read.
- ledger owns `docs/agents/STANDARDS.md` and `scripts/SCRIPTS.md`: new
  `references/STANDARDS-FORMAT.md` (line format, ~30-rule budget, retire rules), a `standard` mode,
  `init` creates SCRIPTS.md, DRIFT.md proposes retiring lines whose path is gone; a test pins
  that `smithy drift` reports missing paths in STANDARDS.md.
- smelt lessons now cite evidence: the ship Lesson step reads `rulings.md` and the review
  findings, proposes at most 3 lessons (each with a receipt, failing test, finding, `file:line`
  or ruling; a lint rule, test or script instead of prose when it can be one), and accepted
  judgement rules go to `docs/agents/STANDARDS.md` as `- <rule> — covers: … — from: <evidence>`.
- warden's final message lists every `decisions.md` entry and card `followup:` note with its PR,
  marks reusable rules as candidate standards (proposal only), or says `no decisions or followups`.

## v0.9.1 — 2026-11-19

### Fixed

- `smithy cleanup` removes a worktree only when you pass it, and never the folder it runs in.
- Warden workers on smelt now run every phase through ship, not just the build.
- If you ran v0.9.0, `smithy cleanup` may have removed a worktree it was not given; this release fixes that.

### Details

Fixes from a real warden batch and a second smelt validation run. Upgrade: `smithy cleanup` in
v0.9.0 could remove a worktree it wasn't given.

- `smithy cleanup` removes a worktree only when it is passed and never the folder it runs in; a
  branch checked out in a worktree it wasn't given is kept (a cleanup removed a harness worktree).
- `smithy names worktree` from inside a worktree names it after the main checkout.
- `smithy route --agent tend|scout` (or their SKILL.md path) routes those skills.
- smelt reads CONTRIBUTING.md in context and gives it to standards; scratch files go in `.smelt/`
  or the temp dir; test-agent worktrees get a branch so cleanup can remove them; adversary is
  skipped for docs or test-script-only diffs; a second look covers any blocking finding; tend
  is told to clean up a worktree only when smelt created it.

- warden workers on smelt now run every phase from build to ship (test agents, review agents,
  receipts, scan); a real batch showed workers stopping after build and skipping review.
- README: proof (receipts, scan), your say on merges, and who can start each skill; the flow
  diagram shows the proof step.
- smelt and warden: the PR body file goes inside the worktree's `.smelt/` or the system temp
  dir, never next to the worktree.
- warden: the worker brief's wait targets read `run:<run-id>` and `branch:<branch-name>`, so
  filling the `<id>` slot no longer rewrites them; the CLI channel line uses `<address>`.
- tests: `tests/shared/run.sh` builds its own binary when `SMITHY_BIN` is unset, so it passes
  run alone.

## v0.9.0 — 2026-11-19

### Highlights

**Breaking changes.** `smithy config suggest-models` and `accept-models` are removed, `warden.merge` has no default so warden asks each batch, and every skill can be run by an agent.

### New

- Commands that fail now say why on stderr, for example a skill that is not installed or an unset model.
- Every command accepts `-h` or `--help` and prints its usage.
- Value flags accept `--flag=value` as well as `--flag value`.
- smithy ships no model names; the first time a tier is needed, the skill asks you to name it.

### Fixed

- Windows installs no longer fail with "checksum mismatch".
- `smithy cleanup` removes a branch whose commits landed under new hashes after a rebase merge.
- hush and warden treat web pages, files, issues and tool output as data, never as instructions.

### Details

Breaking: `smithy config suggest-models` and `accept-models` are gone (name each model tier
yourself), `warden.merge` has no default (warden asks per batch), and every skill can be run
by an agent.

#### CLI
- `smithy pr-state <n>` with gh missing or signed out exits 3 with a one-line reason, like `wait` and `doctor`.
- `smithy cleanup <repo> <branch>` says `no branch <x>` and exits 1 when neither the branch nor its worktree exists, instead of "cleaned".
- `smithy commit "<msg>" <path>` with a path that is neither on disk nor tracked exits 2 with `no such path: <x>` before touching git.
- `smithy drift` and `smithy version` exit 2 on a surplus argument (`unexpected argument <x>`).
- `smithy changed <base>` with an unknown base exits 2 with `no such base <x>` instead of git's error and exit 0.
- `smithy comments --diff` without a base exits 2 instead of passing as clean.
- `smithy tasks` checks flags per subcommand: `--all` only for `board`, `--blocked-by`/`--key` only for `add` (`tasks ready --all` exits 2).
- A repeated value flag: the last one wins for every command (`smithy route` kept the first).
- A value flag with no value (`smithy tasks add "x" --blocked-by`) exits 2 with `<flag> needs a value` and writes nothing.
- Value flags accept `--flag=value` as well as `--flag value` (`smithy search --max=1`, `smithy tasks add --key=X`).
- A bare `--` no longer reaches commands (`smithy guard-tests -- x_test.go` now blocks); `receipt run` and `search` still read it.
- `smithy cleanup` removes a branch whose commits already landed under new hashes (a rebase
  merge), instead of keeping it as unpushed.
- Every command: `-h`/`--help` anywhere prints its usage on stdout and exits 0 before any repo
  check, gh call or side effect (`smithy pr --help` and `smithy wait --help` included); an unknown option prints `smithy <cmd>: unknown option <x>`
  and the usage on stderr, exit 2 (so `smithy adr --bogus` no longer writes an ADR).
- Usage mistakes exit 2 (`smithy commit "<msg>"` without paths, `smithy pr create` misuse).
- `smithy changed` outside a repo says "not a git repo" (exit 2); `smithy comments <path>`
  with a missing path exits 2; `smithy wait` rejects an unknown target (exit 2) before gh.
- Failures say why on stderr: `smithy skills <name>` not installed, `smithy config model|get`
  unset, `smithy tasks where` with none, `smithy cards get` for a missing card (now exit 1, not
  `null`), `smithy agent` with a missing prompt file.
- `smithy tasks add|list` without a dir use the newest task dir, even an empty one made by
  `smithy task-dir`.

#### hardening
- `smithy comments --diff` reads file names with `-z` (spaces and non-ASCII names count) and exits 2 for a base git can't resolve or one starting with `-`.
- `smithy drift` exits 2 for a base git can't resolve and reads changed and tracked names with `-z`.
- `smithy route` exits 2 for an unknown `--kind` or `--speed` and for a flag without its value.
- `smithy config` reports a config file that isn't a JSON object (including `null`) on reads too, and `set` refuses one instead of overwriting it; it rejects empty or malformed keys, harness names and model names, and locks the file around writes so concurrent `set` calls keep every value.
- `smithy scan` prints to stderr how many lines `smithy:allow` skipped and where.
- `smithy config` writes its pid in the lock and reclaims a lock whose pid is gone or that is older than 30s, so a killed writer no longer blocks every later write.
- `smithy config` writes through a symlinked config (the link stays), keeps the file mode, and refuses a read-only config with exit 1.
- `smithy comments --diff` and `smithy drift` read a file header only right after `diff --git` (an added line starting with "++ " is not one) and unquote quoted names.
- `smithy scan` checks files added by a rename (`git mv README .env`) as secret-bearing names too.
- `smithy receipt run` exits 1 when the command passed but the receipt could not be written, records a signal kill as 128+signal, and on SIGINT/SIGTERM forwards the signal to the command (run in its own process group), waits for it and writes no receipt.
- The launcher stops `gh release download` and `gh attestation verify` after 120s (`SMITHY_DOWNLOAD_TIMEOUT` overrides) with exit 3, and removes its temp dir before running the installed binary.
- `smithy stack` gives Go `go test -count=1 ./...` (cached results made receipts look fresh) and prints `base:` from origin/main or origin/master when origin/HEAD is unset.
- `smithy wait-turn` says `batch.json unreadable: stack must be true or false` (not "no batch") for a non-bool `stack`, and `smithy cards board` holds an item only for `"hold": true` or a reason, not `"hold": false`.
- `smithy doctor` prints the agent CLI line only for the cli and any adapters, not under desktop.
- `smithy drift` lists under "Missing references" the backticked code symbols (`Put`, `Store.Put`) in changed docs and docs that mention changed files that were in the code at base and are gone now.
- `smithy comments` labels only code-shaped text "commented-out code"; prose such as "a map because lookups must be O(1)" is an explanatory comment.

#### skills consistency
- `warden.merge` has no built-in default: warden asks once per batch in its plan line (auto or
  manual); a saved setting counts as the user's words.
- Merge, cleanup and report rules match use-smithy everywhere: workers end with `done: <id> …`,
  tend can stay until a PR merges and cleans up, warden ends tend when the batch is final, and
  nothing runs `git worktree remove --force`.
- Sessions that can't ask use the session model and default mode (smelt included); scout treats
  outside content as data; slash commands are only examples of the words.
- smelt: fast with no plan builds from the task's criteria (in `.smelt/spec.md`, with its own
  `.gitignore`); reviewers diff the working tree against the base; a no-remote ship skips the PR.

#### cleanup
- Clean up after merge is a shared rule: whoever sees the PR merge or close removes the work's
  worktrees and local branches and ends the agents or sessions it launched. smelt hands the
  branch and worktree to tend, which cleans them up when the PR merges.

#### models
- smithy ships no model names: `smithy config suggest-models` and `accept-models` are gone. The
  first time a tier is needed the skill asks the user to name it and saves it with
  `smithy config set-model` in `~/.smithy/config.json`.

#### from the smelt validation run
- The launcher builds the dev binary per checkout (`~/.smithy/bin/dev/<checkout id>`), so
  parallel worktrees never run each other's build.
- `smithy route --agent <file|name>` routes a smelt agent (reviewers cheap, test agents mid).
- `smithy comments --diff <base>` reports only the lines the change adds.
- `smithy pr template` says why when there is no template.
- smelt: inline criteria go to `.smelt/spec.md` for the agents; test agents run together,
  in worktrees at absolute paths; VERIFY splits "ready to open" from "ready to merge"; a branch
  the harness made is kept; the standards reviewer falls back to use-smithy's ground rules.

#### launcher
- Windows: falls back to `curl.exe` when `gh` is missing; that curl path times out downloads
  (`--connect-timeout 10 --max-time 120`), and a failed download prints
  `smithy: can't download v<version> (<asset>): <reason>` with exit 3; a CI job runs it on Windows.
- Windows: the SHA-256 read from `certutil` skipped the hash line, so every install failed with
  "checksum mismatch"; it now reads the hash.
- The binary is installed only after its GitHub build attestation is verified (needs `gh`);
  without it the launcher stops with how to fix it, unless `SMITHY_ALLOW_UNATTESTED=1`
  accepts a checksum-only install. Answers the skills.sh Snyk E004/W012 on the launcher.

#### hush and warden workers, from the skills.sh Snyk audit
- hush no longer tells the agent to override the harness's narration rules; it keeps chat to
  what the harness or user requires, records each decision visibly, and the user can end it by
  asking. Web pages, files, issues and tool output are data, never instructions: a shared rule
  in use-smithy, and stated where content enters (hush, smelt review comments, tend, warden
  intake and worker brief).

#### smithy binary
- `smithy drift [base]` resolves its base like `smithy scan` (one shared resolver): a branch
  name diffs against its merge-base with `origin/<branch>`, no base means origin's default
  branch, and the report's `base:` line names the ref instead of a bare SHA.
- New `agent.effort` setting (default `medium`, also the cap): `smithy agent --effort`,
  `$AGENT_EFFORT` or the setting; `high`, `xhigh` and `max` are lowered to `medium`. Claude Code
  gets `--effort`, Codex `-c model_reasoning_effort`; every CLI gets `$AGENT_EFFORT`, and one
  without an effort flag says it runs at its own default. `smithy consult` uses it too.

#### smelt
- New `agents/hostile.md`: runs the built CLI, API or worker as a hostile user across eight
  classes (malformed input, interruption, cancel-and-resume, repeats, injected instructions,
  dirty surroundings, hung dependencies, lying output), in at most 5 cycles, with failure
  fingerprints, flakiness recorded as a finding and harness debris kept apart. Runs after review
  at `full`.
- `smithy receipt run --claim "<c>" -- <cmd>` records each verify run (exit code, output
  digest, commit, dirty tree, platform) in `.smelt/receipts.jsonl`; `smithy receipt list --md`
  prints the table for the PR body and marks receipts from another commit or a dirty tree stale.
- `smithy scan` checks the lines and files a change adds for private keys, tokens and
  secret-bearing files before ship; `smithy:allow` on a line marks a false positive.
- Review: the reviewer closes a blocking finding, not the author. A fixed blocking or high
  security finding (every finding at `full`) gets one bounded second look by a fresh subagent.
- Ship: scan before anything leaves the machine, receipts in the PR body, and one proposed
  lesson when something surprised you (or "no lesson").

#### from the first smelt run
- `smithy scan --base <branch>` diffs against `origin/<branch>` (merge-base) when it exists, and
  says which: `scan: clean against origin/main (merge-base dbcdb0e)`.
- `smithy scan` now reads untracked files' content, handles non-ASCII names, isn't fooled by
  added lines that start with `++`, refuses a `--base` that looks like an option, and without
  an origin default branch or upstream scans the whole history instead of only edits.
- Receipts: tokens in a recorded command are masked in `list`; a write failure keeps the
  command's exit code; a cut last line no longer swallows the next receipt.
- Receipts: one taken with (or followed by) uncommitted changes shows as `dirty`, not `stale`; commands keep their
  quotes (`sh -c 'bun run test'`); `.smelt/` writes its own `.gitignore`; `list` says how many
  receipts from other commits it hid.
- `smithy config set agent.effort high` saves `medium` and says so; other values than `low` or
  `medium` are refused. New `smithy config unset [--repo] <key>`.
- `smithy stack` gives `bun run test` (the package's script), not `bun test` (Bun's runner).
- `smithy verify` usage says `<repo-dir>` is a local path.
- `smithy verify` says `draft` (exit 3) for a draft PR that is otherwise green, instead of
  `pending`; `checks` reads `green`/`red`/`pending`/`none` in `verify`, `pr-state` and `wait`
  alike (was `pass`/`fail` in the first two).
- `smithy verify` takes a PR URL and exits 2 when it belongs to another repo than the checkout's
  origin; tend and smelt pass URLs to `verify` and `wait`, so a bare number can't hit the wrong PR.
- tend reads the PR state before any worktree, and only uses the main checkout when the PR
  branch is already there and clean.
- smelt: ship hands the PR to a tend subagent when
  tend is installed; review and scan use `origin/<base>`; test-agent worktrees start from your
  commit and their files are folded into the repo's tests, never committed as is; another review
  look only when the fix pays for it; agents that didn't apply are listed in the PR body.

#### skills, from a best-practice review
- Every skill can be run by an agent (no more `disable-model-invocation`); warden is kept out of
  implicit invocation and still asks for a yes because it costs sessions. Docs no longer assume
  slash commands.
- Merges: only the session that talks to the user decides; tend and workers merge on `merge
  auto`; a production merge needs the user's explicit yes, up front or asked at green.
- Agents launched by smelt get a report contract: they work silently, end with `done:` and send
  questions up under `Question:`.
- warden acts on the verdicts `smithy verify` really prints; tend keeps one review-round counter
  and handles `smithy wait` exit codes; smelt's ship phase moved to `references/SHIP.md`.

#### tests
- Go unit tests for cards, config, harness, ghx, tasks, wait and warden (each 96–100%); total
  Go coverage 66%.
- CI runs `go test -race`, writes per-package coverage to the job summary and fails under 60%.
- Fix: `smithy wait prs|issues` no longer misses the first new item when the first snapshot was
  empty.
- `smithy doctor` looks up skills in-process instead of running itself once per skill.

## v0.8.0 — 2026-11-16

### Highlights

**Renamed to smithy.** The skills are now `use-smithy` (was playbook), `smelt` (forge), `warden` (foreman), `ledger` (docs), `hush` (silent) and `simple` (direct); `scout` and `tend` keep their names. Reinstall and use the new names.

**One binary.** The helpers are a single `smithy` binary that the skills download once and check, so at runtime only git and gh are needed.

### New

- `smithy wait` watches PRs, runs, branches, issues and batches without spending tokens.
- Settings, plans and batches live in one smithy folder, and the first run moves the old files there.
- `use-smithy` proposes saving repeated commands into a scripts folder with an index.

### Details

#### smithy (breaking)
- The repo is now `Zovaris/smithy` (GitHub redirects the old name): install with
  `npx skills add Zovaris/smithy -g`.
- The collection is now **smithy** and the generic names are gone: playbook → `use-smithy`,
  forge → `smelt`, foreman → `warden`, docs → `ledger`, silent → `hush`, direct → `simple`;
  `scout` and `tend` stay. Reinstall, and use the new commands (`/smelt`, `/warden`, `/hush`…).
- Settings, plans, batches and tend state live in `~/.smithy/` (`$SMITHY_HOME`) and
  `<repo>/.smithy/config.json`; the first run moves the old `~/.agents/` files
  (`config.sh migrate` by hand). The repo's old `.agents/config.json` is still read until moved.

#### smithy binary
- One Go binary, `smithy`, replaces the shell and Node helpers: `smithy config`, `harness`,
  `names`, `tasks`, `cards` (warden status cards), `wait` and `status`. Same commands, flags,
  output and exit codes as the scripts they replace; the old script tests now run through it.
- Skills ship a launcher (`scripts/smithy`, `smithy.cmd`) that downloads the binary for the
  platform once from the GitHub release (gzipped, ~1.4 MB), verifies its checksum and build
  attestation, and caches it in `~/.smithy/bin/<version>`; in a source checkout it builds from
  source instead. `SMITHY_BIN` overrides.
- Every helper is now a `smithy` command; no skill ships bash, Node or Python any more (only
  the launcher). New since the first cut: `commit`, `pr`, `cleanup`, `verify`, `pr-state`,
  `agent`, `consult`, `route`, `skills`, `setup`, `stack`, `changed`, `guard-tests`,
  `task-dir`, `adr` (one for smelt and ledger), `search`, `comments`, `drift`, `doctor` and
  `wait-turn`; tend's and warden's `watch.sh` are `smithy wait watch[:scope]` and
  `smithy wait batch:<name>`. Runtime needs drop to git and gh (no jq, node or python3).
- Small differences from the scripts: messages say `smithy <cmd>:`; an empty check conclusion
  counts as pending; cleanup handles worktree paths with spaces; `$AGENT_ARGS` is split with
  quotes but no shell expansion; `setup block add` is idempotent; search cuts long lines by
  characters, not bytes.
- CI: `check` on pull requests and main (gofmt, vet, tests, cross builds); `release` on `v*`
  tags (binaries, checksums, provenance attestation, GitHub release from the CHANGELOG).

#### waiting
- `smithy wait`: one zero-token waiter for `pr:` (checks finished green/red with the failed
  names, merged, closed, conflict, review, comment, push), `run:`, `branch:`, `prs`, `issues:`
  and `batch:` targets, any mix in one call. Already-finished checks return at once; a bad
  target fails fast. Settings `watch.events`, `watch.every`, `watch.max`.
- tend's and warden's `watch.sh` are presets over it; smelt, warden workers and tend wait on it
  instead of sleep loops.

#### naming
- `names.sh`: branches `<type>/<key>-<slug>` (key: Jira or Linear key, issue number or task
  `NN`) and worktrees `<repo-parent>/worktrees/<repo>-<branch>`, from `git.branch` and
  `git.worktree`. smelt, warden and tend use it; tend no longer depends on a branch prefix.

#### tasks
- `tasks.js add` creates the next task file; `tasks.js board [--all]` lists every task list and
  warden batch here or on the whole machine; `/warden status all` shows it.

#### use-smithy
- Proposes saving repeated commands and throwaway scripts into `scripts/` with a `SCRIPTS.md`
  index at the end of a task (after scriptify), and checks that index first.

## v0.7.0 — 2026-11-15

### Highlights

**Model choice per task.** smithy picks a cheap, mid or strong model for each task from its risk, difficulty and size, using the models you set for your harness.

### New

- `consult` asks your strong model through the CLI you are already using, instead of a fixed one.
- A new mid tier sits between cheap and strong.
- The worker brief carries the silent rules itself, so they hold over a long session.

### Fixed

- Duplicated rules and contradictions between the skills are resolved, and skill descriptions are shorter.

### Details

#### review against skill-authoring best practices
- Shorter descriptions on every skill (what + when, ~170–290 characters).
- foreman split from 237 to 114 lines: intake, state, acting and models moved to references; one defaults table with precedence; contradictions on production merges, `max`, tend launch and model rules resolved.
- forge references no longer chain into each other; duplicated rules and red flags removed; every phase has a done check; one definition of "agent".
- playbook, docs, scout, tend, silent and direct: overlaps and contradictions resolved (ADR test, inline scout runs, `done:` stays English, docs init creates STANDARDS.md); Claude-only wording removed outside harness notes; golangci-lint example on v2.

#### decisions
- `consult.sh` moved to `shared/` and ships with forge, foreman and tend. It asks the user's strong model through the CLI of the harness you're in (Claude with no tools; Codex, OpenCode, Gemini, Pi, DeepSeek or Cursor read-only through `agent.sh`) instead of preferring Claude whenever it's installed; `--context <file>` adds a short excerpt. Tested live through OpenCode.
- forge (`build all`, `/silent`), tend and playbook use the same rule as foreman: low impact decide, medium `consult.sh`, high the user; every decision is logged.

#### model routing
- `route.js` picks a model tier per task from the task itself (risk, difficulty, size, kind, speed): cheap, mid or strong, mapped to the user's models for the current harness. Nothing is tied to the session's model.
- `config.sh suggest-models` / `accept-models`: where a harness has stable tier aliases (Claude: haiku, sonnet, opus) the agent proposes them in one line and saves them on a yes; elsewhere it asks for each tier. playbook setup shows the suggestions.
- New `mid` tier (`config.sh set-model mid <name>`) and `foreman.worker_model` (`route` by default, `session`, or one model name).
- foreman launches each worker on its routed model and shows tier and model in the plan line; forge's `build all` routes implementers and reviewers the same way.

#### foreman and silent
- The worker brief carries the silent rules itself, at the top and again before the closing line, and foreman spawns workers with the brief's full text instead of a pointer to the file, so the rules don't fade over a long session.
- `silent` names the narration it forbids ("Now let me…", "Next I'll…") and says the rule lasts the whole session.

## v0.6.0 — 2026-11-15

### New

- The agent runner works unattended on Claude Code, Codex, OpenCode, Gemini, Cursor, Pi and DeepSeek Harness.
- The README has a table for each harness: where its skills go, how to start a skill and how workers run.

### Fixed

- A Codex settings variable is no longer mistaken for a harness.

### Details

#### shared
- `agent.sh` runs workers unattended on every supported CLI, checked against each harness's docs and source: OpenCode `--standalone --auto` (tested live; without `--standalone` it doesn't work in the current folder), Codex with the git dir writable and network on, Gemini `--approval-mode yolo`, Claude `--permission-mode auto`, plus Cursor (`agent -p --force`), Pi (`pi -p`) and DeepSeek Harness (`dsh headless`). `AGENT_CLI` picks one, `AGENT_ARGS` adds flags. Workers get `FOREMAN_HARNESS` and lose the parent's harness variables; output goes through a file so a CLI that leaves a server running can't hang the caller.
- `harness.sh` detects Pi and DeepSeek Harness and no longer mistakes user settings (`CODEX_HOME`) for a harness; `find-skill.sh` and playbook's setup know every harness's skills folder and global instructions file.
- Codex: `agents/openai.yaml` keeps forge, foreman, silent and tend user-invoked.
- README: per-harness table (skills folder, how to start a skill, how workers run, background wake).

## v0.5.0 — 2026-11-15

### Highlights

**Five new skills.** `playbook` routes work to the right skill, `docs` keeps decision records and the glossary, `scout` answers questions read-only, `tend` keeps open PRs mergeable, and `direct` gives short replies.

### New

- Forge's `build all` gives each task its own implementer, a review and a capped fix loop.
- `tend watch` adopts new PRs and can queue labeled issues as tasks, without building them.
- Items that cannot start yet wait in their own session until their prerequisites merge.
- `commit` detects the repo's commit style (conventional, gitmoji or plain) and checks messages before committing.

### Fixed

- A PR closed without merging marks its item dropped and ends its worker.

### Details

#### playbook (new)
- Loaded by the agent at the start of software work: lists the installed skills, routes work to them (forge, foreman, docs, scout, tend, silent, direct) and holds the shared rules and red flags.
- `scripts/setup.sh`: status of installed skills and models, and an opt-in pointer block for the user's `AGENTS.md` / `CLAUDE.md`.

#### direct (new)
- Short, direct replies with no filler or AI tells; `silent` wins while it runs; code, commits, PRs and docs are written normally.

#### docs (new)
- ADRs, glossary, module READMEs, codebase map, doc drift checks, comment policy (API doc comments only) and search over docs and decisions.

#### scout (new)
- Read-only investigation with sources on the user's cheap model; reports recommend, never authorize.

#### tend (new)
- Keeps open PRs mergeable: CI fixes, rebases after sibling merges, review comments; launched by foreman for its batches.
- Models: routine work on the user's cheap model, code fixes on the strong one; `--model` / `--fix-model` override.
- `watch [prs|issues|all]`: keeps adopting new PRs (default) and can queue accepted labeled issues as task files for foreman, never building them; `scripts/watch.sh` waits without tokens.
- A PR it can't get green comes back to foreman as a `blocked` / `needs_decision` card, or to the user as `needs you <url>: <why>`.

#### forge
- `build all`: every task here with one implementer subagent each, per-task review, capped fix loop and a rulings ledger.
- Verify gate (no claim without fresh evidence) on build, test, review and ship; red-flag tables.
- Receiving review, a builder check after the spec, a security reviewer, Files and Interfaces in tasks, baseline test run, a three-way close (PR, merge if asked, keep).
- Delegates `adr`, `init`, context and the ship docs step to the docs skill when installed; fact-finding to scout.

#### foreman
- Launches every worker in the user's turn; items that can't start yet wait in their own session on `wait-turn.sh` (new `waiting` state) until their prereqs merge and a slot frees. Batches with dependencies stay autonomous in harnesses that can't start sessions from event turns.
- The watcher wakes only for cards that need the foreman (decisions, stuck, PRs, merged, reported, dropped); `--all` for every change.
- Workers merge `origin/<base>` before opening the PR and before merging; a refused merge ends as `ready note=merge-blocked` and the foreman merges; work found after a merge becomes a follow-up PR (small) or a task file for the foreman to decide.
- `merge auto|manual` and `cleanup auto|ask`, both `auto` by default and stored in `batch.json`; worktrees default to a sibling `<repo-parent>/worktrees/<repo>-<id>`.
- `doctor` suggests allowing `gh pr merge` in Claude Code; `harness.md` documents the event-turn rule and finding a card's session id.
- Scout items follow the scout skill; a tend session keeps the batch's PRs green once workers end.
- Models come from the user's choice per harness (`config.sh`), never hard-coded; `consult.sh` uses the strong one.
- `doctor` checks the companion skills and model settings.

#### shared
- Settings: `config.sh` reads `<repo>/.agents/config.json` then `~/.agents/config.json` then defaults; `config.sh list` shows every setting, its value and source; `set --repo` saves per repo. Known settings: models, `tasks.location/commit/ignore`, `foreman.merge/cleanup/max/mode`, `tend.watch/label`.
- `task-dir.sh <slug>`: one place that decides where specs and tasks go; asks `tasks.commit` once per repo and, when specs stay out of git, whether to add `.docs/` to `.gitignore` or `.git/info/exclude`.
- `tasks.js where` also finds `.docs/specs/` (specs kept out of git) and `$FORGE_TASKS_DIRS`; forge reads `tasks.location` / `tasks.commit` (default committed) from the user config.
- `config.sh` (cheap and strong model per harness in `~/.agents/config.json`), `find-skill.sh --list`; `harness.sh`, `verify.sh` moved to `shared/`.
- `tests/run.sh` (offline tests, skill lint) in CI, and `scripts/drill.sh` for live pressure tests against an agent CLI.

#### forge and foreman
- `commit.sh`: detects the repo's commit style (commitlint, history: conventional, gitmoji or plain), checks messages and commits exactly the given paths, one concern per commit.
- `pr.sh`: finds the PR template and refuses a body that drops its headings; reuses an open PR instead of opening a second one.
- `cleanup.sh`: removes a finished task's worktree and local branch every time, and refuses to drop unpushed work.
- Shared scripts live in `shared/` and are copied into each skill by `scripts/sync-shared.sh`; CI checks they stay in sync.

#### foreman
- Workers turn on their harness's auto-archive when their PR closes, so sessions waiting on a manual merge end on their own.
- A PR closed without merge marks the item `dropped` and ends its worker.
- The foreman ends every finished worker before its final message; `blocked` / `failed` ones stay open for inspection.
- New `/foreman cleanup [batch]`: re-verifies waiting PRs after the batch is over and ends their workers.

## v0.4.0 — 2026-11-15

### New

- `silent` runs a task end to end with no chatter between tool calls, and finishes with one `done:` line.
- `forge` runs its phases with exit gates, from brainstorm to retro, at three speeds: fast, normal and full.
- `foreman` runs one or many tasks in parallel, each in its own git worktree, ending in a PR or a report.
- The skills are not tied to one agent: they run in Claude Code and in any agent CLI.

### Details

First tagged release.

#### silent
- Runs a task end to end with no talk between tool calls; progress only in the harness's to-do list.
- Ends with one line in English: `done: <where it landed>` (PR, commits, uncommitted changes or an answer); only commits or opens a PR when the task asks.
- Stops only for what is the user's alone, with `blocked: <why>`.

#### forge
- Phases with exit gates: brainstorm (why, approaches, design) → grill → spec → task list → TDD → test agents → review → ship → retro.
- Start at any phase (`/forge build`, `/forge test`, `/forge from test`) at three speeds: `fast`, `normal`, `full`.
- Shared task list (`NN-<slug>.md` with `Status`, `Blocked by`, `PR`) on a board, in the repo or in `~/.agents/plans`, managed by `scripts/tasks.js`; `/forge build next` works it one task at a time.
- Independent test agents: adversary, regression, property, mutation, E2E; read-only spec-check and standards reviewers.
- ADRs, glossary and standards docs; optional `guard-tests.sh` pre-edit hook.

#### foreman
- Runs one or many tasks in parallel: one silent worker and git worktree per task, each to a PR or a report.
- Takes inline tasks, a task dir, board ids or just an idea (offers to plan it with forge or split it).
- Workers follow forge (`fast` / `normal` / `full`) or plain mode, reading each skill's `SKILL.md` by path.
- Status cards, a zero-token watcher, PR verification, decisions escalated by impact, recovery ladder.
- Harness-agnostic: adapters for Claude Code desktop and CLI, and `agent.sh` for any agent CLI; `doctor` checks only what the detected harness needs.
