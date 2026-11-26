# 0015 · One release asset per platform

- Status: accepted
- Date: 2026-11-25
- Supersedes: "Release assets are unchanged for now" in [0014](0014-one-shipped-binary.md)

## Context and Problem Statement

After 0014 every install used the harness build, but releases still shipped two binaries per
platform (`smithy-<os>-<arch>`, the skills build, and `smithy-harness-<os>-<arch>`). Updates also
kept every downloaded version and printed the pre-0.21 migration note on every run, because the
`~/.smithy/bin` compat links it left behind made each run look like a migration.

## Decision Outcome

- `release.yml` builds only the harness build, published as `smithy-<os>-<arch>`.
- `install.sh` (and `smithy update`, which runs it) downloads `smithy-<os>-<arch>`, keeps the new
  version and the one it replaces, and deletes older ones.
- The `~/.smithy/bin` migration runs once: versions move to `~/.local/share/smithy/versions`, then
  `~/.smithy/bin` and `~/.smithy/kind` are removed, so later runs say nothing.
- The skill launcher never downloads: it runs the matching version, else `~/.local/bin/smithy` or
  `smithy` on PATH with a `smithy update <version>` notice, else prints the install line and exits 3.
  `warden doctor` names the same `smithy update <version>`.

## Consequences

- Good: half the release assets; updates are quiet and bounded in disk use.
- Bad: pinning a release before v0.24 (`install.sh 0.23.1`) installs that release's skills build,
  since its `smithy-<os>-<arch>` was the skills build. Skills older than v0.21 that look in
  `~/.smithy/bin` stop finding the binary and print the install line.
