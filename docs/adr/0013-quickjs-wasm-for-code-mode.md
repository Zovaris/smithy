# 0013 · QuickJS in WebAssembly for code mode

- Status: accepted
- Date: 2026-11-25

## Context and Problem Statement

The API loop (ADR 0012) can let the model write one short JavaScript program that calls several
tools, instead of one model turn per tool call, which may save tokens on MCP servers with many
small calls. The program is model-written code, so it must run sandboxed: no files, no network,
bounded memory and time, and every tool call still checked like a direct one. The build has no
cgo (ADR 0009).

## Decision Outcome

The harness build may also depend on `github.com/fastschema/qjs` (QuickJS-ng compiled to
WebAssembly, run by `github.com/tetratelabs/wazero`, pure Go). Only `internal/codemode` imports it,
behind `//go:build harness`; a test fails if the skills build links it.

- `codemode.Wrap(tools, limits)` adds one tool, `run_code`, whose description lists a typed
  `tools.<name>(args)` binding per loop tool (from each JSON schema). The program's only way out is
  one host function that runs `Permit` then `Call` on the wrapped tools.
- Caps: QuickJS's heap limit plus a watchdog on the WebAssembly memory (the heap limit misses
  ArrayBuffer storage), 64 MB by default; 30 s through a context that closes the module.
- No filesystem or network: WASI has no sockets; the library mounts a host folder as `/`, so each
  run gets a fresh empty read-only temp dir, removed afterwards. Timers, `std`, `os` and `fetch`
  globals are deleted.
- Off by default (`agent.code_mode`); turned on only if `smithy bench` shows token savings on
  real MCP servers.

## Consequences

- Good: a real JS engine with no cgo and a WebAssembly memory boundary around it.
- Bad: a young library (v0.0.x) and a ~1 MB WASM blob in the harness build; `qjs:std` stays
  importable inside the sandbox, so the empty read-only mount is what keeps it harmless.
