# 0008 · Agents per speed are set by trial evidence

- Status: proposed
- Date: 2026-11-20

## Context and Problem Statement

Two trials measured real cost (`docs/trials/2026-11-trial.md`, a sandbox; `docs/trials/2026-11-trial-2.md`,
five fixes in `Zovaris/claude-mods`). Every subagent costs a floor of ~55k tokens on a toy repo
and ~73k on real code, whatever its prompt; skill text is a few thousand. In trial 1 a normal
run cost ~219k with four agents and a full run ~497k with nine, and most findings came from one
agent per run. Trial 2, after the changes below, ran normal at 114–150k with two agents, and
those two still found real bugs in 4 of 5 tasks.

## Decision Outcome

Cost is controlled by how many agents run, so each one has to earn its place with findings in a
trial:
- At `normal`, spec-check and standards run as one reviewer; a fixed blocking finding is
  confirmed by resuming the same reviewer; the builder check runs only for specs over 8 criteria or with open
  questions; regression runs only for a reported bug with a repro.
- Trial 3's planted bugs (`docs/trials/2026-11-trial-3.md`): every defect was caught at normal by
  four agents or more; the full-only agents (a second adversary, a separate standards reviewer,
  hostile on a library) found nothing new for +168k. So full runs one adversary and one combined
  reviewer too, and hostile only where something runs; normal caps at three agents.
- Trial 4's A/B (`docs/trials/2026-11-trial-4.md`): one session without smithy built the same
  CLI in ~11 min and ~180k tokens; smithy took ~31 min and ~1.8M, half of it in per-task agents,
  and both caught 3/3 planted defects. So agents run once per change, not once per task: warden
  workers and `build all` tasks build at `fast`, and one wave checks the whole batch or branch.
- The v0.12 A/B rounds (`docs/trials/2026-11-v012-rounds.md`): fast costs about a plain session; warden pays only for big independent tasks.
- Each trial's report lives in `docs/trials/`; this ADR is updated, not duplicated, when one
  changes the agent set.

## Consequences

- Good: normal costs about a third less with no finding lost in trial 2.
- Bad: one reviewer reading both files may weigh standards less; the next trial checks it with a
  planted standards break.
