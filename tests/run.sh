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
# golangci-lint runs when installed; CI pins the version (GOLANGCI_LINT_VERSION) and always runs it.
GOLANGCI_LINT_VERSION=2.15.0
if command -v golangci-lint >/dev/null 2>&1; then
  have="$(golangci-lint version --short 2>/dev/null)"
  [ "${have#v}" = "$GOLANGCI_LINT_VERSION" ] || echo "note: golangci-lint $have here, CI pins $GOLANGCI_LINT_VERSION" >&2
  golangci-lint run ./... || rc=1
else
  echo "note: golangci-lint not installed; CI runs it (v$GOLANGCI_LINT_VERSION)" >&2
fi
tests/skills.sh || rc=1
for t in tests/*/run.sh; do bash "$t" || rc=1; done
exit $rc
