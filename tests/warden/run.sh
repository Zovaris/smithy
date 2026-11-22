#!/usr/bin/env bash
# Offline tests for warden's status cards, wait-turn and the actionable watcher.
set -uo pipefail
F="$(cd "$(dirname "$0")/../../skills/warden/scripts" && pwd -P)"
pass=0; fail=0
t() { if eval "$2" >/dev/null 2>&1; then pass=$((pass+1)); else fail=$((fail+1)); echo "FAIL: $1"; fi; }
export WARDEN_HOME="$(mktemp -d)"; trap 'rm -rf "$WARDEN_HOME"' EXIT
B="$WARDEN_HOME/b1"; mkdir -p "$B"
echo '{"batch":"b1","max":1,"items":[{"id":"a"},{"id":"b","prereqs":["a"]},{"id":"c"},{"id":"d","prereqs":["x"]},{"id":"x"}]}' > "$B/batch.json"
"$F/smithy" cards set b1 a state=working phase=build >/dev/null
t "b waits while prereq a works" '$F/smithy wait-turn b1 b 0; [ $? -eq 4 ]'
t "b card is waiting" '[ "$(jq -r .state "$B/b.json")" = waiting ]'
t "c waits for a slot (max 1)" '$F/smithy wait-turn b1 c 0; [ $? -eq 4 ]'
t "waiting is not stale-stuck" '"$F/smithy" cards board b1 --json | jq -e ".items[] | select(.id==\"b\") | .stage == \"waiting\""'
"$F/smithy" cards set b1 x state=dropped >/dev/null
t "prereq dropped → blocked" '$F/smithy wait-turn b1 d 0; [ "$(jq -r .state "$B/d.json")" = blocked ]'
"$F/smithy" cards set b1 a state=merged >/dev/null
t "b goes once a merged and a slot is free" '$F/smithy wait-turn b1 b 1 | grep -q "go b"'
t "unknown item → exit 2" '$F/smithy wait-turn b1 zz 0; [ $? -eq 2 ]'
( sleep 2; "$F/smithy" cards set b1 b phase=test progress=2/5 >/dev/null ) &
t "progress-only change → heartbeat" '$F/smithy wait batch:b1 --max 0; [ $? -eq 4 ]'
wait
( sleep 2; "$F/smithy" cards set b1 b state=pr_open pr=u >/dev/null ) &
t "pr_open wakes the watcher" '$F/smithy wait batch:b1 --max 1 | grep -q "b  verify"'
wait

WB="$F/../references/worker-brief.md"
t "brief: workers don't wait on CI" '! grep -q "wait pr:" "$WB" && grep -q "Don.t wait for CI" "$WB"'
t "brief: branch wait is not a slot" '! grep -q "branch:<name>" "$WB"'
t "brief: PR body file stays out of the worktrees folder" 'grep -q "body file.*\.smelt/" "$WB"'
t "ship: PR body file stays out of the worktrees folder" 'grep -q "body file.*\.smelt/" "$F/../../smelt/references/SHIP.md"'
AC="$F/../references/acting.md"
t "acting: batch end lists decisions and followups" 'grep -q "batch end" "$AC" && grep -q "decisions.md" "$AC" && grep -q "followup:" "$AC"'
t "acting: candidate standards are proposal only" 'grep -q "candidate standard:" "$AC" && grep -q "without the user.s yes" "$AC"'
t "acting: empty batch end line" 'grep -q "no decisions or followups" "$AC"'
t "state: decisions.md is read at batch end" 'grep -q "Read at batch end" "$F/../references/state.md"'
echo "warden: $pass passed, $fail failed"; [ $fail -eq 0 ]
