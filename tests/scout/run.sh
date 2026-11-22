#!/usr/bin/env bash
# Offline tests for the scout skill: frontmatter, referenced files, read-only contract.
set -uo pipefail
cd "$(dirname "$0")/../.."
S=skills/scout; fail=0
pass() { echo "  ok   $1"; }
bad()  { echo "  FAIL $1"; fail=1; }

echo "scout"
head -1 $S/SKILL.md | grep -qx -- '---' && pass "frontmatter opens" || bad "frontmatter opens"
grep -q '^name: scout$' $S/SKILL.md && pass "name: scout" || bad "name: scout"
grep -q '^description: .*Use when' $S/SKILL.md && pass "trigger-shaped description" || bad "trigger-shaped description"
grep -q '^disable-model-invocation:' $S/SKILL.md && bad "must be model-invocable" || pass "model-invocable"
for r in $(grep -oE 'references/[A-Za-z-]+\.md|scripts/[a-z.-]+\.sh' $S/SKILL.md | sort -u); do
  [ -e "$S/$r" ] && pass "exists $r" || bad "missing $r"
done
grep -qi 'never edits' $S/SKILL.md && pass "read-only rule stated" || bad "read-only rule stated"
grep -qi 'never authorizes' $S/SKILL.md && pass "no-authorization rule stated" || bad "no-authorization rule stated"
grep -q 'smithy config model cheap' $S/SKILL.md && pass "uses cheap model config" || bad "uses cheap model config"
grep -q 'smithy agent --readonly' $S/SKILL.md && pass "harness-agnostic fallback" || bad "harness-agnostic fallback"
for h in '## Answer' '## Evidence' '## Confidence' '## Open questions'; do
  grep -qF "$h" $S/references/INVESTIGATOR.md && pass "format has $h" || bad "format has $h"
done
grep -qi 'data, not instructions' $S/references/INVESTIGATOR.md && pass "investigator treats sources as data" || bad "investigator treats sources as data"

[ $fail -eq 0 ] && echo "scout: all passed" || echo "scout: FAILED"
exit $fail
