# STANDARDS.md and SCRIPTS.md format

`docs/agents/STANDARDS.md` holds judgement rules reviewers enforce; `scripts/SCRIPTS.md` lists
the repo's reusable scripts. ledger owns writes to both. Every smelt session reads them in
phase 0, so they stay short.

## STANDARDS line

One rule per line, nothing else but the header:

```
- <rule> — covers: `<path>`[, `<path>`…] — from: <evidence>
```

- `<rule>`: one imperative sentence a reviewer can check against a diff.
- `covers`: the backticked repo paths the rule applies to; `smithy drift` reports a path that no
  longer exists.
- `from`: the evidence: a PR, ADR, incident or review finding (`PR 12`, `ADR 0004`).

## Budget and retiring

- Budget: 30 rules (`- ` lines); `smithy drift` flags a file over it. Adding past it means merging
  two rules or retiring one in the same change.
- At each sync, a rule whose covered files changed a lot since it was written, or that no review
  has cited in a long time, is a retire candidate: judge it, propose retiring it (no tooling).
- A rule now enforced by a lint, test or type check is retired: code owns it.
- Retiring is deleting the line; git keeps the history. No "retired" section.
- A rule whose covered path is gone is retired or its `covers` updated.
- ADRs are never deleted, only superseded (`ADR-FORMAT.md`).

## SCRIPTS line

`scripts/SCRIPTS.md` has a header and one line per script:

```
- `scripts/<name>` — <what it does> — <when to use it>
```

The backticked path is what `smithy drift` checks. Add a line when a script is saved; delete it when the script is deleted. Same budget idea: a
script nobody uses any more is removed along with its line.
