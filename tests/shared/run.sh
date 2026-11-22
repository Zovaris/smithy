#!/usr/bin/env bash
# Offline tests for shared/ scripts. Builds throwaway repos; needs git, node, jq.
set -uo pipefail
S="$(cd "$(dirname "$0")/../../shared" && pwd -P)"
P="$(cd "$(dirname "$0")/../../skills/use-smithy/scripts" && pwd -P)"
pass=0; fail=0
t() { if eval "$2" >/dev/null 2>&1; then pass=$((pass+1)); else fail=$((fail+1)); echo "FAIL: $1"; fi; }
T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
if [ -z "${SMITHY_BIN:-}" ]; then
  (cd "$S/.." && go build -trimpath -o "$T/smithy-bin" ./cmd/smithy) || exit 1
  export SMITHY_BIN="$T/smithy-bin"
fi
# The host agent session must not leak into the tests.
unset CLAUDECODE CLAUDE_CODE_ENTRYPOINT CODEX_THREAD_ID CODEX_CI CODEX_SANDBOX CODEX_SANDBOX_NETWORK_DISABLED OPENCODE OPENCODE_SESSION_ID GEMINI_CLI PI_CODING_AGENT PI_SESSION_ID DSH_SESSION_ID AI_AGENT CURSOR_AGENT CURSOR_TRACE_ID AGENT_CLI AGENT_CMD AGENT_MODEL AGENT_EFFORT AGENT_ARGS CONSULT_CMD CONSULT_MODEL
export SMITHY_CONFIG="$T/config.json" WARDEN_HARNESS=codex

git init -q --bare "$T/remote.git"; git clone -q "$T/remote.git" "$T/repo" 2>/dev/null; cd "$T/repo"
git config user.email t@t; git config user.name t
git switch -q -c main 2>/dev/null || git checkout -q -b main  # CI runners default to master
git commit -q --allow-empty -m "feat(core): init"; git commit -q --allow-empty -m "fix(api): handle nulls"
git push -q origin HEAD:main 2>/dev/null; git remote set-head origin main >/dev/null 2>&1

# smithy names
t "branch from ticket" '[ "$($S/smithy names branch PROJ-123 "Add login with Google")" = feat/PROJ-123-add-login-google ]'
t "branch from issue, type from title" '[ "$($S/smithy names branch "#42" "Fix null crash")" = fix/42-null-crash ]'
t "branch without key" '[ "$($S/smithy names branch - "Update README")" = docs/update-readme ]'
t "branch slug capped at 40" 'b="$($S/smithy names branch 03 "a very long title that keeps going and going past forty chars")" && [ ${#b} -le 52 ]'
t "worktree is a sibling" '[ "$($S/smithy names worktree feat/x)" = "$(cd "$T" && pwd -P)/worktrees/repo-feat-x" ]'
git worktree add -q -b nw "$T/repo/.claude/wt-nw" HEAD 2>/dev/null
t "worktree name from inside a worktree uses the main checkout" '[ "$(cd "$T/repo/.claude/wt-nw" && $S/smithy names worktree feat/y)" = "$(cd "$T" && pwd -P)/worktrees/repo-feat-y" ]'
git worktree remove --force "$T/repo/.claude/wt-nw"; git branch -q -D nw; rm -rf "$T/repo/.claude"
t "branch template setting" '$S/smithy config set git.branch "agent/{key}-{slug}" && [ "$($S/smithy names branch 7 "Add box")" = agent/7-add-box ] && $S/smithy config set git.branch "{type}/{key}-{slug}"'

# smithy wait with a fake gh
mkdir -p "$T/bin"
cat > "$T/bin/gh" <<'GH'
#!/usr/bin/env bash
case "$1 $2" in
  "auth status") exit 0 ;;
  "pr view") cat "$T_PR" ;;
  "run view") cat "$T_RUN" ;;
esac
GH
chmod +x "$T/bin/gh"; export T_PR="$T/pr.json" T_RUN="$T/run.json"
W() { PATH="$T/bin:$PATH" "$S/smithy" wait "$@"; }
echo '{"number":5,"url":"u5","state":"OPEN","mergeable":"MERGEABLE","headRefOid":"abc1234","statusCheckRollup":[{"name":"unit","status":"COMPLETED","conclusion":"SUCCESS"}]}' > "$T/pr.json"
t "pr: finished checks reported at once" '[ "$(W pr:5 --max 0)" = "pr 5 checks=green u5" ]'
echo '{"number":5,"url":"u5","state":"OPEN","mergeable":"MERGEABLE","headRefOid":"abc1234","statusCheckRollup":[{"name":"unit","status":"COMPLETED","conclusion":"FAILURE"},{"name":"lint","status":"COMPLETED","conclusion":"SUCCESS"}]}' > "$T/pr.json"
t "pr: red names the failed checks" '[ "$(W pr:5 --max 0)" = "pr 5 checks=red failed=unit u5" ]'
echo '{"number":5,"url":"u5","state":"OPEN","mergeable":"MERGEABLE","headRefOid":"abc1234","statusCheckRollup":[{"name":"unit","status":"IN_PROGRESS"}]}' > "$T/pr.json"
t "pr: pending → heartbeat" 'W pr:5 --max 0; [ $? -eq 4 ]'
( sleep 2; echo '{"number":5,"url":"u5","state":"MERGED","mergeable":"UNKNOWN","headRefOid":"abc1234","statusCheckRollup":[{"name":"unit","status":"IN_PROGRESS"}]}' > "$T/pr.json" ) &
t "pr: merge wakes it" '[ "$(W pr:5 --every 1 --max 1)" = "pr 5 merged u5" ]'
wait
echo '{"status":"completed","conclusion":"failure","name":"CI","url":"r1","jobs":[{"name":"build","conclusion":"failure"},{"name":"lint","conclusion":"success"}]}' > "$T/run.json"
t "run: finished with failed jobs" '[ "$(W run:9 --max 0)" = "run 9 failure failed=build r1" ]'
t "unknown target exits 2" 'W nope:1 --max 0; [ $? -eq 2 ]'

# smithy tasks add / board
export SMITHY_HOME="$T/home"
t "add numbers the next task" '[ "$(basename "$("$S/smithy" tasks add docs/specs/b "Add login")")" = 01-add-login.md ] && [ "$(basename "$("$S/smithy" tasks add docs/specs/b "Rate limit" --blocked-by 01)")" = 02-rate-limit.md ]'
t "added task is todo and blocked" 'grep -q "^Blocked by: 01" docs/specs/b/02-rate-limit.md && [ "$(basename "$("$S/smithy" tasks next docs/specs/b)")" = 01-add-login.md ]'
t "board shows the list" '"$S/smithy" tasks board | grep -q "0/2 done, 0 doing, 1 ready"'
t "board --all finds it from elsewhere" '(cd / && "$S/smithy" tasks board --all) | grep -q "repo/b"'
rm -rf docs/specs/b

# smithy commit
t "style is conventional" '[ "$($S/smithy commit style | head -1)" = "style: conventional" ]'
t "rejects trailing period" '! $S/smithy commit check "feat: add x."'
t "rejects non-conventional" '! $S/smithy commit check "Added stuff"'
t "rejects > 72 chars" '! $S/smithy commit check "feat: $(printf "a%.0s" {1..80})"'
t "accepts good message" '$S/smithy commit check "feat(api): add pagination"'
echo a > a.txt; echo b > b.txt
t "requires paths" '! $S/smithy commit "feat(api): add a"'
t "commits only given paths" '$S/smithy commit "feat(api): add a" a.txt && [ "$(git status --short)" = "?? b.txt" ]'

# smithy pr
t "no template → exit 1" '! $S/smithy pr template'
mkdir -p .github; printf '## Summary\n\n## Testing\n' > .github/pull_request_template.md
t "template found once" '[ "$($S/smithy pr template | wc -l | tr -d " ")" = 1 ]'

# smithy cleanup
git worktree add -q -b t1 "$T/wt1" main; (cd "$T/wt1" && git commit -q --allow-empty -m "feat(x): wip")
t "keeps unpushed branch" '! $S/smithy cleanup . t1 && git show-ref -q --verify refs/heads/t1'
git push -q origin t1 2>/dev/null
t "keeps a branch checked out in a worktree it wasn't given" '! $S/smithy cleanup . t1 && [ -d "$T/wt1" ] && git show-ref -q --verify refs/heads/t1'
t "never removes the folder it runs in" '! (cd "$T/wt1" && $S/smithy cleanup "$T/repo" t1 "$T/wt1") && [ -d "$T/wt1" ]'
t "cleans pushed branch" '$S/smithy cleanup . t1 "$T/wt1" && ! git show-ref -q --verify refs/heads/t1 && [ ! -d "$T/wt1" ]'
git worktree add -q -b t2 "$T/wt2" main; echo dirty > "$T/wt2/x"
t "keeps dirty worktree" '! $S/smithy cleanup . t2 && [ -d "$T/wt2" ]'
git worktree add -q -b t3 "$T/wt3" origin/main; (cd "$T/wt3" && echo r > r.txt && git add r.txt && git commit -q -m "feat(x): rebased")
git worktree add -q --detach "$T/wt4" origin/main; (cd "$T/wt4" && git commit -q --allow-empty -m "chore: other" && git cherry-pick t3 >/dev/null && git push -q origin HEAD:main 2>/dev/null); git worktree remove --force "$T/wt4"; git fetch -q origin
t "cleans a branch whose commits landed rebased" '$S/smithy cleanup . t3 "$T/wt3" && ! git show-ref -q --verify refs/heads/t3'

# smithy tasks
mkdir -p docs/specs/pay
printf '# 01 · A\nStatus: todo\nBlocked by: none\n' > docs/specs/pay/01-a.md
printf '# 02 · B\nStatus: todo\nBlocked by: 01\n' > docs/specs/pay/02-b.md
t "next is 01" '"$S/smithy" tasks next | grep -q 01-a.md'
t "02 blocked" '[ "$("$S/smithy" tasks ready | wc -l | tr -d " ")" = 1 ]'
"$S/smithy" tasks set docs/specs/pay/01-a.md status=done pr=https://x/1 >/dev/null
t "02 ready after 01 done" '"$S/smithy" tasks next | grep -q 02-b.md'
t "rejects bad status" '! "$S/smithy" tasks set docs/specs/pay/02-b.md status=bogus'

mkdir -p .docs/specs/priv elsewhere/x
printf '# 01 · P\nStatus: todo\nBlocked by: none\n' > .docs/specs/priv/01-p.md
printf '# 01 · E\nStatus: todo\nBlocked by: none\n' > elsewhere/x/01-e.md
t "where finds .docs/specs" '"$S/smithy" tasks where | grep -q "/.docs/specs/priv$"'
t "where honours SMELT_TASKS_DIRS" 'SMELT_TASKS_DIRS=elsewhere "$S/smithy" tasks where | grep -q "/elsewhere/x$"'

# smithy config + smithy harness
t "harness override" '[ "$($S/smithy harness)" = "codex any" ]'
t "model unset → exit 3" '$S/smithy config model cheap; [ $? -eq 3 ]'
$S/smithy config set-model cheap mini >/dev/null
t "model saved per harness" '[ "$($S/smithy config model cheap)" = mini ] && jq -e ".models.codex.cheap" "$SMITHY_CONFIG"'

(cd "$T/repo" && $S/smithy config set --repo tasks.commit false >/dev/null)
t "repo setting overrides, false kept as false" '[ "$(cd "$T/repo" && $S/smithy config get tasks.commit)" = false ]'
t "default applies when unset" '[ "$(cd "$T/repo" && $S/smithy config get warden.cleanup)" = auto ]'
t "no default → exit 3" '(cd "$T/repo" && $S/smithy config get tasks.ignore); [ $? -eq 3 ]'
t "task-dir asks for tasks.ignore" 'o="$(cd "$T/repo" && $S/smithy task-dir pay)"; [ $? -eq 3 ] && grep -q "^ask tasks.ignore" <<<"$o"'
(cd "$T/repo" && $S/smithy config set --repo tasks.ignore exclude >/dev/null && $S/smithy task-dir pay >/dev/null && $S/smithy task-dir pay >/dev/null)
t "exclude adds .docs/ once" '[ "$(grep -c "^.docs/$" "$(cd "$T/repo" && git rev-parse --git-path info/exclude)")" = 1 ] && [ -d "$T/repo/.docs/specs/pay" ]'

# no model names in smithy: the user names each tier
t "no suggest command" 'WARDEN_HARNESS=claude-cli $S/smithy config suggest-models 2>/dev/null; [ $? -eq 2 ]'
WARDEN_HARNESS=claude-cli $S/smithy config set-model strong opus >/dev/null
t "set-model saves it for that harness" '[ "$(WARDEN_HARNESS=claude-cli $S/smithy config model strong)" = opus ]'

# smithy route
$S/smithy config set-model mid midm >/dev/null
t "router: typo is cheap" '[ "$($S/smithy route --text "fix typo in README" | cut -d" " -f1)" = cheap ]'
t "router: auth is strong" '[ "$($S/smithy route --text "add OAuth login with tokens" | cut -d" " -f1)" = strong ]'
t "router: plain feature is mid, model from config" '[ "$($S/smithy route --text "add CSV export to reports" | cut -d" " -f1-2)" = "mid midm" ]'
t "router: full speed is strong" '[ "$($S/smithy route --text "add CSV export" --speed full | cut -d" " -f1)" = strong ]'
t "router: review is cheap" '[ "$($S/smithy route --kind review | cut -d" " -f1)" = cheap ]'
t "router: unset tier prints -" '[ "$($S/smithy route --text "add OAuth login" | cut -d" " -f2)" = - ]'
t "router: needs input" '$S/smithy route; [ $? -eq 2 ]'

# smithy consult
t "consult builds a tool-less prompt" 'CONSULT_CMD=cat $S/smithy consult "Q1" "A; B" | grep -q "Question: Q1"'
echo "ctx-line" > "$T/ctx.md"
t "consult adds context" 'CONSULT_CMD=cat $S/smithy consult "Q1" "A; B" --context "$T/ctx.md" | grep -q ctx-line'
t "consult falls back when the harness CLI is missing" 'WARDEN_HARNESS=pi AGENT_CMD="grep -c Question" PATH="/usr/bin:/bin:$(dirname "$(command -v node)")" $S/smithy consult "Q" "A; B" | grep -q 1'

# use-smithy smithy setup block
printf '# Rules\n' > "$T/AGENTS.md"
t "block missing" '! $P/smithy setup block status "$T/AGENTS.md"'
$P/smithy setup block add "$T/AGENTS.md" >/dev/null; $P/smithy setup block add "$T/AGENTS.md" >/dev/null
t "block added once" '[ "$(grep -c use-smithy:start "$T/AGENTS.md")" = 1 ]'
$P/smithy setup block remove "$T/AGENTS.md" >/dev/null
t "block removed, rest kept" '! grep -q use-smithy "$T/AGENTS.md" && grep -q "# Rules" "$T/AGENTS.md"'

# smithy agent with a fake CLI
echo hi > "$T/p.md"
t "smithy agent strips parent harness vars" '[ "$(CLAUDECODE=1 WARDEN_HARNESS=pi AGENT_CMD="echo \${CLAUDECODE:-none} \$WARDEN_HARNESS" $S/smithy agent "$T/p.md")" = "none pi" ]'
t "harness ignores CODEX_HOME" '[ "$(env -i PATH=/usr/bin:/bin CODEX_HOME=/x SMITHY_BIN="$SMITHY_BIN" $S/smithy harness)" = "unknown any" ]'
t "smithy agent pipes prompt to AGENT_CMD" '[ "$(AGENT_CMD=cat $S/smithy agent "$T/p.md")" = hi ]'

# launcher download failures (copied away from the checkout so it can't build)
L="$T/launch"; mkdir -p "$L/curl" "$L/gh"; cp "$S/smithy" "$L/smithy"; echo 0.0.0 > "$L/VERSION"
printf '#!/bin/sh\necho "$@" > "%s/curl.args"\necho "curl: (22) The requested URL returned error: 404" >&2\nexit 22\n' "$L" > "$L/curl/curl"
printf '#!/bin/sh\necho "release not found" >&2\nexit 1\n' > "$L/gh/gh"
chmod +x "$L/curl/curl" "$L/gh/gh"
mkdir -p "$L/sys"
for p in /bin/* /usr/bin/*; do
  n=${p##*/}; [ "$n" = gh ] || [ "$n" = curl ] || [ -e "$L/sys/$n" ] || ln -s "$p" "$L/sys/$n"
done
LR() { env -u SMITHY_BIN SMITHY_HOME="$L/home" PATH="$1:$L/sys" "$L/smithy" version 2>"$L/err"; }
t "launcher: curl 404 exits 3 with one line" 'LR "$L/curl"; [ $? -eq 3 ] && [ "$(wc -l < "$L/err")" -eq 2 ] && grep -q "^smithy: can.t download v0.0.0 (smithy-.*\.gz): .*404" "$L/err" && ! grep -q awk "$L/err"'
t "launcher: curl has timeouts" 'grep -q -- "--connect-timeout 10 --max-time 120" "$L/curl.args"'
t "launcher: gh failure exits 3 with one line" 'LR "$L/gh"; [ $? -eq 3 ] && grep -q "^smithy: can.t download v0.0.0 (smithy-.*\.gz): release not found" "$L/err" && ! grep -q awk "$L/err"'

# launcher watchdog: a hanging gh is stopped (SMITHY_DOWNLOAD_TIMEOUT shortens the wait)
mkdir -p "$L/hang" "$L/slowatt" "$L/okgh" "$L/tmp"
printf '#!/bin/sh\nexec sleep 30\n' > "$L/hang/gh"
cat > "$L/fakegh" <<'EOF'
#!/bin/sh
if [ "$1" = release ]; then
  d=; a=; while [ $# -gt 0 ]; do case $1 in -D) d=$2; shift ;; -p) [ -z "$a" ] && a=$2; shift ;; esac; shift; done
  printf '#!/bin/sh\necho fake-smithy\n' | gzip > "$d/$a"
  echo "$( (command -v sha256sum >/dev/null 2>&1 && sha256sum "$d/$a" || shasum -a 256 "$d/$a") | awk '{print $1}')  $a" > "$d/checksums.txt"
  exit 0
fi
[ "$3" = --help ] && exit 0
[ -n "$HANG_ATTEST" ] && exec sleep 30
exit 0
EOF
cp "$L/fakegh" "$L/slowatt/gh"; cp "$L/fakegh" "$L/okgh/gh"; chmod +x "$L/hang/gh" "$L/slowatt/gh" "$L/okgh/gh"
LW() { env -u SMITHY_BIN SMITHY_HOME="$L/home$2" TMPDIR="$L/tmp" SMITHY_DOWNLOAD_TIMEOUT=1 HANG_ATTEST="$3" PATH="$1:$L/sys" "$L/smithy" version 2>"$L/err"; }
t "launcher: hanging gh download times out with exit 3" 's=$(date +%s); LW "$L/hang" a ""; c=$?; [ $c -eq 3 ] && [ $(( $(date +%s) - s )) -lt 10 ] && grep -q "^smithy: can.t download v0.0.0 (smithy-.*\.gz): timed out" "$L/err"'
t "launcher: hanging attestation times out with exit 3" 'LW "$L/slowatt" b 1; [ $? -eq 3 ] && grep -q "^smithy: can.t verify .*timed out" "$L/err"'
t "launcher: install leaves no temp dir" 'rm -rf "$L/tmp"/*; [ "$(LW "$L/okgh" c "")" = fake-smithy ] && [ -z "$(ls "$L/tmp")" ]'

# launcher without gh: curl download, checksum only, warning once
mkdir -p "$L/nogh" "$L/nogh-bad"
cat > "$L/fakecurl" <<'EOF'
#!/bin/sh
o=; u=; while [ $# -gt 0 ]; do case $1 in -o) o=$2; shift ;; http*) u=$1 ;; esac; shift; done
f=${u##*/}
case $f in
  checksums.txt)
    d=$(dirname "$o"); a=$(ls "$d" | grep '\.gz$' | head -n 1)
    h=$( (command -v sha256sum >/dev/null 2>&1 && sha256sum "$d/$a" || shasum -a 256 "$d/$a") | awk '{print $1}')
    [ "$BAD_SUM" = empty ] && : > "$o" && exit 0
    [ -n "$BAD_SUM" ] && h=0000000000000000000000000000000000000000000000000000000000000000
    echo "$h  $a" > "$o" ;;
  *) printf '#!/bin/sh\necho fake-smithy\n' | gzip > "$o" ;;
esac
EOF
cp "$L/fakecurl" "$L/nogh/curl"; cp "$L/fakecurl" "$L/nogh-bad/curl"; chmod +x "$L/nogh/curl" "$L/nogh-bad/curl"
LN() { env -u SMITHY_BIN -u SMITHY_ALLOW_UNATTESTED SMITHY_HOME="$L/home$2" TMPDIR="$L/tmp" BAD_SUM="$3" PATH="$1:$L/sys" "$L/smithy" version 2>"$L/err"; }
t "launcher: no gh installs on checksum with one warning" '[ "$(LN "$L/nogh" d "")" = fake-smithy ] && [ "$(grep -c "installed with checksum only: install the GitHub CLI to verify the build.s origin" "$L/err")" -eq 1 ]'
t "launcher: no gh, second run is silent" '[ "$(LN "$L/nogh" d "")" = fake-smithy ] && ! grep -q "checksum only" "$L/err"'
t "launcher: no gh + bad checksum refuses" 'LN "$L/nogh-bad" e 1; [ $? -eq 3 ] && grep -q "checksum mismatch" "$L/err" && [ ! -e "$L/home"e/bin/0.0.0/smithy ]'
t "launcher: no gh + empty checksums.txt refuses" 'LN "$L/nogh-bad" g empty; [ $? -eq 3 ] && [ ! -e "$L/home"g/bin/0.0.0/smithy ]'

echo "shared: $pass passed, $fail failed"; [ $fail -eq 0 ]
