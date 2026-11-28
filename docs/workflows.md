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

`dir` is where steps run (home when empty). `inputs` are names with defaults (the form Run
shows), `vars` the starting variables, `outputs` templates filled when the run ends.
`allow_write: true` lets steps change things: a writing agent (`"write": true`), `session`,
`smelt`, `warden`, `github` comment/label/issue/pr/merge, `http` other than GET and `file`
write. Without it those steps fail validation. `files` lists the folders `file` steps may use.

`smithy workflow example ship-a-fix` adds the AI-first example: an agent fixes the bug you
describe and commits, the tests run in a `loop` until green (3 tries, each try seeing the last
failure), a `judge` grades the diff against three criteria, an `ask` shows you the grade, and only
on yes a `github pr` step pushes the branch and opens the PR.

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
| `agent` with `items` | a fan-out: the prompt once per item (`{{item}}`), `parallel` and `limit` as in `foreach`; each counts toward `max_agents` | a JSON array of the answers |
| `judge` | `subject` (a template), `criteria` (a list), `pass` (score 0-1, default 0.7) | answer `pass` or `fail`, `why`, and the grade as JSON |
| `loop` | `do`: steps run again until `until` (a condition) holds, at most `max` tries (default 3, at most 10); later tries see the earlier outputs | `done after N of M tries`; it fails when `until` never holds |
| `session` | `prompt`, `agent`: opens a chat session on the daemon | the session's id (the web links to it, where its turns show live) |
| `smelt` | `task`, `speed` (fast, normal, full): a session that runs smelt on the task | the session's id |
| `warden` | `task` (a task dir or an idea): a session that starts a warden batch | the session's id |
| `http` | `method`, `url`, `headers`, `body`; `{{secret.name}}` (a key from `smithy auth set`) works in headers and body only | the response body; `exit` is the status, 400 and up fails |
| `parallel` | `branches`: lists of steps run at the same time; their results and variables join the run after | a JSON array of each branch's last output |
| `wait` | `for` (a duration), or `check` (a command polled every `every`, default 30s, until it exits 0; `timeout` caps it) | the check's last output |
| `github` | `action`: `alerts`, `prs`, `issues` (read), `comment` and `label` (with `pr`), `issue` (`title`, `body`), `pr` (pushes the branch and opens a PR with `title`, `body`), `merge` (with `pr`). Run with `gh`, no shell | gh's output |
| `file` | `op`: `read`, `write` or `append`; `path`; `content`. Only inside the workflow's `files` folders (symlinks resolved) | the text read, or the bytes written |

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
- Steps that change things need `allow_write: true` on the workflow (pinned with it). `github
  pr` and `merge`, and `warden` (it starts paid sessions), also need an earlier `ask` step and
  `when: {step: <ask>, equals: yes}`: validation refuses them otherwise.
- `http` reaches only hosts listed in your `guard.fetch_allow` (`*` doesn't count) and never a
  loopback, private or link-local address, with no redirects, 30 s and 1 MiB at most. A secret goes
  only over https. `{{secret.name}}` values are redacted like `{{env.X}}`, and they can't be put in
  `vars`.
- A `session` or `smelt` prompt built from inputs or step outputs needs an ask gate too, since a
  webhook caller or a fetched page could steer the agent; those values reach it inside `<data>` tags.
- `file` steps go through Go's `os.Root`: no path or symlink inside a files folder reaches outside
  it, and a write never follows a symlink. `github pr` refuses to push main, master or a detached HEAD.
- One yes covers one ask: inside a loop, put the ask inside the loop to approve each try.
- Shell steps go through the guard (`guard.block`, `guard.allow`). Nobody is there to answer, so a
  blocked command fails the step.
- Agent steps run read-only through `smithy agent` with the auto profile (a `"write": true`
  agent drops read-only and keeps the rest): the unattended allow-list
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
