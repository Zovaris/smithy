# Agent instructions

- Rules for changes live in [CONTRIBUTING.md](CONTRIBUTING.md): Go standard library only (except `internal/tui`: Charm v2 libs), edit the
  launcher in `shared/` then `go run ./tools/sync-shared`, CHANGELOG entry under **Unreleased**.
- Run `bash tests/run.sh` before pushing; `bash tests/skills.sh` after any skill text edit.
- Branches come from `smithy names branch`; `main` is protected (PRs with green CI only).
- Large or exploratory work merges into a `release/*` branch the maintainer reviews, never straight
  to `main`.
- After a big feature lands, run a cheap-model audit before releasing: coherence between skills,
  contradictions, duplicated rules, and skill size in lines and tokens. Fix what it finds first.
- Skill text and outputs are in English. Never add a feature for its own sake: it must fit the
  few-concepts, evidence-over-claims, user-decides standard.
