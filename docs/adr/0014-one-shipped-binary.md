# 0014 · One shipped binary: harness plus skills

- Status: accepted
- Date: 2026-11-25
- Supersedes: the skills build as an install choice in [0009](0009-harness-build-dependencies.md)

## Context and Problem Statement

ADR 0009 made the skills build (standard library only) the default binary and the harness build
an option, and `install.sh` asked which one to install. In practice the question confused people,
the skills build hid commands they then asked for, and the skills had to be installed in a second
step with Node.

## Decision Outcome

smithy is all-in-one:
- `install.sh` always installs the harness build (`smithy-harness-<os>-<arch>`), then runs
  `smithy skills install`, which copies the skills into every agent it finds. `--no-skills` skips
  that step. `--harness` and `--skills` are accepted and ignored with a one-line note;
  `~/.smithy/kind` is no longer written or read.
- `smithy update` runs the same `install.sh --update`: the harness binary plus the skills.
- The skills build stays only as `go build ./cmd/smithy` without tags, for development; its
  dependency rule (standard library only, except `internal/tui`) still holds for untagged code.
- Release assets: superseded by [0015](0015-one-release-asset-per-platform.md), one `smithy-<os>-<arch>` per platform.

## Consequences

- Good: one install line, no question, no Node; every user gets the daemon, TUI and web.
- Bad: every install downloads the larger harness binary (SQLite, provider SDKs, code mode).
