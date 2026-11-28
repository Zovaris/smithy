# Workflows

The daemon runs workflows: triggers that start them and steps that compose. Each one is a JSON
file in `~/.smithy/workflows/` (no YAML), and each run is kept with every step's input, output,
duration and status.

```bash
smithy workflow example                  # adds dependabot-daily, repo-health, webhook-reply (disabled)
smithy workflow validate repo-health
smithy workflow enable repo-health       # after you set "dir" in the file
smithy workflow run webhook-reply --input message="lunch at 1?"
smithy workflow history repo-health
smithy workflow hook webhook-reply       # its URL and token, once enabled
```

The TUI's `/workflows` and the web's Workflows page show the steps as a tree, run one now, turn it
on or off, show a running workflow live and its past runs step by step, and edit it (the web
editor validates as you type; the TUI opens the file in your editor).

## Triggers

- **Manual**: always: `smithy workflow run`, run now in the TUI and web.
- **Schedule**: `"schedule"`: five cron fields (minute, hour, day, month, weekday; `*`, lists,
  ranges, `/steps`, `jan`, `sun`) or `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`, in
  local time. A run missed by under an hour (daemon asleep) still starts; older misses are skipped.
- **Webhook**: `"webhook": true`. `POST /v1/hooks/workflows/<name>` on the daemon, from this
  machine only, with header `X-Smithy-Hook: <token>` (`smithy workflow hook <name>`). The JSON
  body's fields that the workflow declares under `inputs` become its inputs; the response holds
  the run's `status`, `outputs` and `error` (202 when it runs past two minutes).

## File

```json
{
  "name": "webhook-reply",
  "webhook": true,
  "inputs": {"message": ""},
  "vars": {"tone": "brief"},
  "outputs": {"reply": "{{steps.reply.output}}"},
  "max_minutes": 5, "max_agents": 1, "max_cost_usd": 0.1,
  "steps": [
    {"id": "given", "kind": "if", "if": {"value": "{{inputs.message}}", "match": "\\S"}, "else": "stop"},
    {"id": "reply", "kind": "agent", "timeout": "3m", "retry": {"count": 1, "backoff": "10s"},
     "prompt": "Reply in a {{vars.tone}} way to: {{inputs.message}}"}
  ]
}
```

`dir` is where steps run (home when empty). `inputs` are names with defaults, `vars` the starting
variables, `outputs` templates filled when the run ends.

### Every step

- `id` (a-z, 0-9, `_`, `-`) and `kind`.
- `when`: a condition; when it doesn't hold the step is `skipped`.
- `timeout`: a duration like `30s` or `5m` (default 10m).
- `retry`: `{"count": 2, "backoff": "30s"}`, the wait doubling each try (at most 5 tries more).
- `on_error`: `stop` (default: the run fails), `continue`, or a later step's id to go to.

### Steps

| kind | fields | output |
|---|---|---|
| `run` | `run`: a shell command | its output; exit code kept. A non-zero exit fails the step unless a condition tests that step's `exit` |
| `agent` | `prompt`; `answer`: empty, `yesno`, `enum` (with `options`) or `json` (with an optional `schema` of field types: string, number, bool, array, object); `model`, `effort` | the reply; `answer` and `why` hold the parsed answer. A reply that doesn't fit fails the step |
| `if` | `if`: a condition; `then`, `else`: a later step, `stop`, or empty for the next | `true` or `false` |
| `switch` | `value`: a template; `cases`: `[{"equals"|"match": …, "goto": …}]`; `default` | the branch taken |
| `set` | `vars`: names to templates | the values set |
| `json` | `from`: a template holding JSON; `path`: like `items.0.name` (empty: all) | the value (strings bare, the rest as JSON) |
| `foreach` | `items`: a JSON array, or lines with `"lines": true`; `do`: steps; `parallel` (up to 8), `limit` (up to 200) | a JSON array of each item's last output |
| `ask` | `question`, `options` (empty: any answer) | the person's answer, given in the TUI, web or a chat connector |
| `notify` | `title`, `message` | sent to the TUI chat, the web page and the chat connectors' notifications channel (kind `workflow`) |
| `call` | `workflow`, `with`: its inputs | the called workflow's outputs as JSON |

A condition tests one value: `value` (a template) or, when empty, step `step`'s answer (its output
when it has none). Exactly one test: `exit` (step `step`'s exit code), `match` (regex), `equals`
(case-insensitive), or `path` (a JSON field) with `op` (`eq`, `ne`, `gt`, `ge`, `lt`, `le`,
`contains`, `exists`) and `to`.

### Templates

`{{steps.<id>.output}}`, `.answer`, `.why`, `.exit`, `.status`; `{{vars.x}}`; `{{inputs.x}}`;
`{{env.X}}`; `{{item}}` and `{{index}}` inside a `foreach`. They work in every field but `run`:
commands take none, so no step output ever reaches a shell. A command reads `$SMITHY_VAR_<name>`,
`$SMITHY_INPUT_<name>`, `$SMITHY_ITEM` and `$SMITHY_INDEX` instead. In an agent prompt each value
sits inside `<data>` tags the agent is told to treat as data; still, an agent judging untrusted
text (PR bodies, web pages) can be steered, so keep it read-only and its answer advisory.

## Safety

- Nothing runs on a schedule or webhook until you enable it. Enabling pins the file's hash: a
  change made outside smithy (a script, a pulled repo file) stops it until you enable it again,
  and so does saving an edit from the web page. `smithy workflow add <file>` copies a repo's file
  in, disabled. Run now works on a disabled workflow: you asked for that one run.
- An unattended run (schedule, webhook) calls only enabled workflows; calls nest at most 3 deep.
- Shell steps go through the guard (`guard.block`, `guard.allow`). Nobody is there to answer, so a
  blocked command fails the step.
- Agent steps run read-only through `smithy agent` with the auto profile: the unattended allow-list
  and `auto.sandbox`, failing closed when the sandbox is unavailable.
- Caps per run, calls and loops included: `max_minutes` (30), `max_agents` (3), `max_cost_usd`
  (1.00, from the agent's reported cost). A cap ends the run as `capped`; `on_error` doesn't catch it.
- Values read through `{{env.X}}` are replaced by `***` in the history, the live view, outputs
  and notifications.
- Output is capped at 64 KiB per step while it runs.
- An enabled workflow keeps the background daemon from its idle exit, so its schedule fires.
- Running, enabling and editing over the API need this machine's daemon token; paired devices
  can read the list and history only. The webhook takes only its own token, from this machine.
