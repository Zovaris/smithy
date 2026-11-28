# 0016 · Go package layout and deferred refactors

- Status: accepted
- Date: 2026-11-26

## Context and Problem Statement

An architecture review before v0.25 found the same helpers copied across packages (git test
helpers, `plural`, `ago`, byte sizes), home lookups done ad hoc in about thirty places, agent CLI
facts (names, binaries, install hints, effort, ACP, pool login) spread over a dozen literal tables,
and the effort rule written once in `agents` and again in `daemon`. It also found large types and
functions that are risky to split right before a release.

## Decision Outcome

v0.25 lands the mechanical part, with no behaviour change:

- `internal/testkit` holds the test-only git helpers (`Git`, `CommitFile`, `Repo`).
- `internal/text` holds `Plural`, `Ago` and `Bytes` next to the text cleaners.
- `internal/layout` owns every home lookup: `UserHome`, `Home` (`$SMITHY_HOME`, else ~/.smithy,
  never the real one under `go test`), `WardenHome`, `PlansHome` and `Tilde`. It imports nothing
  from smithy. `ghx.Home`, `cards.Home` and `tasks.PlansHome` are gone.
- `internal/agents/catalog` is the one agent CLI table (name, binary, install hint, takes effort,
  takes skills, consult fallback, tier harness, ACP command, model list command, pool login and
  config env). It imports nothing from smithy, so `pool` can read it; `agents.KnownCLIs` re-exports
  it (`skills.Known` names the repo's skills). `CLIs`, `EffortFlags`, `ACPCLIs`,
  `InstallHint`, `pool.Harnesses` and the warden and consult lists derive from it.
- Effort has one rule: `agents.Efforts`, `MaxEffort` and `ClampEffort`. `daemon.ValidEffort` uses
  `ClampEffort`, and `GET /v1/setup` reports `effort` (`agents`, `levels`, `max`) so clients stop
  hard-coding it.

Target layout for later releases, each in its own PR:

- `internal/api`: the daemon's wire types and client, so the TUI and other clients stop importing
  the whole daemon. Done in v0.26: the request and response types and `Client` moved there and the
  daemon aliases them; the TUI still imports the daemon for event kinds, the shell and token files.
- `internal/tui` split into `kit` (widgets, theme), `stats` (dashboard panels) and `chat`. Done in
  v0.26: `kit` holds the themes, dither ramp, charts, header and text helpers; the modal stays in
  `chat`, since every modal is drawn by and acts on the chat view.
- `internal/agents` split into `sandbox` (native and API sandboxes, guard) and `skills` (find,
  install, inventory). Done in v0.26: `internal/agents/sandbox` and `internal/agents/skills`
  import nothing from `agents`; the guard and native settings that read a run's `Options` stay
  in `agents`.
- One shared `httpx` client (timeouts, user agent, size limits) for the HTTP callers.
- Break up `chatView` (158 methods) and `daemon.Server` (96 methods) along those lines.
- Shorten the longest functions: `warden.Brief`, `plan.Stack`, `agents.Run`, `warden.Doctor`,
  `chatView.update`, `Model.key`.
- v0.26 drops the `smithy tui` alias of `smithy stats`. Done, with the `tui` settings group:
  `tui.reduce_motion` is `ui.reduce_motion`, and the old key is still read.

## Consequences

- Good: one place to add an agent CLI, change a home path or the effort cap; tests stop copying
  git setup.
- Bad: the deferred splits keep the large types for one more release; harness detection
  (`internal/harness`), per-CLI argv building and MCP config paths stay as code, not table rows,
  since they are behaviour rather than data.
