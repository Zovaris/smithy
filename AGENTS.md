# Agent instructions

- Rules for changes live in [CONTRIBUTING.md](CONTRIBUTING.md): Go standard library only (except `internal/tui`: Charm v2 libs; `-tags harness` code: ADRs 0009, 0012, 0013; `web/` is Node, ADR 0010/0011: its build `internal/web/dist/app` is not committed; without it the harness build serves a placeholder), edit the
  launcher in `shared/` then `go run ./tools/sync-shared`, CHANGELOG entry under **Unreleased**.
- Run `bash tests/run.sh` before pushing; `bash tests/skills.sh` after any skill text edit.
- Branches come from `smithy names branch`; `main` is protected (PRs with green CI only).
- Large or exploratory work merges into a `release/*` branch the maintainer reviews, never straight
  to `main`.
- After a big feature lands, run a cheap-model audit before releasing: coherence between skills,
  contradictions, duplicated rules, and skill size in lines and tokens. Fix what it finds first.
- Skill text and outputs are in English. Never add a feature for its own sake: it must fit the
  few-concepts, evidence-over-claims, user-decides standard.
