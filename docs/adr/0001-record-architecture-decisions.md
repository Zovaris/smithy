# 0001 · Record architecture decisions

- Status: proposed
- Date: 2026-11-19

## Context and Problem Statement

smithy's design choices (what the binary does vs the skills, which dependencies it takes, how
merges are decided) were spread across CHANGELOG entries and chat history. Agents and reviewers
need the why in the repo.

## Considered Options

- Keep decisions in the CHANGELOG — it says what changed, not why or what was rejected.

## Decision Outcome

Chosen option: "ADRs in `docs/adr/`, written with ledger's `adr` mode", because ledger already
owns that format and `smithy search` reads it first.

## Consequences

- Good: rejected ideas stay rejected with their reason, so research does not reopen them blindly.
- Bad: one more file per hard-to-reverse decision.
