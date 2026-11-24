#!/usr/bin/env bash
# Runs every offline test. `scripts/drill.sh` (live agent runs) is separate and optional.
set -uo pipefail
cd "$(dirname "$0")/.."
rc=0
# Tests never touch the user's real ~/.smithy (events, cards, notify state).
home="$(mktemp -d)"; export SMITHY_HOME="$home" WARDEN_HOME="$home/warden"; trap 'rm -rf "$home"' EXIT
# One build of the smithy binary for every test (the launcher would build or download it per call).
if [ -z "${SMITHY_BIN:-}" ]; then
  out="$(mktemp -d)"; trap 'rm -rf "$out" "$home"' EXIT
  go build -trimpath -o "$out/smithy" ./cmd/smithy || exit 1
  export SMITHY_BIN="$out/smithy"
fi
[ -n "${SKIP_GO_TEST:-}" ] || go test ./... >/dev/null || { go test ./...; rc=1; }
go run ./tools/sync-shared --check || rc=1
tests/skills.sh || rc=1
for t in tests/*/run.sh; do bash "$t" || rc=1; done
exit $rc
