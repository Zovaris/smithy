#!/usr/bin/env bash
# Every skill: frontmatter name matches its folder, has a description, invocation mode is the
# intended one, English only in fixed outputs, no agent-kit, referenced files exist.
set -uo pipefail
cd "$(dirname "$0")/.."
USER_INVOKED="refine"
fail=0; bad() { echo "FAIL: $*"; fail=1; }
for d in skills/*/; do
  s="$(basename "$d")"; f="$d/SKILL.md"
  [ -f "$f" ] || { bad "$s: no SKILL.md"; continue; }
  head -1 "$f" | grep -qx -- '---' || bad "$s: no frontmatter"
  grep -qx "name: $s" "$f" || bad "$s: name doesn't match folder"
  grep -q '^description: .\{40,\}' "$f" || bad "$s: description missing or too short"
  if [[ " $USER_INVOKED " == *" $s "* ]]; then grep -q '^disable-model-invocation: true' "$f" || bad "$s: should be user-invoked"
  else grep -q '^disable-model-invocation' "$f" && bad "$s: should be model-invocable"; fi
  if grep -q '^argument-hint:' "$f"; then grep -q '^argument-hint: "' "$f" || bad "$s: argument-hint must be quoted"; fi
  grep -q '^argument-hint:.*\\|' "$f" && bad "$s: argument-hint has a backslash escape"
  grep -q '^<[A-Z-]*>$' "$f" && bad "$s: pseudo-tag line in SKILL.md"
  grep -rqi 'agent-kit' "$d" && bad "$s: mentions agent-kit"
  for r in $(grep -ohE '(^|[ `(])((references|agents)/[A-Za-z0-9_-]+\.md|scripts/[a-z0-9_.-]+\.(sh|js))' "$f" | sed -E 's/^[ `(]//' | sort -u); do
    [ -e "$d/$r" ] || bad "$s: SKILL.md references missing $r"
  done
done
for f in skills/*/SKILL.md; do
  s="$(basename "$(dirname "$f")")"; max=76; [ "$s" = smelt ] && max=96
  n=$(awk 'f>=2{c++} /^---$/{f++} END{print c+0}' "$f"); [ "$n" -le "$max" ] || bad "$s: SKILL.md body has $n lines (max $max)"
done
for f in skills/*/references/*.md skills/*/agents/*.md; do
  [ -f "$f" ] || continue; n=$(($(wc -l <"$f"))); [ "$n" -le 120 ] || bad "$f has $n lines (max 120)"
done
vendor='Claude Code|SendMessage|start_session|archive_session|PreToolUse|run_in_background|Agent tool|spawn_agent|codex exec'
for f in $(grep -rlE "$vendor" skills --include='*.md' | grep -v '^skills/warden/references/harness.md$'); do
  bad "$f names a vendor tool (only warden/references/harness.md may)"
done
[ $fail -eq 0 ] && echo "skills: ok"; exit $fail
