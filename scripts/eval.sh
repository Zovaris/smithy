#!/usr/bin/env bash
# Outcome eval: does smelt change whether an agent fixes a bug? (costs tokens; manual, never CI)
#   scripts/eval.sh [-k N] [-a none,fast,normal] [task...]     default: 1 trial, every arm and task
# Each tests/eval/<task>/ has fixture/ (a tiny Go package with a bug), prompt.md, hidden/ (the
# regression test, copied in only after the agent is done) and check (the command that grades it).
# (tests/eval/go.mod keeps the hidden tests out of the repo's own `go test ./...`.)
# Per trial: copy fixture/ to a temp dir, run the agent in write mode through `smithy agent`
# (so AGENT_CLI / AGENT_CMD decide the harness, as in drill.sh), copy hidden/ in, run check.
# Arms: `none` prepends "do not use skills"; `fast` / `normal` prepend "use smelt at that speed".
# The none arm is a plain instruction, not a skill-free HOME: an agent that auto-loads installed
# skills may still do so, so run `none` on a machine or profile without them for a clean baseline.
# Prints pass@1 (share of trials that passed) and pass^k (share of tasks passing all k trials).
# One smithy binary ($SMITHY_BIN, else built once here) and one private Go cache ($EVAL_GOCACHE,
# else a fresh one) serve every trial, so a launcher rebuild or another process pruning the shared
# Go cache never grades as a task failure. An agent that never starts (no log) stops the eval with
# exit 1 instead of counting as a fail.
set -uo pipefail
cd "$(dirname "$0")/.."
k=1; arms="none,fast,normal"
while getopts k:a: o; do case $o in k) k=$OPTARG;; a) arms=$OPTARG;; *) exit 2;; esac; done
shift $((OPTIND-1))
case $k in ''|*[!0-9]*|0) echo "eval: -k needs a positive number" >&2; exit 2;; esac
tasks=("$@"); [ ${#tasks[@]} -gt 0 ] || for d in tests/eval/*/; do tasks+=("$(basename "$d")"); done
logs="$(mktemp -d)"; echo "transcripts: $logs"
own="$(mktemp -d)"; trap 'rm -rf "$own"' EXIT; AGENT="${SMITHY_BIN:-$own/smithy}"
[ -n "${SMITHY_BIN:-}" ] || go build -trimpath -o "$AGENT" ./cmd/smithy || { echo "eval: cannot build smithy" >&2; exit 1; }
export GOCACHE="${EVAL_GOCACHE:-$own/gocache}"
intro() { case $1 in
  none) echo "Do not load or use any skills; solve this directly." ;;
  fast|normal) echo "Use the smelt skill at speed $1 for this task." ;;
  *) echo "eval: unknown arm $1" >&2; return 1 ;; esac; }
for arm in ${arms//,/ }; do
  intro "$arm" >/dev/null || exit 2
  trials=0; passed=0; allk=0
  for t in "${tasks[@]}"; do
    d="tests/eval/$t"; [ -d "$d/fixture" ] || { echo "eval: no task $t" >&2; exit 2; }
    wins=0; chk="$(cat "$d/check")"
    for i in $(seq "$k"); do
      w="$(mktemp -d)"; cp -R "$d/fixture/." "$w"
      p="$logs/$arm-$t-$i.prompt"; { intro "$arm"; echo; cat "$d/prompt.md"; } > "$p"
      "$AGENT" agent --cwd "$w" --log "$logs/$arm-$t-$i.log" "$p" >/dev/null 2>"$logs/$arm-$t-$i.err"
      [ -f "$logs/$arm-$t-$i.log" ] || { echo "eval: agent did not start ($arm $t #$i): $(tail -n 1 "$logs/$arm-$t-$i.err")" >&2; rm -rf "$w"; exit 1; }
      cp -R "$d/hidden/." "$w"
      if (cd "$w" && bash -c "$chk") >"$logs/$arm-$t-$i.check" 2>&1; then
        wins=$((wins+1)); r=pass; else r=fail; fi
      echo "  $arm $t #$i $r"; rm -rf "$w"
    done
    trials=$((trials+k)); passed=$((passed+wins)); [ "$wins" -eq "$k" ] && allk=$((allk+1))
  done
  awk -v a="$arm" -v p="$passed" -v n="$trials" -v s="$allk" -v t="${#tasks[@]}" -v k="$k" \
    'BEGIN{printf "%s: pass@1 %.2f (%d/%d)  pass^%d %.2f (%d/%d tasks)\n", a, p/n, p, n, k, s/t, s, t}'
done
