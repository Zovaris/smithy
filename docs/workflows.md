# Workflows and schedules

Two things, kept apart. A **workflow** is a flow of steps you run: one command or one click.
A **schedule** starts a workflow on its own: a cron line, a webhook, a GitHub event or a file
change. The schedule names the workflow; the workflow doesn't know it is scheduled. Both are JSON
files: workflows in `~/.smithy/workflows/`, schedules in `~/.smithy/workflows/schedules/` (no
YAML). Each run is kept with every step's input, output, duration and status.

```bash
smithy workflow example                  # dependabot-daily, repo-health, webhook-reply + their schedules (off)
smithy workflow run webhook-reply --input message="lunch at 1?"
smithy workflow history repo-health
smithy schedule add nightly --workflow repo-health --cron "0 9 * * 1-5"
smithy schedule enable nightly           # pins the schedule and its workflow as they are now
smithy schedule list
smithy schedule hook webhook-reply       # a webhook schedule's URL and token, once enabled
```

In the TUI, `/workflows` lists workflows (r runs one, with its inputs prefilled in the composer
when it has any; enter shows the live run, the steps and past runs; e opens the file) and tab
switches to `/schedules` (left/right turns one on or off, enter shows its runs). `/run <workflow>
[input=value]…` runs one. On the web, the Workflows page has a Run button (with a form for the
inputs), the live run, history and an editor that validates as you type; the Schedules page turns
schedules on or off and shows the runs each started.

## Schedules

```json
{"name": "nightly", "workflow": "repo-health", "cron": "0 9 * * 1-5", "inputs": {"since": "1d"}}
```

Exactly one trigger:

- `cron`: five fields (minute, hour, day, month, weekday; `*`, lists, ranges, `/steps`, `jan`,
  `sun`) or `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`, in local time. A run missed by
  under an hour (daemon asleep) still starts; older misses are skipped.
- `webhook: true`: `POST /v1/hooks/schedules/<name>` on the daemon, from this machine only, with
  header `X-Smithy-Hook: <token>` (`smithy schedule hook <name>`). The JSON body's fields that the
  workflow declares under `inputs` become its inputs; the response holds the run's `status`,
  `outputs` and `error` (202 when it runs past two minutes).
- `github`: `{"event": "dependabot_alert"}`, `{"event": "pr_red"}` (a PR's checks failed) or
  `{"event": "issue_labeled", "label": "bug"}`, read with `gh` in `dir` (the workflow's when
  empty) every 5 minutes. The first look only notes what is there; each new item after that
  starts a run with input `event`, a JSON array of `{id, title, url}`.
- `watch`: a file or folder (no `.git` or `node_modules`, at most 5000 files), looked at every
  minute. The first look only notes it; a change starts a run with input `changed`, the paths one
  per line.

`inputs` are passed to the workflow; the trigger's own inputs win.

## Workflows

```json
{
  "name": "webhook-reply",
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

- Nothing starts on its own until you enable a schedule. Enabling pins the schedule file, its
  workflow and every workflow that one calls: a change to any of them (a script, a pulled repo
  file, a save from the web editor) turns the schedule off until you enable it again. `smithy
  workflow add <file>` copies a repo's workflow or schedule in; a schedule lands off. Running a
  workflow by hand needs no enable: you asked for that one run. Calls nest at most 3 deep.
- Files from before schedules (a workflow with `schedule` or `webhook`) are moved on first use:
  the workflow loses those fields and a schedule of the same name gets them, enabled if the
  workflow was, keeping its webhook token.
- Shell steps go through the guard (`guard.block`, `guard.allow`). Nobody is there to answer, so a
  blocked command fails the step.
- Agent steps run read-only through `smithy agent` with the auto profile: the unattended allow-list
  and `auto.sandbox`, failing closed when the sandbox is unavailable.
- Caps per run, calls and loops included: `max_minutes` (30), `max_agents` (3), `max_cost_usd`
  (1.00, from the agent's reported cost). A cap ends the run as `capped`; `on_error` doesn't catch it.
- Shell steps start from a small environment (PATH, HOME, USER, SHELL, LANG, TMPDIR, TERM,
  XDG_CONFIG_HOME, SSH_AUTH_SOCK, GH_HOST, GH_TOKEN, GITHUB_TOKEN) plus the `SMITHY_*` values; the
  daemon's other variables, API keys among them, never reach them.
- Values read through `{{env.X}}` and the GitHub tokens are replaced by `***` in the history, the
  live view, outputs and notifications. Only exact matches of 4 characters or more are caught: a
  secret a step transforms (base64, say) is not.
- Webhook inputs are untrusted text from whoever holds the token. They reach commands only as
  `$SMITHY_INPUT_*` and agents only inside `<data>` tags, and agents are read-only.
- Output is capped at 64 KiB per step while it runs.
- An enabled workflow keeps the background daemon from its idle exit, so its schedule fires.
- Running, enabling and editing over the API need this machine's daemon token; paired devices
  can read the lists and history only. A webhook takes only its own token, from this machine.
