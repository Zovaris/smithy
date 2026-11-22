# Keeping docs in sync with code

## 1 · Deterministic checks first

`smithy drift [base]` (default: origin's default branch; a branch name means `origin/<branch>`, as in `smithy scan`) reports:
- **Changed files and the docs that mention them** (by path or by exported symbol name): each
  pairing is a doc to re-read.
- **Missing references:** paths and backticked symbols named in docs that no longer exist in the
  code. A path is only checked when it contains a `/` and a file extension (`scripts/x.sh`,
  not `x.sh`).
- **Always-loaded files:** lines and bytes of AGENTS.md, CLAUDE.md, `docs/agents/STANDARDS.md`
  and `scripts/SCRIPTS.md`, where they exist; STANDARDS.md also shows its rule count and is
  flagged over the budget. A file that keeps growing is trimmed in the same branch.
- **Public API changes without docs:** changed source files with exported symbols whose doc
  comments or module README didn't change in the same diff.

Then run the stack's doc generator in strict mode when it exists (`typedoc --treatWarningsAsErrors`,
`RUSTDOCFLAGS="-D warnings" cargo doc`, `pdoc --fail-on-warning` or mkdocs `--strict`), and the
comment check: `smithy comments --diff <base>`.

## 2 · Then judgement

For each flagged doc, read the doc and the diff and decide:
- Still true → leave it.
- Wrong or stale → fix it in this branch (module README, map, glossary, how-to).
- A decision changed → new ADR superseding the old one; never edit the old decision.
- A line of `docs/agents/STANDARDS.md` or `scripts/SCRIPTS.md` whose covered path is reported
  missing → propose retiring the line or updating its path (`STANDARDS-FORMAT.md`).
- Can't tell from the code → list it for the user; don't guess.

## 3 · Report

List what changed and what is left for the user, with file paths. In a PR body this is the
"Docs updated" section.
