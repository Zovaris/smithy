# 0005 · Learning lives in repo docs, not a memory service

- Status: proposed
- Date: 2026-11-19

## Context and Problem Statement

The maintainer wanted smithy to improve itself across sessions ("dreaming"). Research found
post-hoc reflection helps only when grounded in an external signal, and memory stores that only
grow cause error propagation and cost (arXiv 2505.16067, 2602.11988).

## Considered Options

- A post-merge "dream" pass with its own store — three new concepts, unproven need.
- A memory service (mem0, Letta) — vendor-run numbers, not reviewed in PRs.

## Decision Outcome

Chosen option: "evidence-backed proposals into repo docs": smelt proposes up to 3 lessons per PR,
each citing a receipt, test, finding or ruling; warden lists its decisions at batch end; accepted
rules go to `docs/agents/STANDARDS.md` / `scripts/SCRIPTS.md`, owned by ledger with a rule budget
with stale or over-budget lines flagged by `smithy drift` and retired by judgement. Nothing is written without the user's yes.

## Consequences

- Good: every session and worker reads the same reviewed rules; the store stays small.
- Bad: learning depends on the user accepting proposals.
