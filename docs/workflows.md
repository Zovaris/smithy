# Scheduled workflows

The daemon runs workflows on a schedule: a cron line plus ordered steps. Each one is a JSON file in
`~/.smithy/workflows/` (no YAML), and each run is kept with every step's output.

```bash
smithy workflow example                 # adds dependabot-daily, disabled
smithy workflow enable dependabot-daily # after you set "dir" in the file
smithy workflow list
smithy workflow run dependabot-daily    # now, on the daemon when one runs
smithy workflow history dependabot-daily
```

The TUI's `/workflows` and the web's Workflows page do the same: run now, turn on or off, history
with each step's output, and edit (the web saves the file; the TUI opens it in your editor).

## File

```json
{
  "name": "dependabot-daily",
  "schedule": "0 17 * * *",
  "dir": "~/code/my-repo",
  "max_minutes": 20, "max_agents": 1, "max_cost_usd": 0.5,
  "steps": [
    {"id": "alerts", "kind": "run", "run": "gh api 'repos/{owner}/{repo}/dependabot/alerts?state=open' --jq '.[].dependency.package.name'"},
    {"id": "any", "kind": "if", "step": "alerts", "match": "\\S", "else": "stop"},
    {"id": "judge", "kind": "agent", "answer": "yesno", "prompt": "Open alerts:\n{{alerts.output}}\nShould I merge the Dependabot PRs now?"},
    {"id": "tell", "kind": "notify", "title": "Dependabot: merge? {{judge.answer}}", "message": "{{judge.why}}"}
  ]
}
```

- `schedule`: five cron fields (minute, hour, day, month, weekday; `*`, lists, ranges, `/steps`,
  `jan`, `sun`) or `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`. Local time. A run
  missed by under an hour (daemon asleep) still starts; older misses are skipped.
- `run`: a shell command in `dir` (home when empty). Its output and exit code are kept; a non-zero
  exit fails the run unless a later `if` tests that step's `exit`.
- `agent`: a read-only agent answers `prompt`. `answer` is empty (free text), `yesno` (first line
  yes or no, the rest is the why) or `json` (one JSON value). A reply that doesn't fit fails the step.
- `if`: tests one earlier step with exactly one of `exit` (code), `match` (regex on its output) or
  `equals` (its answer). `then` and `else` name a later step or `stop`; empty means the next step.
- `notify`: `title` and `message` go to the TUI and web feed and the chat connectors'
  notifications channel (connector kind `workflow`).
- Templates `{{<id>.output}}`, `{{<id>.answer}}`, `{{<id>.why}}`, `{{<id>.exit}}` work in agent
  prompts and notify text only. Run commands take none, so no step output ever reaches a shell. In a
  prompt each value sits inside `<data>` tags the agent is told to treat as data; still, an agent
  judging untrusted output (PR text, web pages) can be steered, so keep it read-only and its
  answer advisory.

## Safety

- Nothing runs until you enable it. Enabling pins the file's hash: a change made outside smithy
  (a script, a pulled repo file) stops the schedule until you enable it again, and so does saving an
  edit from the web page. `smithy workflow add <file>` copies a repo's file in, disabled. Run now
  works on a disabled workflow: you asked for that one run.
- Shell steps go through the guard (`guard.block`, `guard.allow`). Nobody is there to answer, so a
  blocked command fails the step.
- Agent steps run read-only through `smithy agent` with the auto profile: the unattended allow-list
  and `auto.sandbox`, failing closed when the sandbox is unavailable.
- Caps per run: `max_minutes` (30), `max_agents` (3), `max_cost_usd` (1.00, from the agent's
  reported cost); `timeout_minutes` per step (10). A cap ends the run as `capped`.
- An enabled workflow keeps the background daemon from its idle exit, so the schedule fires.
- Running, enabling and editing over the API need this machine's token (paired devices can read
  the list and history only).
