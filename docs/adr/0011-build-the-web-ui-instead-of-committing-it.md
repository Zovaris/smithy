# 0011 · Build the web UI where it is needed instead of committing it

- Status: accepted
- Date: 2026-11-24
- Supersedes: the "commit `internal/web/dist`" part of ADR 0010

## Context and Problem Statement

ADR 0010 committed the built web UI (`internal/web/dist`, minified JS/CSS with hashed names) so
`go build` needs no Node, and CI failed when it was stale. The repo should hold sources only:
generated files bloat diffs and every web change needed a committed rebuild.

## Decision Outcome

- `npm run build` in `web/` writes into `internal/web/dist/app`, which git ignores.
- `internal/web/dist/placeholder.html` (plain HTML, Steel colours) is committed. When
  `dist/app/index.html` is absent the harness build serves the placeholder, which says to run
  `npm ci && npm run build` in `web/`. A clean clone builds both builds without Node.
- CI builds the web UI before the jobs that build or test the harness build; the stale-dist check
  is gone, the bundle budget stays.
- The release workflow builds the web UI before the Go builds and fails when the embedded UI is the
  placeholder (`TestReleaseEmbedsBuild` with `SMITHY_REQUIRE_WEB_BUILD=1`).

## Consequences

- Good: no generated files in git or diffs; a web change is one source commit.
- Bad: a harness binary built from a clean clone without Node shows the placeholder, not the UI;
  CI jobs that embed the UI install npm dependencies (cached).
