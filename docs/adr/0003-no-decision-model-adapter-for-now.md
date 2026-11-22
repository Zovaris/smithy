# 0003 · No decision-model adapter for now

- Status: superseded by 0007
- Date: 2026-11-19

## Context and Problem Statement

Small "decision models" (Respan Span-01 Lite / Span-02, TypeSafe Jev, Cloudflare Clef) and routers
(RouteLLM, Arch-Router, NotDiamond) could pick a model tier in `smithy route` or settle choices in
`smithy consult`.

## Considered Options

- Adapter per provider — adapter sprawl, early-access APIs.
- One OpenAI-compatible `route.judge` hook with a 2 s timeout falling back to `Route()` — the
  smallest design if ever needed.

## Decision Outcome

Chosen option: "no adapter now", because `smithy route` is deterministic, free and offline;
quality numbers for these models are vendor-reported; and `consult` needs reasoning over repo
context, which a classifier doesn't give. Revisit when logs show misrouted tiers that matter and a
model has a stable, generally available API; then build only the single `route.judge` hook.

## Consequences

- Good: no network dependency or key in the routing path.
- Bad: tier choice stays heuristic.
