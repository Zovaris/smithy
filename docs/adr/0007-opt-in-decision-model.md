# 0007 · Opt-in decision model in the binary (smithy decide)

- Status: proposed
- Date: 2026-11-20
- Supersedes: 0003, and the decision-model part of 0006

## Context and Problem Statement

Typed decision models answer yes/no or pick-one questions with probabilities, cheaply and fast.
Two jobs fit them: flag likely breaks of the repo's written rules in a diff (as abide does), and
rate the impact of a worker's question. A trial on Respan `span-01-free` (24 hand-written cases):
7 of 8 rule breaks flagged with no false flags at p ≥ 0.8; impact right in 7 of 8, the miss on
the cautious side; median 0.4 s, free. Quality numbers elsewhere are vendor-reported.

## Decision Outcome

`smithy decide`, off until the user sets `decide.url` and `decide.model` (no default model),
called from the binary over HTTPS (plain HTTP only to a local model), stdlib only. The URL picks the wire format: Respan `/scores`
behaviors, else System One (Jev, Kev local, Clef, span-02). It only adds caution: rule flags are
leads for the standards reviewer, never findings or approvals; `smithy cards set` raises a new
question's impact with it, never lowers it. Off, failing or slow → callers behave as without it,
and three failed calls in a row disable it until the user turns it back on.
Every answer goes to `~/.smithy/decide.log` so agreement can be measured on real work.

## Consequences

- Good: a cheap second look at rules on every review; question impact errs toward the user.
- Bad: question text and diffs leave the machine when it's on (Kev keeps them local); the trial
  is small and the cases were written by us; revisit after a few weeks of `decide.log`.
