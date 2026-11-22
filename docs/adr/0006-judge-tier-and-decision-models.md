# 0006 · Judge tier for consult; decision models and a session watcher stay out

- Status: proposed (decision-model part superseded by 0007)
- Date: 2026-11-19

## Context and Problem Statement

Questions climb three rungs: the worker decides low impact, `smithy consult` settles medium, the
user decides high. Consult used the `strong` tier, the same one that does risky work, while many
users have a smarter, pricier model they want only for these calls. Typed decision models (Jev,
Clef, Span) and a cheap watcher session over all workers were also proposed.

## Decision Outcome

- Add an optional fourth tier, `judge`, used only by `smithy consult` (falls back to `strong`).
  `smithy route` never picks it.
- Decision models: not built. Best fit is a caution-only impact classifier (it may raise a
  question's impact, never lower it), but independent evidence is thin (name-bias flips, no
  accuracy gain over label probabilities) and none speaks an OpenAI-compatible API. Revisit after
  checking agreement offline against `decisions.md` from real batches.
- Watcher session: not built. Weak monitors catch little (low TPR at usable FPR, worse on long
  transcripts); stuck and loop detection already exist (stale cards, attempt cap); lesson mining
  would duplicate the evidence-gated lessons. Revisit only for a rule-triggered flag (same command,
  same error, 3 times) if batches show that failure.

## Consequences

- Good: the smartest model is spent only on decisions, set once per harness.
- Bad: one more optional config key; tier quality of `judge` is the user's choice.
