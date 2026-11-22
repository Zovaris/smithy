# Contributing

- The binary's source is in `cmd/smithy` and `internal/` (Go, standard library only). Inside a
  checkout the launcher builds it from source when a `.go` file changed, so symlinked skills
  always run your working copy.
- Pull requests run `check` (gofmt, vet, `go test`, every offline test through the binary, a
  build per release target). A `v*` tag runs `release`: it checks the tag matches
  `shared/VERSION`, builds darwin, linux (arm64, amd64) and windows binaries, gzips them,
  writes `checksums.txt`, attests the build and publishes the GitHub release with the
  CHANGELOG section as notes.
- `shared/` holds the launcher (`smithy`, `smithy.cmd`) and `VERSION`; edit them there and run
  `go run ./tools/sync-shared`, which copies them into each skill (each skill installs on its own).
- `tests/run.sh` runs every offline test (`go test`, skill lint via `tests/skills.sh`, and the
  `tests/shared`, ledger, warden, scout and tend scripts); CI runs it. After editing any skill
  text, run `bash tests/skills.sh` before pushing: it checks cross-skill references CI would reject.
- `scripts/drill.sh` pressure-tests skills against a real agent CLI (`tests/drill/*.md`); it
  costs tokens, so run it by hand per harness.
- `scripts/eval.sh [-k N] [-a none,fast,normal] [task...]` measures outcomes: for each
  `tests/eval/<task>/` it copies `fixture/` (a tiny Go package with a bug) to a temp dir, runs the
  agent in write mode on `prompt.md`, copies the hidden regression test in and runs `check`. It
  prints pass@1 and pass^k per arm and the folder holding the transcripts. Every trial is a real
  agent run, so it costs tokens: run it by hand, never in CI. The `none` arm only tells the agent
  not to use skills; use a profile without the skills installed for a clean baseline.
  `tests/eval/run.sh` is its offline dry run with fake agents.

- `main` is protected: changes land through a pull request with green `check` and
  `windows-launcher`; no direct pushes, force-pushes or deletion. A release is a PR that bumps
  `shared/VERSION` and the CHANGELOG, then a `v*` tag on the merged commit.
- Pull requests follow `.github/pull_request_template.md`.

Commits are atomic and conventional (`smithy commit style` shows the rules); every change lands under
**Unreleased** in [CHANGELOG.md](CHANGELOG.md) first.
