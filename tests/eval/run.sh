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
# An agent that does nothing: the hidden tests must fail on every task.
out="$(AGENT_CMD=true scripts/eval.sh -a none 2>&1)"
has "$out" "none: pass@1 0.00 (0/3)  pass^1 0.00 (0/3 tasks)" "no-op agent fails every task"
has "$out" "transcripts: " "prints the transcript dir"
# An agent that fixes only the hold task: the check runs the hidden test after the agent.
fixed="$(mktemp)"; trap 'rm -f "$fixed"' EXIT
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
scripts/eval.sh -a bogus >/dev/null 2>&1; [ $? -eq 2 ] && ok || bad "unknown arm exits 2"
echo "eval: $pass passed, $fail failed"; [ $fail -eq 0 ]
