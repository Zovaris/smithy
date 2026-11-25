# 0010 · A Node toolchain for the web UI, in web/ only

- Status: accepted; committing `internal/web/dist` superseded by ADR 0011
- Date: 2026-11-24

## Context and Problem Statement

v0.20 adds a web UI the daemon serves at `/` (harness build). It uses Sephiro, a React component
toolkit, so it needs a JavaScript build. The repo is Go standard library only (ADR 0009 lists the
harness build's exceptions), and `go build` must keep working on a machine without Node.

## Decision Outcome

- `web/` is the only place with a Node toolchain: Preact with `preact/compat` aliased as React,
  `@zovaris/sephiro` (exact version) with smithy's own Steel theme, Vite, TypeScript and Vitest.
  `package-lock.json` is committed; CI installs with `npm ci`.
- `npm run build` writes into `internal/web/dist`, which is committed and embedded with `go:embed`
  by `internal/web` (`//go:build harness`). `go build` needs no Node; the skills build embeds nothing.
- CI rebuilds `web/` and fails when `internal/web/dist` differs from the commit (stale dist), and
  when the gzipped JS+CSS of the first load is above 120 KB (`web/scripts/budget.mjs`).
- The page is static and holds no secret: the daemon serves it to loopback Hosts, and to `*.ts.net`
  Hosts only for a login in `daemon.allow`. API calls carry the token from the URL fragment
  (`smithy web`), or rely on the Tailscale login header, exactly as the API already does.

## Consequences

- Good: Go users and release builds stay Node-free; the UI gets a real component toolkit and a
  bundle budget CI enforces.
- Bad: a web change needs Node locally and a committed rebuild of `dist`; generated files show in
  diffs; npm dependencies need their own updates.
