# 0009 · The harness build and what it may depend on

- Status: accepted
- Date: 2026-11-24

## Context and Problem Statement

v0.18 adds a daemon, `smithy attach` and a built-in tracker, which need durable storage. The
binary behind the skills is Go standard library only (except `internal/tui`), and every skill
user downloads it, so a database must not make it heavier or pull cgo into it.

## Decision Outcome

One command, two builds from the same source, commands and config:
- **skills build** (default): standard library only, except `internal/tui` (Charm v2 libraries
  and harmonica, already allowed). Harness-only commands (`daemon`, `attach`, `board`, `chat`) stay
  hidden and print how to get the harness build, exit code 2.
- **harness build** (`go build -tags harness ./cmd/smithy`): harness-only packages carry
  `//go:build harness`. Exception: the daemon's auth and detach helpers stay untagged so the
  skills build's `smithy doctor` can check a running daemon; they import no SQLite. It may also depend on:
  - `modernc.org/sqlite` (pure Go, no cgo), latest release, for `internal/store`
    (`~/.smithy/smithy.db`, WAL, append-only `events`, a `migrations` table);
  - a bundled ripgrep binary (see the 2026-11-25 amendment below).
  It does not need `tsnet`: remote access goes through `tailscale serve` in front of a
  loopback-only daemon, with no Tailscale code in smithy.
- Release assets, per platform (darwin arm64/amd64, linux arm64/amd64, windows amd64), all
  `CGO_ENABLED=0`: `smithy-<os>-<arch>` (skills) and `smithy-harness-<os>-<arch>` (harness),
  gzipped except on Windows (`.exe`), both in `checksums.txt` and attested.
- CI vets, tests and cross-builds both builds; a test fails if the skills build links SQLite.
- `install.sh` keeps installing the skills build until v0.19.

## Consequences

- Good: skill users keep a small stdlib binary; the harness gets SQLite without cgo, so
  cross-builds stay one `go build` each.
- Bad: two binaries per platform to release and test; a harness-only change can break only the
  tagged build, so CI must keep running `-tags harness`. `modernc.org/sqlite` adds several MB
  and transitive modules to the harness build.

## Amendment (2026-11-25): bundled ripgrep

`smithy search --code` needs fast repo search, so releases now bundle ripgrep:

- ripgrep ships per platform as `smithy-rg-<os>-<arch>`, next to the smithy binaries, in
  `checksums.txt` and the build attestation; `install.sh` installs it next to smithy after the same
  checks, best effort.
- The version is pinned (14.1.1) with one sha256 per upstream archive in `scripts/fetch-ripgrep.sh`;
  a mismatch fails the release.
- Its license (MIT or Unlicense) ships with the release as `ripgrep-LICENSE.txt`.
- Without it, `internal/search` falls back to rg on PATH, then `git ls-files`, then a stdlib walker,
  so smithy links no ripgrep code and keeps working when the binary is missing.

## Amendment (2026-11-25): API backend and code mode

The harness build also depends on the provider SDKs for the API backend (ADR 0012) and on
`fastschema/qjs` with `wazero` for code mode (ADR 0013). Both stay behind `-tags harness`.
