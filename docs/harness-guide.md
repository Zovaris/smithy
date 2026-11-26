# Chat and TUI

`smithy` opens a chat with any agent CLI. A local daemon runs the sessions; the TUI and the web
are two views of it. For the agent CLIs it can drive, see [Harnesses](harnesses.md).

![How the smithy harness works](harness.svg)

```bash
smithy                 # the chat (starts a background daemon if none runs)
smithy web             # the same daemon in a browser
smithy welcome         # the first-run walk
smithy stop            # stop the background daemon
```

The daemon outlives the chat. It exits after `daemon.idle_exit` (default 30m) with no clients
and no running work, or on `smithy stop`. `smithy chat --standalone` runs a private daemon that
stops with the chat.

## Keys

| Key | What it does |
|---|---|
| `enter` | send |
| `shift+enter`, `alt+enter`, `ctrl+j` | new line |
| `/` | every command |
| `@` | attach a file from this folder |
| `tab` | agents and sessions (`a`: every project) |
| `ctrl+k` | palette |
| `ctrl+l` | agent and model |
| `v` (empty input) | transcript mode: minimal, expandable, verbose |
| `pgup` `pgdn`, `ctrl+u` `ctrl+d`, wheel | scroll; `home` and `end` jump |
| click | open a tool block or answer a decision |
| a letter (empty input) | answer the pinned decision with that option |
| `!cmd` | run a shell command in the session folder |
| `ctrl+v` | attach a clipboard image |
| `esc` | clear the input or close a modal |
| `?` | every key and command |
| `ctrl+c` | quit |

## Commands

| Command | What it does |
|---|---|
| `/new` | start a new session; your next message opens it |
| `/sessions` | agents and sessions: open, stop, see what waits on you |
| `/agent [name]` | the agent new sessions start with |
| `/model [name]` | the model of the open session, else new sessions |
| `/effort [low\|medium]` | the open session's reasoning effort |
| `/skills` | skills and MCP injected into every session |
| `/name` or `$name` | run an installed skill on the rest of the line |
| `/theme [steel\|frost\|plum]` | color theme, shared with the web (`ui.theme`) |
| `/mode [minimal\|expandable\|verbose]` | how much of each turn the transcript shows |
| `/stop` | stop the open session's turn |
| `/ask [question]` | pin a sample question to try how decisions look |
| `/answer <text>` | answer the pinned decision in your words |
| `/connect` | which daemon this chat talks to |
| `/web` | open the web UI |
| `/remote` | reach this daemon from your phone or another machine |
| `/log` | daemon events kept out of the transcript |
| `/set <key> <value>`, `/unset <key>` | change a setting (same as `smithy config set`) |
| `/keys`, `/key rm <service>` | list or remove stored API keys |
| `/serve [on\|off]` | serve mode; off launches nothing new |
| `/doctor` | run `smithy doctor` on the daemon's machine |
| `/welcome` | walk the first-run steps again |
| `/setup` | every agent, ready or missing, with its fix |
| `/help` | keys and commands |
| `/detach` | quit and leave the daemon running |
| `/quit` | leave; a background daemon keeps running |

## Projects

A session runs in one project folder. `tab` lists the sessions of this folder; `a` shows every
project. A new session asks which project it works in.

## Settings

| Setting | Default | Effect |
|---|---|---|
| `ui.theme` | `steel` | `steel`, `frost` or `plum`, for TUI and web |
| `ui.splash` | `true` | `false` skips the opening animation |
| `daemon.idle_exit` | `30m` | how long an idle daemon waits before it exits |

Set them with `/set` or `smithy config set`. Every setting: [settings.md](settings.md).

## Web

`smithy web` prints the link to the web UI the daemon serves and `smithy web open` opens it; the
token rides in the URL fragment. Behind `tailscale serve`, open `https://<machine>.<tailnet>.ts.net/`.
The web does what the TUI does:

- **New session**: pick a project (added with a folder browser on the daemon's machine), the
  agent, its model and effort.
- **Session**: the chat, with `!cmd`, pasted or dropped images, `/ask` and every TUI command
  under `/`; `ctrl+k` opens the palette.
- **Decisions** (`2`), **Agents** (`3`: sessions, ready and missing agents, cost, tokens, tok/s
  and runs per hour as text charts), **Board** (`4`: local task files by status; moving a card
  updates the file), **Bench** (`5`: runs, comparisons, start a run) and **Settings** (`6`).
- A side panel on every page shows running agents, what waits on you and the cost.

Settings in the web match the TUI's: every setting with a typed control, API keys, serve on/off,
doctor, the theme (`ui.theme`) and saved servers (add one with a pairing code, or switch).

Install it as an app: in Chrome use Install, on iOS Safari use Add to Home Screen. The page shell
opens offline; the data needs the daemon.

## Other machines

`smithy chat <url>` or `--server <name>` connects to another daemon. `/remote` shows how to reach
this one. Saved servers: `smithy servers`.
