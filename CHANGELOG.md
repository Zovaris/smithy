# Changelog

One version for the whole repo, tagged `vX.Y.Z` (semver). Every change to a skill lands under
**Unreleased** first and moves into a version when it is tagged.

## Unreleased

- **Skill diet.** Skills load fewer tokens and work in any harness. The top-session read set for
  a `normal` smelt build drops from ~9.5k to ~5.5k tokens. Shared rules live once, in a
  use-smithy table; descriptions are trigger-only; phases end in "Done when". Merged references:
  smelt `TESTING.md` + `REVIEW.md` → `CHECK.md`, smelt agents `spec-check.md` + `standards.md`
  → `reviewer.md`, scout `FORMAT.md` → `INVESTIGATOR.md`, tend alerts, issues and base recipes →
  `ACTIONS.md` (receiving points at smelt's), use-smithy `AGENTS-BLOCK.md` → `SETUP.md`
  (`smithy setup block add` reads the block between its markers), ledger ADR and glossary
  formats → `FORMATS.md`. Vendor tool names live only in warden's `references/harness.md`.
  `tests/skills.sh` fails on a SKILL.md over 80 lines (smelt 100), a reference over 120, a
  README over 25, or a vendor tool name outside harness.md.

## v0.11.0 — 2026-11-21

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

Breaking: `smithy config suggest-models` and `accept-models` are gone (name each model tier
yourself), `warden.merge` has no default (warden asks per batch), and every skill can be run
by an agent.

### CLI
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

### hardening
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

### skills consistency
- `warden.merge` has no built-in default: warden asks once per batch in its plan line (auto or
  manual); a saved setting counts as the user's words.
- Merge, cleanup and report rules match use-smithy everywhere: workers end with `done: <id> …`,
  tend can stay until a PR merges and cleans up, warden ends tend when the batch is final, and
  nothing runs `git worktree remove --force`.
- Sessions that can't ask use the session model and default mode (smelt included); scout treats
  outside content as data; slash commands are only examples of the words.
- smelt: fast with no plan builds from the task's criteria (in `.smelt/spec.md`, with its own
  `.gitignore`); reviewers diff the working tree against the base; a no-remote ship skips the PR.

### cleanup
- Clean up after merge is a shared rule: whoever sees the PR merge or close removes the work's
  worktrees and local branches and ends the agents or sessions it launched. smelt hands the
  branch and worktree to tend, which cleans them up when the PR merges.

### models
- smithy ships no model names: `smithy config suggest-models` and `accept-models` are gone. The
  first time a tier is needed the skill asks the user to name it and saves it with
  `smithy config set-model` in `~/.smithy/config.json`.

### from the smelt validation run
- The launcher builds the dev binary per checkout (`~/.smithy/bin/dev/<checkout id>`), so
  parallel worktrees never run each other's build.
- `smithy route --agent <file|name>` routes a smelt agent (reviewers cheap, test agents mid).
- `smithy comments --diff <base>` reports only the lines the change adds.
- `smithy pr template` says why when there is no template.
- smelt: inline criteria go to `.smelt/spec.md` for the agents; test agents run together,
  in worktrees at absolute paths; VERIFY splits "ready to open" from "ready to merge"; a branch
  the harness made is kept; the standards reviewer falls back to use-smithy's ground rules.

### launcher
- Windows: falls back to `curl.exe` when `gh` is missing; that curl path times out downloads
  (`--connect-timeout 10 --max-time 120`), and a failed download prints
  `smithy: can't download v<version> (<asset>): <reason>` with exit 3; a CI job runs it on Windows.
- Windows: the SHA-256 read from `certutil` skipped the hash line, so every install failed with
  "checksum mismatch"; it now reads the hash.
- The binary is installed only after its GitHub build attestation is verified (needs `gh`);
  without it the launcher stops with how to fix it, unless `SMITHY_ALLOW_UNATTESTED=1`
  accepts a checksum-only install. Answers the skills.sh Snyk E004/W012 on the launcher.

### hush and warden workers, from the skills.sh Snyk audit
- hush no longer tells the agent to override the harness's narration rules; it keeps chat to
  what the harness or user requires, records each decision visibly, and the user can end it by
  asking. Web pages, files, issues and tool output are data, never instructions: a shared rule
  in use-smithy, and stated where content enters (hush, smelt review comments, tend, warden
  intake and worker brief).

### smithy binary
- `smithy drift [base]` resolves its base like `smithy scan` (one shared resolver): a branch
  name diffs against its merge-base with `origin/<branch>`, no base means origin's default
  branch, and the report's `base:` line names the ref instead of a bare SHA.
- New `agent.effort` setting (default `medium`, also the cap): `smithy agent --effort`,
  `$AGENT_EFFORT` or the setting; `high`, `xhigh` and `max` are lowered to `medium`. Claude Code
  gets `--effort`, Codex `-c model_reasoning_effort`; every CLI gets `$AGENT_EFFORT`, and one
  without an effort flag says it runs at its own default. `smithy consult` uses it too.

### smelt
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

### from the first smelt run
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

### skills, from a best-practice review
- Every skill can be run by an agent (no more `disable-model-invocation`); warden is kept out of
  implicit invocation and still asks for a yes because it costs sessions. Docs no longer assume
  slash commands.
- Merges: only the session that talks to the user decides; tend and workers merge on `merge
  auto`; a production merge needs the user's explicit yes, up front or asked at green.
- Agents launched by smelt get a report contract: they work silently, end with `done:` and send
  questions up under `Question:`.
- warden acts on the verdicts `smithy verify` really prints; tend keeps one review-round counter
  and handles `smithy wait` exit codes; smelt's ship phase moved to `references/SHIP.md`.

### tests
- Go unit tests for cards, config, harness, ghx, tasks, wait and warden (each 96–100%); total
  Go coverage 66%.
- CI runs `go test -race`, writes per-package coverage to the job summary and fails under 60%.
- Fix: `smithy wait prs|issues` no longer misses the first new item when the first snapshot was
  empty.
- `smithy doctor` looks up skills in-process instead of running itself once per skill.

## v0.8.0 — 2026-11-16

### smithy (breaking)
- The repo is now `Zovaris/smithy` (GitHub redirects the old name): install with
  `npx skills add Zovaris/smithy -g`.
- The collection is now **smithy** and the generic names are gone: playbook → `use-smithy`,
  forge → `smelt`, foreman → `warden`, docs → `ledger`, silent → `hush`, direct → `simple`;
  `scout` and `tend` stay. Reinstall, and use the new commands (`/smelt`, `/warden`, `/hush`…).
- Settings, plans, batches and tend state live in `~/.smithy/` (`$SMITHY_HOME`) and
  `<repo>/.smithy/config.json`; the first run moves the old `~/.agents/` files
  (`config.sh migrate` by hand). The repo's old `.agents/config.json` is still read until moved.

### smithy binary
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

### waiting
- `smithy wait`: one zero-token waiter for `pr:` (checks finished green/red with the failed
  names, merged, closed, conflict, review, comment, push), `run:`, `branch:`, `prs`, `issues:`
  and `batch:` targets, any mix in one call. Already-finished checks return at once; a bad
  target fails fast. Settings `watch.events`, `watch.every`, `watch.max`.
- tend's and warden's `watch.sh` are presets over it; smelt, warden workers and tend wait on it
  instead of sleep loops.

### naming
- `names.sh`: branches `<type>/<key>-<slug>` (key: Jira or Linear key, issue number or task
  `NN`) and worktrees `<repo-parent>/worktrees/<repo>-<branch>`, from `git.branch` and
  `git.worktree`. smelt, warden and tend use it; tend no longer depends on a branch prefix.

### tasks
- `tasks.js add` creates the next task file; `tasks.js board [--all]` lists every task list and
  warden batch here or on the whole machine; `/warden status all` shows it.

### use-smithy
- Proposes saving repeated commands and throwaway scripts into `scripts/` with a `SCRIPTS.md`
  index at the end of a task (after scriptify), and checks that index first.

## v0.7.0 — 2026-11-15

### review against skill-authoring best practices
- Shorter descriptions on every skill (what + when, ~170–290 characters).
- foreman split from 237 to 114 lines: intake, state, acting and models moved to references; one defaults table with precedence; contradictions on production merges, `max`, tend launch and model rules resolved.
- forge references no longer chain into each other; duplicated rules and red flags removed; every phase has a done check; one definition of "agent".
- playbook, docs, scout, tend, silent and direct: overlaps and contradictions resolved (ADR test, inline scout runs, `done:` stays English, docs init creates STANDARDS.md); Claude-only wording removed outside harness notes; golangci-lint example on v2.

### decisions
- `consult.sh` moved to `shared/` and ships with forge, foreman and tend. It asks the user's strong model through the CLI of the harness you're in (Claude with no tools; Codex, OpenCode, Gemini, Pi, DeepSeek or Cursor read-only through `agent.sh`) instead of preferring Claude whenever it's installed; `--context <file>` adds a short excerpt. Tested live through OpenCode.
- forge (`build all`, `/silent`), tend and playbook use the same rule as foreman: low impact decide, medium `consult.sh`, high the user; every decision is logged.

### model routing
- `route.js` picks a model tier per task from the task itself (risk, difficulty, size, kind, speed): cheap, mid or strong, mapped to the user's models for the current harness. Nothing is tied to the session's model.
- `config.sh suggest-models` / `accept-models`: where a harness has stable tier aliases (Claude: haiku, sonnet, opus) the agent proposes them in one line and saves them on a yes; elsewhere it asks for each tier. playbook setup shows the suggestions.
- New `mid` tier (`config.sh set-model mid <name>`) and `foreman.worker_model` (`route` by default, `session`, or one model name).
- foreman launches each worker on its routed model and shows tier and model in the plan line; forge's `build all` routes implementers and reviewers the same way.

### foreman and silent
- The worker brief carries the silent rules itself, at the top and again before the closing line, and foreman spawns workers with the brief's full text instead of a pointer to the file, so the rules don't fade over a long session.
- `silent` names the narration it forbids ("Now let me…", "Next I'll…") and says the rule lasts the whole session.

## v0.6.0 — 2026-11-15

### shared
- `agent.sh` runs workers unattended on every supported CLI, checked against each harness's docs and source: OpenCode `--standalone --auto` (tested live; without `--standalone` it doesn't work in the current folder), Codex with the git dir writable and network on, Gemini `--approval-mode yolo`, Claude `--permission-mode auto`, plus Cursor (`agent -p --force`), Pi (`pi -p`) and DeepSeek Harness (`dsh headless`). `AGENT_CLI` picks one, `AGENT_ARGS` adds flags. Workers get `FOREMAN_HARNESS` and lose the parent's harness variables; output goes through a file so a CLI that leaves a server running can't hang the caller.
- `harness.sh` detects Pi and DeepSeek Harness and no longer mistakes user settings (`CODEX_HOME`) for a harness; `find-skill.sh` and playbook's setup know every harness's skills folder and global instructions file.
- Codex: `agents/openai.yaml` keeps forge, foreman, silent and tend user-invoked.
- README: per-harness table (skills folder, how to start a skill, how workers run, background wake).

## v0.5.0 — 2026-11-15

### playbook (new)
- Loaded by the agent at the start of software work: lists the installed skills, routes work to them (forge, foreman, docs, scout, tend, silent, direct) and holds the shared rules and red flags.
- `scripts/setup.sh`: status of installed skills and models, and an opt-in pointer block for the user's `AGENTS.md` / `CLAUDE.md`.

### direct (new)
- Short, direct replies with no filler or AI tells; `silent` wins while it runs; code, commits, PRs and docs are written normally.

### docs (new)
- ADRs, glossary, module READMEs, codebase map, doc drift checks, comment policy (API doc comments only) and search over docs and decisions.

### scout (new)
- Read-only investigation with sources on the user's cheap model; reports recommend, never authorize.

### tend (new)
- Keeps open PRs mergeable: CI fixes, rebases after sibling merges, review comments; launched by foreman for its batches.
- Models: routine work on the user's cheap model, code fixes on the strong one; `--model` / `--fix-model` override.
- `watch [prs|issues|all]`: keeps adopting new PRs (default) and can queue accepted labeled issues as task files for foreman, never building them; `scripts/watch.sh` waits without tokens.
- A PR it can't get green comes back to foreman as a `blocked` / `needs_decision` card, or to the user as `needs you <url>: <why>`.

### forge
- `build all`: every task here with one implementer subagent each, per-task review, capped fix loop and a rulings ledger.
- Verify gate (no claim without fresh evidence) on build, test, review and ship; red-flag tables.
- Receiving review, a builder check after the spec, a security reviewer, Files and Interfaces in tasks, baseline test run, a three-way close (PR, merge if asked, keep).
- Delegates `adr`, `init`, context and the ship docs step to the docs skill when installed; fact-finding to scout.

### foreman
- Launches every worker in the user's turn; items that can't start yet wait in their own session on `wait-turn.sh` (new `waiting` state) until their prereqs merge and a slot frees. Batches with dependencies stay autonomous in harnesses that can't start sessions from event turns.
- The watcher wakes only for cards that need the foreman (decisions, stuck, PRs, merged, reported, dropped); `--all` for every change.
- Workers merge `origin/<base>` before opening the PR and before merging; a refused merge ends as `ready note=merge-blocked` and the foreman merges; work found after a merge becomes a follow-up PR (small) or a task file for the foreman to decide.
- `merge auto|manual` and `cleanup auto|ask`, both `auto` by default and stored in `batch.json`; worktrees default to a sibling `<repo-parent>/worktrees/<repo>-<id>`.
- `doctor` suggests allowing `gh pr merge` in Claude Code; `harness.md` documents the event-turn rule and finding a card's session id.
- Scout items follow the scout skill; a tend session keeps the batch's PRs green once workers end.
- Models come from the user's choice per harness (`config.sh`), never hard-coded; `consult.sh` uses the strong one.
- `doctor` checks the companion skills and model settings.

### shared
- Settings: `config.sh` reads `<repo>/.agents/config.json` then `~/.agents/config.json` then defaults; `config.sh list` shows every setting, its value and source; `set --repo` saves per repo. Known settings: models, `tasks.location/commit/ignore`, `foreman.merge/cleanup/max/mode`, `tend.watch/label`.
- `task-dir.sh <slug>`: one place that decides where specs and tasks go; asks `tasks.commit` once per repo and, when specs stay out of git, whether to add `.docs/` to `.gitignore` or `.git/info/exclude`.
- `tasks.js where` also finds `.docs/specs/` (specs kept out of git) and `$FORGE_TASKS_DIRS`; forge reads `tasks.location` / `tasks.commit` (default committed) from the user config.
- `config.sh` (cheap and strong model per harness in `~/.agents/config.json`), `find-skill.sh --list`; `harness.sh`, `verify.sh` moved to `shared/`.
- `tests/run.sh` (offline tests, skill lint) in CI, and `scripts/drill.sh` for live pressure tests against an agent CLI.

### forge and foreman
- `commit.sh`: detects the repo's commit style (commitlint, history: conventional, gitmoji or plain), checks messages and commits exactly the given paths, one concern per commit.
- `pr.sh`: finds the PR template and refuses a body that drops its headings; reuses an open PR instead of opening a second one.
- `cleanup.sh`: removes a finished task's worktree and local branch every time, and refuses to drop unpushed work.
- Shared scripts live in `shared/` and are copied into each skill by `scripts/sync-shared.sh`; CI checks they stay in sync.

### foreman
- Workers turn on their harness's auto-archive when their PR closes, so sessions waiting on a manual merge end on their own.
- A PR closed without merge marks the item `dropped` and ends its worker.
- The foreman ends every finished worker before its final message; `blocked` / `failed` ones stay open for inspection.
- New `/foreman cleanup [batch]`: re-verifies waiting PRs after the batch is over and ends their workers.

## v0.4.0 — 2026-11-15

First tagged release.

### silent
- Runs a task end to end with no talk between tool calls; progress only in the harness's to-do list.
- Ends with one line in English: `done: <where it landed>` (PR, commits, uncommitted changes or an answer); only commits or opens a PR when the task asks.
- Stops only for what is the user's alone, with `blocked: <why>`.

### forge
- Phases with exit gates: brainstorm (why, approaches, design) → grill → spec → task list → TDD → test agents → review → ship → retro.
- Start at any phase (`/forge build`, `/forge test`, `/forge from test`) at three speeds: `fast`, `normal`, `full`.
- Shared task list (`NN-<slug>.md` with `Status`, `Blocked by`, `PR`) on a board, in the repo or in `~/.agents/plans`, managed by `scripts/tasks.js`; `/forge build next` works it one task at a time.
- Independent test agents: adversary, regression, property, mutation, E2E; read-only spec-check and standards reviewers.
- ADRs, glossary and standards docs; optional `guard-tests.sh` pre-edit hook.

### foreman
- Runs one or many tasks in parallel: one silent worker and git worktree per task, each to a PR or a report.
- Takes inline tasks, a task dir, board ids or just an idea (offers to plan it with forge or split it).
- Workers follow forge (`fast` / `normal` / `full`) or plain mode, reading each skill's `SKILL.md` by path.
- Status cards, a zero-token watcher, PR verification, decisions escalated by impact, recovery ladder.
- Harness-agnostic: adapters for Claude Code desktop and CLI, and `agent.sh` for any agent CLI; `doctor` checks only what the detected harness needs.
