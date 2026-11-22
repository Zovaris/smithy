# 0004 · Big changes land on a release branch first

- Status: proposed
- Date: 2026-11-19

## Context and Problem Statement

Agents can now ship many PRs unattended. `main` is protected and is what users install from, so
research-driven or large changes need a human review point that doesn't block small fixes.

## Considered Options

- Auto-merge everything to `main` — no review of direction.
- Manual merge of every PR — the maintainer becomes the bottleneck overnight.

## Decision Outcome

Chosen option: "a `release/*` branch": warden batches for large or exploratory work target it with
merge auto; the maintainer reviews the branch and merges it to `main` (production) themself. After
each big feature a cheap-model audit checks coherence, contradictions and skill size (AGENTS.md).

## Consequences

- Good: unattended work keeps moving; one review covers a coherent set of changes.
- Bad: release branches need rebasing when `main` moves.
