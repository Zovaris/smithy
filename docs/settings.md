# Settings

Two JSON files, the repo's first: `<repo>/.smithy/config.json` (this repo only) and
`~/.smithy/config.json` (your defaults). In a linked worktree the repo file is the main
checkout's, so one setting holds for every worktree. You never edit them by hand:

- **use-smithy setup** asks everything up front, or each skill asks a setting the first time it
  needs it and saves your answer;
- `smithy config list` (in any skill's `scripts/`) shows each setting, its value and where it came
  from; `smithy config set [--repo] <key> <value>` changes one and `smithy config unset [--repo] <key>`
  puts it back to the default.

| Setting | Default | What it does |
|---|---|---|
| `models.<harness>.cheap` / `.mid` / `.strong` | ask | Stored per harness in your user config (`smithy config set-model <tier> <name>`); smithy ships no model names, so the first skill that needs a tier asks you to name it. The router (`smithy route`) picks a tier per task: cheap for small, mechanical or read-only work, mid for most features, strong for risky, hard or large work and the last fix round. |
| `models.<harness>.judge` | unset (falls back to the strong model) | Optional fourth tier (`smithy config set-model judge <name>`): the model `smithy consult` uses for a decision's one-call pick; unset, consult uses `strong`. |
| `agent.effort` | `medium` | Reasoning effort for every agent smithy starts (`smithy agent`, `smithy consult`, warden workers): `low` or `medium`. It is also the cap: `high`, `xhigh` or `max` run at `medium`. Passed as `--effort` (Claude Code) or `-c model_reasoning_effort` (Codex) and as `$AGENT_EFFORT`; CLIs without an effort flag keep their own default. Speed (`fast`/`normal`/`full`) adds agents, never effort. |
| `ask.timeout` | `10` | Minutes an agent waits for your answer to one of your decisions (use-smithy's stop list) before one `smithy consult` call picks. `0` = no timer: wait. Never applies to the hard floor. Auto-decisions go to `smithy decisions` (`~/.smithy/decisions/<owner>-<repo>.md`, from origin; the folder name without one); `smithy decisions pending` lists the ones you haven't kept or changed. |
| `agent.timeout` | `90` | Minutes before `smithy agent` (warden workers, `--bg` runs too) kills the agent's whole process group (Unix; on Windows only the agent process), logs "timed out after Nm" and exits 124; `0` means no limit; a value that is not a number warns and uses 90. `--timeout <min>` overrides it for one run. |
| `route.domain` | unset | Comma list of the product's own nouns (`secret` for a secret scanner) that never count as risk for `smithy route`. |
| `warden.worker_model` | `route` | `route`: a tier per task; `session`: the parent's model; or one model name for every worker. |
| `tasks.commit` | ask | Commit specs and tasks (`docs/specs/`) or keep them out of git (`.docs/specs/`). |
| `tasks.ignore` | ask | When not committed: a committed `.gitignore` line, or `.git/info/exclude` (this machine only). |
| `tasks.location` | `repo` | `repo`, `local` (`~/.smithy/plans`) or `board`. |
| `warden.merge` | unset (warden asks once per batch) | For merge mode only; never the yes for the production branch. Unset: warden asks once per batch in its plan line (auto or manual); `auto`: warden merges green PRs (workers never merge), except a production-branch PR, which needs your yes for the batch; `manual`: warden lists each green PR for you. A value you save counts as your words. |
| `warden.cleanup` | `auto` | End finished workers and remove their worktrees and branches without asking. |
| `warden.max` / `warden.mode` | `5` / `smelt normal` | Parallel workers; the speed of the one check wave warden runs on the whole batch (`smelt fast`, `smelt normal`, `smelt full`) or `plain`. Workers always build at `fast`. |
| `tend.watch` / `tend.label` | `prs` / `agent` | What `/tend watch` follows (comma list of `prs`, `issues`, `actions`, `base`, `alerts`, or `all`); the issue label it may queue. |
| `stack.<key>` | unset | A command `smithy stack` can't detect (`stack.test`, `stack.single`, `stack.lint`, `stack.dev`…), set with `--repo`. Read from the default branch's committed config or the main checkout's local file, never a branch's own edit. |
| `decide.url` / `decide.model` | unset (off) | Optional decision model for `smithy decide`, on once both are set. A URL ending in `/scores` uses Respan's behavior format; anything else System One (Jev, Kev local, Clef, span-02). `decide.key_env` names the env var with the key (never the key), `decide.timeout` is in ms (3000). Three failed calls in a row set `decide.enabled` false; set it `true` to turn it back on. |
| `git.branch` | `{type}/{key}-{slug}` | Branch names (`smithy names`); e.g. `agent/{key}-{slug}`. |
| `git.worktree` | `{parent}/worktrees/{repo}-{branch}` | Where worktrees go. |
| `watch.events` / `watch.every` / `watch.max` | `checks,merged,closed,conflict,review,comment,push,new,updated` / `30` / `50` | What wakes a waiting agent on `pr:` and list targets (`actions`, `base` and `alerts` always report), seconds between polls, minutes before a heartbeat. |

Coming from 0.7 (`~/.agents/`): the first run moves your settings, plans and batches to
`~/.smithy/` (`smithy config migrate` does it by hand).

## GitHub access

smithy reaches GitHub (open, merge, verify and watch PRs, read CI runs) in one of three modes;
`smithy doctor` says which one it found:

| Mode | When | What works |
|---|---|---|
| `gh` | the GitHub CLI is installed (and signed in, or no token is set) | everything; the best experience |
| `token` | no gh (or gh signed out) and `GH_TOKEN` or `GITHUB_TOKEN` is set | everything, over the GitHub REST/GraphQL API; owner/repo come from the `origin` remote |
| `none` | neither | git only: `smithy pr create` pushes and prints the compare URL to open the PR by hand (a non-GitHub `origin` exits 3: no PR opened, branch not pushed); `pr merge`, `pr-state`, `verify` and `wait` on PRs exit with "needs the GitHub CLI or GH_TOKEN" |

The token goes only in the `Authorization` header to `api.github.com`, or to `https://<GH_HOST>/api/v3`
when `GH_HOST` names a GitHub Enterprise host; it is never printed, logged or written to receipts.
`smithy pr merge <pr> [--rebase|--merge|--squash]` merges through whichever mode is active, so
skills don't need to call `gh pr merge` themselves.

## Hooks (all optional)

| Hook | Harness | Does |
|---|---|---|
| `skills/smelt/scripts/smithy guard-tests` | any with a pre-edit hook | Blocks test-file edits during TDD's green phase. |
