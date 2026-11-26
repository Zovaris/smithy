#!/usr/bin/env bash
# Dry run of scripts/eval.sh on the none arm with fake agents (no tokens).
#   tests/eval/run.sh      exit 0 when every assertion passes
set -uo pipefail
cd "$(dirname "$0")/../.."
pass=0; fail=0
ok()  { pass=$((pass+1)); }
bad() { fail=$((fail+1)); echo "FAIL: $*"; }
has() { grep -qF -- "$2" <<<"$1" && ok || bad "$3 (missing: $2)"; }
export AGENT_CLI=custom
# One smithy build and one private Go cache for every eval call below (tests/run.sh passes its smithy).
gc="$(mktemp -d)"; export EVAL_GOCACHE="$gc"
if [ -z "${SMITHY_BIN:-}" ]; then
  bin="$(mktemp -d)"; trap 'rm -rf "$bin" "$gc"' EXIT
  go build -trimpath -o "$bin/smithy" ./cmd/smithy || exit 1; export SMITHY_BIN="$bin/smithy"
fi
# An agent that does nothing: the hidden tests must fail on every task.
out="$(AGENT_CMD=true scripts/eval.sh -a none 2>&1)"
has "$out" "none: pass@1 0.00 (0/3)  pass^1 0.00 (0/3 tasks)" "no-op agent fails every task"
has "$out" "transcripts: " "prints the transcript dir"
# An agent that fixes only the hold task: the check runs the hidden test after the agent.
fixed="$(mktemp)"; trap 'rm -rf "$fixed" "${bin:-}" "$gc"' EXIT
cat > "$fixed" <<'G'
package hold

func Stage(item map[string]any) string {
	switch v := item["hold"].(type) {
	case bool:
		if v {
			return "held"
		}
	case string:
		if v != "" {
			return "held"
		}
	}
	return "queued"
}
G
export FIXED="$fixed"; fix='[ -f hold.go ] && cp "$FIXED" hold.go; true'
out="$(AGENT_CMD="$fix" scripts/eval.sh -k 2 -a none 2>&1)"
has "$out" "none hold #2 pass" "fixed task passes both trials"
has "$out" "none cleanup-branch #1 fail" "unfixed task fails"
has "$out" "none: pass@1 0.33 (2/6)  pass^2 0.33 (1/3 tasks)" "pass@1 and pass^k"
# The agent and the check use the eval's own Go cache, never the shared one.
out="$(AGENT_CMD='go env GOCACHE' scripts/eval.sh -a none hold 2>&1)"; logs="${out#*transcripts: }"; logs="${logs%%$'\n'*}"
[ "$(cat "$logs/none-hold-1.log")" = "$gc" ] && ok || bad "trial uses the eval's GOCACHE"
# An agent that never starts (no log) stops the eval; it is not graded as a task failure.
out="$(SMITHY_BIN=false scripts/eval.sh -a none hold 2>&1)"; rc=$?
[ $rc -eq 1 ] && ok || bad "agent that never starts exits 1 (got $rc)"
has "$out" "eval: agent did not start (none hold #1)" "names the trial whose agent did not start"
scripts/eval.sh -a bogus >/dev/null 2>&1; [ $? -eq 2 ] && ok || bad "unknown arm exits 2"
echo "eval: $pass passed, $fail failed"; [ $fail -eq 0 ]
