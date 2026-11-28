#!/usr/bin/env bash
# Routing eval: vague product ideas go to smelt plan with a brainstorm; clear asks never do.
set -uo pipefail
cd "$(dirname "$0")/../.."
f=skills/use-smithy/SKILL.md; fail=0; bad() { echo "FAIL: $*"; fail=1; }
vague="$(grep -i '^| a vague product idea' "$f")"
[ -n "$vague" ] || bad "use-smithy Route has no vague product idea row"
for w in 'smelt' '`plan`' 'brainstorm' 'grill' 'spec' 'tasks' 'before any build' 'smithy skills --list'; do
  grep -qF -- "$w" <<<"$vague" || bad "vague row lacks: $w"
done
clear="$(grep '^| to build, change or fix code' "$f")"
grep -qi brainstorm <<<"$clear" && bad "clear-ask row must not brainstorm"
while IFS=$'\t' read -r p want; do
  case "$p" in ''|'#'*) continue;; esac
  case "$want" in
    "plan brainstorm") grep -qiF -- "\"$p\"" <<<"$vague" || grep -qi 'no file, stack, or behaviour named' <<<"$vague" || bad "vague case not covered: $p";;
    debug|build) grep -qiF -- "\"$p\"" <<<"$vague" && bad "clear case routed to brainstorm: $p"; grep -qF -- "\`$want\`" <<<"$clear" || bad "clear row lacks \`$want\` for: $p";;
  esac
done < tests/route/cases.tsv
grep -qF 'vague product idea' skills/smelt/SKILL.md || bad "smelt Entry does not name a vague product idea"
[ $fail -eq 0 ] && echo "route: ok"; exit $fail
