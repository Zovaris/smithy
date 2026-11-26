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
t "board shows the list" '"$S/smithy" tasks board | grep "0/2 done, 0 doing, 1 ready"'
t "board --all finds it from elsewhere" '(cd / && "$S/smithy" tasks board --all) | grep "repo/b"'
mkdir -p "$T/imp"; printf '# Plan\n\n## 01 · First\nbody\n\n## 02 Second\nBlocked by: 01\nmore\n' > "$T/imp/list.md"
t "import splits a list into tasks" '"$S/smithy" tasks import "$T/imp/list.md" >/dev/null && [ -f "$T/imp/01-first.md" ] && grep -q "^Blocked by: 01" "$T/imp/02-second.md"'
t "import refuses to overwrite" '! "$S/smithy" tasks import "$T/imp/list.md"'
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
t "commits a git mv rename" 'git mv a.txt c.txt && $S/smithy commit "refactor: rename a to c" a.txt c.txt && [ "$(git status --short)" = "?? b.txt" ] && git ls-files --error-unmatch c.txt >/dev/null'
t "refuses staged files outside the paths" 'h=$(git rev-parse HEAD); git add b.txt; o=$($S/smithy commit "feat: add c" c.txt 2>&1); echo "$o" | grep "already staged: b.txt" && [ "$(git rev-parse HEAD)" = "$h" ]; r=$?; git reset -q b.txt; [ $r -eq 0 ]'
t "dot refuses earlier staging" 'git add b.txt; ! $S/smithy commit "feat: add all" . >/dev/null 2>&1; r=$?; git reset -q b.txt; [ $r -eq 0 ]'
t "subdir cwd sees staging outside it" 'mkdir -p sub && echo s > sub/s.txt && git add b.txt && (cd sub && ! $S/smithy commit "feat: add s" s.txt >/dev/null 2>&1); r=$?; git reset -q b.txt; rm -rf sub; [ $r -eq 0 ]'
t "path outside the repo refused" 'o=$($S/smithy commit "feat: x" .. 2>&1); echo "$o" | grep "outside the repo"'

# smithy pr
t "no template → none" '$S/smithy pr template | grep "^none:"'
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
t "next is 01" '"$S/smithy" tasks next | grep 01-a.md'
t "02 blocked" '[ "$("$S/smithy" tasks ready | wc -l | tr -d " ")" = 1 ]'
"$S/smithy" tasks set docs/specs/pay/01-a.md status=done pr=https://x/1 >/dev/null
t "02 ready after 01 done" '"$S/smithy" tasks next | grep 02-b.md'
t "rejects bad status" '! "$S/smithy" tasks set docs/specs/pay/02-b.md status=bogus'

mkdir -p .docs/specs/priv elsewhere/x
printf '# 01 · P\nStatus: todo\nBlocked by: none\n' > .docs/specs/priv/01-p.md
printf '# 01 · E\nStatus: todo\nBlocked by: none\n' > elsewhere/x/01-e.md
t "where finds .docs/specs" '"$S/smithy" tasks where | grep "/.docs/specs/priv$"'
t "where honours SMELT_TASKS_DIRS" 'SMELT_TASKS_DIRS=elsewhere "$S/smithy" tasks where | grep "/elsewhere/x$"'

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
t "router: review is cheap" '[ "$($S/smithy route --kind review --text x | cut -d" " -f1)" = cheap ]'
t "router: unset tier prints -" '[ "$($S/smithy route --text "add OAuth login" | cut -d" " -f2)" = - ]'
t "router: needs input" '$S/smithy route; [ $? -eq 2 ]'

# smithy consult
t "consult builds a tool-less prompt" 'CONSULT_CMD=cat $S/smithy consult "Q1" "A; B" | grep "Question: Q1"'
echo "ctx-line" > "$T/ctx.md"
t "consult adds context" 'CONSULT_CMD=cat $S/smithy consult "Q1" "A; B" --context "$T/ctx.md" | grep ctx-line'
t "consult falls back when the harness CLI is missing" 'WARDEN_HARNESS=pi AGENT_CMD="grep -c Question" PATH="/usr/bin:/bin:$(dirname "$(command -v node)")" $S/smithy consult "Q" "A; B" | grep 1'

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
L="$T/launch"; mkdir -p "$L/curl" "$L/gh"; cp "$S/../install.sh" "$L/install.sh"; cp "$S/smithy" "$L/smithy"; echo 0.0.0 > "$L/VERSION"
printf '#!/bin/sh\necho "$@" > "%s/curl.args"\necho "curl: (22) The requested URL returned error: 404" >&2\nexit 22\n' "$L" > "$L/curl/curl"
printf '#!/bin/sh\necho "release not found" >&2\nexit 1\n' > "$L/gh/gh"
chmod +x "$L/curl/curl" "$L/gh/gh"
mkdir -p "$L/sys"
for p in /bin/* /usr/bin/*; do
  n=${p##*/}; [ "$n" = gh ] || [ "$n" = curl ] || [ -e "$L/sys/$n" ] || ln -s "$p" "$L/sys/$n"
done
LR() { env -u SMITHY_BIN -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/H" SMITHY_HOME="$L/home" PATH="$1:$L/sys" "$L/install.sh" 0.0.0 2>"$L/err"; }
t "install.sh: curl 404 exits 3 with one line" 'LR "$L/curl"; [ $? -eq 3 ] && [ "$(wc -l < "$L/err")" -eq 2 ] && grep -q "^smithy install: can.t download v0.0.0 (smithy-.*\.gz): .*404" "$L/err" && ! grep -q awk "$L/err"'
t "install.sh: curl has timeouts" 'grep -q -- "--connect-timeout 10 --max-time 120" "$L/curl.args"'
t "install.sh: gh failure exits 3 with one line" 'LR "$L/gh"; [ $? -eq 3 ] && grep -q "^smithy install: can.t download v0.0.0 (smithy-.*\.gz): release not found" "$L/err" && ! grep -q awk "$L/err"'

# launcher watchdog: a hanging gh is stopped (SMITHY_DOWNLOAD_TIMEOUT shortens the wait)
mkdir -p "$L/hang" "$L/slowatt" "$L/okgh" "$L/tmp"
printf '#!/bin/sh\nexec sleep 30\n' > "$L/hang/gh"
cat > "$L/fakegh" <<'EOF'
#!/bin/sh
if [ "$1" = release ]; then
  d=; a=; while [ $# -gt 0 ]; do case $1 in -D) d=$2; shift ;; -p) [ -z "$a" ] && a=$2; shift ;; esac; shift; done
  printf '#!/bin/sh\n[ -d "$HOME" ] && echo "$*" >> "$HOME/calls" 2>/dev/null\n[ "$*" = "skills install" ] && [ -n "${FAKE_SKILLS_FAIL:-}" ] && { echo "no harness found" >&2; exit 1; }\necho fake-smithy\n' | gzip > "$d/$a"
  echo "$( (command -v sha256sum >/dev/null 2>&1 && sha256sum "$d/$a" || shasum -a 256 "$d/$a") | awk '{print $1}')  $a" > "$d/checksums.txt"
  exit 0
fi
[ "$3" = --help ] && exit 0
[ -n "$HANG_ATTEST" ] && exec sleep 30
exit 0
EOF
cp "$L/fakegh" "$L/slowatt/gh"; cp "$L/fakegh" "$L/okgh/gh"; chmod +x "$L/hang/gh" "$L/slowatt/gh" "$L/okgh/gh"
LW() { env -u SMITHY_BIN -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/H$2" SMITHY_HOME="$L/home$2" TMPDIR="$L/tmp" SMITHY_DOWNLOAD_TIMEOUT=1 HANG_ATTEST="$3" PATH="$1:$L/sys" "$L/install.sh" 0.0.0 2>"$L/err" && env -u SMITHY_BIN HOME="$L/H$2" SMITHY_HOME="$L/home$2" "$L/smithy" version; }
t "install.sh: hanging gh download times out with exit 3" 's=$(date +%s); LW "$L/hang" a ""; c=$?; [ $c -eq 3 ] && [ $(( $(date +%s) - s )) -lt 10 ] && grep -q "^smithy install: can.t download v0.0.0 (smithy-.*\.gz): timed out" "$L/err"'
t "install.sh: hanging attestation times out with exit 3" 'LW "$L/slowatt" b 1; [ $? -eq 3 ] && grep -q "^smithy install: can.t verify .*timed out" "$L/err"'
t "install.sh: install leaves no temp dir" 'rm -rf "$L/tmp"/*; [ "$(LW "$L/okgh" c "")" = fake-smithy ] && [ -z "$(ls "$L/tmp")" ]'

# launcher without gh: curl download, refused unless SMITHY_ALLOW_UNATTESTED=1
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
LN() { env -u SMITHY_BIN -u SMITHY_ALLOW_UNATTESTED -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/H$2" SMITHY_HOME="$L/home$2" TMPDIR="$L/tmp" BAD_SUM="$3" PATH="$1:$L/sys" "$L/install.sh" 0.0.0 2>"$L/err" && env -u SMITHY_BIN HOME="$L/H$2" SMITHY_HOME="$L/home$2" "$L/smithy" version; }
t "install.sh: no gh refuses an unattested install" 'LN "$L/nogh" d ""; [ $? -eq 3 ] && grep -q "can.t verify the build attestation" "$L/err" && [ ! -e "$L/Hd/.local/share/smithy/versions/0.0.0/smithy" ]'
LA() { env -u SMITHY_BIN -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/H$2" SMITHY_ALLOW_UNATTESTED=1 SMITHY_HOME="$L/home$2" TMPDIR="$L/tmp" BAD_SUM="$3" PATH="$1:$L/sys" "$L/install.sh" 0.0.0 2>"$L/err" && env -u SMITHY_BIN HOME="$L/H$2" SMITHY_HOME="$L/home$2" "$L/smithy" version; }
t "install.sh: no gh + SMITHY_ALLOW_UNATTESTED=1 installs on checksum with one warning" '[ "$(LA "$L/nogh" f "")" = fake-smithy ] && [ "$(grep -c "installing on checksum only" "$L/err")" -eq 1 ]'
t "launcher: installed binary runs silently" '[ "$(env -u SMITHY_BIN HOME="$L/Hf" SMITHY_HOME="$L/homef" "$L/smithy" version 2>"$L/err")" = fake-smithy ] && [ ! -s "$L/err" ]'
t "install.sh: no gh + bad checksum refuses" 'LN "$L/nogh-bad" e 1; [ $? -eq 3 ] && grep -q "checksum mismatch" "$L/err" && [ ! -e "$L/He/.local/share/smithy/versions/0.0.0/smithy" ]'
t "install.sh: no gh + empty checksums.txt refuses" 'LN "$L/nogh-bad" g empty; [ $? -eq 3 ] && [ ! -e "$L/Hg/.local/share/smithy/versions/0.0.0/smithy" ]'

# install.sh: one build (harness) plus the skills
mkdir -p "$L/kind"; cp "$L/fakegh" "$L/kind/gh"; printf '#!/bin/sh\nexit 0\n' > "$L/kind/npx"; chmod +x "$L/kind/gh" "$L/kind/npx"
LK() { h=$1; shift; env -u SMITHY_BIN -u SMITHY_TTY -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/kh$h" SMITHY_HOME="$L/k$h" TMPDIR="$L/tmp" PATH="$L/kind:$L/sys" "$@" 2>"$L/err"; }
t "install.sh: default installs harness and runs skills install" 'LK a "$L/install.sh" 0.0.0 </dev/null && grep -q "(smithy-[a-z]*-[a-z0-9]*\.gz)" "$L/err" && ! grep -q harness- "$L/err" && grep -qx "skills install" "$L/kha/calls" && [ ! -e "$L/ka/kind" ]'
t "install.sh: --no-skills skips skills install" 'LK b "$L/install.sh" --no-skills 0.0.0 </dev/null && grep -q "(smithy-[a-z]*-[a-z0-9]*\.gz)" "$L/err" && ! grep -q harness- "$L/err" && ! grep -q "skills install" "$L/khb/calls" 2>/dev/null && grep -q -- "--no-skills" "$L/err"'
t "install.sh: --skills is an ignored old flag" 'LK c "$L/install.sh" --skills 0.0.0 </dev/null && grep -q -- "--skills is ignored" "$L/err" && grep -q "(smithy-[a-z]*-[a-z0-9]*\.gz)" "$L/err" && ! grep -q harness- "$L/err"'
t "install.sh: no agent found still installs the binary" 'FAKE_SKILLS_FAIL=1 LK e "$L/install.sh" 0.0.0 </dev/null && grep -q "no harness found" "$L/err" && grep -q "skills install" "$L/err"'
t "install.sh: --update installs harness and the skills" 'LK d "$L/install.sh" --update 0.0.0 </dev/null && grep -q "(smithy-[a-z]*-[a-z0-9]*\.gz)" "$L/err" && ! grep -q harness- "$L/err" && grep -qx "skills install" "$L/khd/calls"'
t "install.sh: no build question on a tty" ': > "$L/ans"; LK e env SMITHY_TTY="$L/ans" "$L/install.sh" 0.0.0 && ! grep -q "which build" "$L/err"'
t "install.sh: rejects a bad version" 'env -u SMITHY_BIN HOME="$L/Hbad" SMITHY_HOME="$L/bad" PATH="$L/sys" "$L/install.sh" "../x" 2>"$L/err"; [ $? -eq 3 ] && grep -q "bad version" "$L/err"'
# standard layout: ~/.local/share/smithy/versions + ~/.local/bin/smithy, migration, uninstall
LI() { h=$1; shift; env -u SMITHY_BIN -u SMITHY_TTY -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/I$h" SMITHY_HOME="$L/I$h/.smithy" SHELL=/bin/zsh TMPDIR="$L/tmp" PATH="$L/kind:$L/sys" "$@" 2>"$L/err"; }
t "install.sh: fresh install lands in ~/.local/bin" 'LI a "$L/install.sh" 0.0.0 </dev/null && [ -L "$L/Ia/.local/bin/smithy" ] && [ "$("$L/Ia/.local/bin/smithy")" = fake-smithy ] && [ -x "$L/Ia/.local/share/smithy/versions/0.0.0/smithy" ] && [ ! -e "$L/Ia/.smithy/bin" ]'
t "install.sh: prints the exact PATH line for zsh" "grep -qF 'export PATH=\"$L/Ia/.local/bin:\$PATH\"' \"\$L/err\" && grep -q 'add this line to .*\.zshrc' \"\$L/err\""
t "install.sh: respects XDG_DATA_HOME and XDG_BIN_HOME" 'LI x env XDG_DATA_HOME="$L/Ix/d" XDG_BIN_HOME="$L/Ix/b" "$L/install.sh" 0.0.0 </dev/null && [ -x "$L/Ix/d/smithy/versions/0.0.0/smithy" ] && [ -L "$L/Ix/b/smithy" ]'
t "install.sh: tty yes adds the PATH line once and records it" 'echo y > "$L/ans"; LI y env SMITHY_TTY="$L/ans" "$L/install.sh" 0.0.0 && grep -qF "/Iy/.local/bin:" "$L/Iy/.zshrc" && [ "$(cat "$L/Iy/.smithy/path-added")" = "$L/Iy/.zshrc" ]'
t "install.sh: PATH offer defaults to no" ': > "$L/ans"; LI n env SMITHY_TTY="$L/ans" "$L/install.sh" 0.0.0 && [ ! -e "$L/In/.zshrc" ]'
t "install.sh: --update keeps working in the new layout" 'LI a "$L/install.sh" --update 0.0.0 && [ "$("$L/Ia/.local/bin/smithy")" = fake-smithy ]'
mkdir -p "$L/Im/.smithy/bin/0.0.1" "$L/Im/.smithy/bin/dev/k"; printf '#!/bin/sh\necho old-smithy\n' > "$L/Im/.smithy/bin/0.0.1/smithy"; chmod +x "$L/Im/.smithy/bin/0.0.1/smithy"; ln -s "$L/Im/.smithy/bin/0.0.1/smithy" "$L/Im/.smithy/bin/smithy"
echo harness > "$L/Im/.smithy/kind"
t "install.sh: migrates ~/.smithy/bin once and says so" 'LI m "$L/install.sh" 0.0.0 </dev/null && [ "$(grep -c "moved the old install" "$L/err")" -eq 1 ] && [ -x "$L/Im/.local/share/smithy/versions/0.0.1/smithy" ] && [ -d "$L/Im/.local/share/smithy/dev/k" ]'
t "install.sh: drops ~/.smithy/bin and ~/.smithy/kind" '[ ! -e "$L/Im/.smithy/bin" ] && [ ! -L "$L/Im/.smithy/bin" ] && [ ! -e "$L/Im/.smithy/kind" ]'
t "install.sh: a second run is silent about migration" 'LI m "$L/install.sh" 0.0.0 </dev/null && ! grep -qi "moved\|migrat\|\.smithy/bin" "$L/err"'
J="$L/Ij"; mkdir -p "$J/.smithy/bin/0.0.1" "$J/.smithy/bin/dev/k1" "$J/.smithy/bin/dev/k2" "$J/.local/share/smithy/versions/0.0.1" "$J/.local/share/smithy/dev/k2"
echo old > "$J/.smithy/bin/0.0.1/smithy"; echo new > "$J/.local/share/smithy/versions/0.0.1/smithy"; echo a > "$J/.smithy/bin/dev/k1/smithy"; echo b > "$J/.smithy/bin/dev/k2/smithy"; echo c > "$J/.local/share/smithy/dev/k2/smithy"
t "install.sh: migration keeps what already sits at the target" 'LI j "$L/install.sh" 0.0.0 </dev/null && [ "$(cat "$J/.local/share/smithy/versions/0.0.1/smithy")" = new ] && [ "$(cat "$J/.local/share/smithy/dev/k2/smithy")" = c ] && [ "$(cat "$J/.local/share/smithy/dev/k1/smithy")" = a ] && [ ! -e "$J/.smithy/bin" ]'
P="$L/Ip"; mkdir -p "$P/.local/share/smithy/versions/0.0.1" "$P/.local/share/smithy/versions/0.0.2" "$P/.local/share/smithy/versions/0.0.3" "$P/.local/bin"; ln -s "$P/.local/share/smithy/versions/0.0.2/smithy" "$P/.local/bin/smithy"
t "install.sh: keeps the current and the previous version only" 'LI p "$L/install.sh" 0.0.0 </dev/null && [ "$(ls "$P/.local/share/smithy/versions" | tr "\n" " ")" = "0.0.0 0.0.2 " ]'
t "launcher: an old 0.0.1 skill still finds its binary after migration" 'echo 0.0.1 > "$L/VERSION"; [ "$(env -u SMITHY_BIN HOME="$L/Im" SMITHY_HOME="$L/Im/.smithy" "$L/smithy" 2>"$L/err")" = old-smithy ] && [ ! -s "$L/err" ]'
echo 0.0.0 > "$L/VERSION"
t "launcher: finds the new layout" '[ "$(env -u SMITHY_BIN HOME="$L/Ia" SMITHY_HOME="$L/Ia/.smithy" "$L/smithy" 2>"$L/err")" = fake-smithy ] && [ ! -s "$L/err" ]'
t "launcher: falls back to ~/.local/bin/smithy naming smithy update" 'echo 9.9.9 > "$L/VERSION"; [ "$(env -u SMITHY_BIN HOME="$L/Ia" SMITHY_HOME="$L/Ia/.smithy" "$L/smithy" 2>"$L/err")" = fake-smithy ] && grep -q "using $L/Ia/.local/bin/smithy" "$L/err" && grep -q "smithy update 9.9.9" "$L/err"'
echo 0.0.0 > "$L/VERSION"
# a checkout's dev build never takes over the smithy command on PATH
D="$L/devco"; mkdir -p "$D/skills/x/scripts" "$D/cmd" "$D/internal" "$L/devgo"; echo "module github.com/Zovaris/smithy" > "$D/go.mod"; cp "$L/smithy" "$D/skills/x/scripts/smithy"
printf '#!/bin/sh\nwhile [ $# -gt 0 ]; do [ "$1" = -o ] && { printf "#!/bin/sh\\necho dev-smithy\\n" > "$2"; chmod +x "$2"; }; shift; done\n' > "$L/devgo/go"; chmod +x "$L/devgo/go"
t "launcher: dev build does not re-point ~/.local/bin/smithy" 'before=$(readlink "$L/Ia/.local/bin/smithy"); [ "$(env -u SMITHY_BIN HOME="$L/Ia" SMITHY_HOME="$L/Ia/.smithy" PATH="$L/devgo:$L/sys" "$D/skills/x/scripts/smithy" 2>"$L/err")" = dev-smithy ] && [ "$(readlink "$L/Ia/.local/bin/smithy")" = "$before" ]'
cp "$S/../uninstall.sh" "$L/uninstall.sh"
U() { h=$1; shift; env -u SMITHY_TTY -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/I$h" SMITHY_HOME="$L/I$h/.smithy" PATH="$L/sys" sh "$L/uninstall.sh" "$@" 2>"$L/err"; }
for h in y m; do mkdir -p "$L/I$h/.claude/skills/smelt" "$L/I$h/.claude/skills/mine"; touch "$L/I$h/.claude/skills/smelt/SKILL.md" "$L/I$h/.claude/skills/mine/SKILL.md" "$L/I$h/.smithy/auth.json" "$L/I$h/.local/bin/other"; echo "$L/I$h/.claude/skills/smelt" > "$L/I$h/.smithy/installed-skills"; done
t "uninstall.sh: non-tty without --yes lists and removes nothing" 'U y </dev/null; [ $? -eq 1 ] && grep -q "would remove" "$L/err" && grep -q "/Iy/.local/share/smithy$" "$L/err" && grep -q "PATH line in" "$L/err" && [ -L "$L/Iy/.local/bin/smithy" ] && [ -d "$L/Iy/.claude/skills/smelt" ]'
t "uninstall.sh: --yes removes what it listed, keeps ~/.smithy" 'U y --yes && [ ! -e "$L/Iy/.local/share/smithy" ] && [ ! -e "$L/Iy/.local/bin/smithy" ] && [ ! -e "$L/Iy/.claude/skills/smelt" ] && ! grep -qF "/Iy/.local/bin:" "$L/Iy/.zshrc" && grep -q "kept .*\.smithy" "$L/err"'
t "uninstall.sh: never touches what it did not install" '[ -d "$L/Iy/.claude/skills/mine" ] && [ -e "$L/Iy/.local/bin/other" ] && [ -e "$L/Iy/.smithy/auth.json" ]'
t "uninstall.sh: --purge without a terminal needs --confirm-purge" 'U m --yes --purge && [ -e "$L/Im/.smithy/auth.json" ] && [ ! -e "$L/Im/.smithy/bin" ] && grep -q "confirm-purge" "$L/err"'
t "uninstall.sh: tty purge asks twice and names the contents" 'printf "y\n" > "$L/ans"; U m --purge </dev/null; printf "purge\n" > "$L/ans"; env -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/Im" SMITHY_HOME="$L/Im/.smithy" SMITHY_TTY="$L/ans" PATH="$L/sys" sh "$L/uninstall.sh" --yes --purge 2>"$L/err"; grep -q "holds: .*auth.json" "$L/err" && [ ! -e "$L/Im/.smithy" ] && [ -d "$L/Im/.claude/skills/mine" ]'
mkdir -p "$L/Ic/.local/share/smithy/versions/0.0.0" "$L/Ic/.local/bin" "$L/Id/.local/share/smithy/versions/0.0.0" "$L/Id/.local/bin"
for h in c d; do echo bin > "$L/I$h/.local/share/smithy/versions/0.0.0/smithy"; done
echo bin > "$L/Ic/.local/bin/smithy"; echo other > "$L/Id/.local/bin/smithy"
t "uninstall.sh: removes a copied smithy binary" 'U c --yes && [ ! -e "$L/Ic/.local/bin/smithy" ]'
t "uninstall.sh: keeps a binary smithy did not install" 'U d --yes && [ -e "$L/Id/.local/bin/smithy" ]'
t "install.sh: a fresh smithy home gets the marker" '[ -e "$L/Ia/.smithy/.smithy-home" ]'
mkdir -p "$L/Iq/.smithy/my-project"; echo mine > "$L/Iq/.smithy/notes.txt"; touch "$L/Iq/.smithy/auth.json"
t "uninstall.sh: --purge refuses a folder that is not smithy's" 'U q --yes --purge --confirm-purge; [ -e "$L/Iq/.smithy/notes.txt" ] && grep -q "refusing to purge .*notes.txt" "$L/err"'
mkdir -p "$L/Ir/.smithy"; touch "$L/Ir/.smithy/.smithy-home" "$L/Ir/.smithy/whatever"
t "uninstall.sh: --purge removes a marked smithy home" 'U r --yes --purge --confirm-purge; [ ! -e "$L/Ir/.smithy" ]'
# launcher never downloads: no binary → exit 3 naming the install page
LM() { env -u SMITHY_BIN -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/emptyH$1" SMITHY_HOME="$L/empty$1" PATH="$L/curl:$L/sys" "$L/smithy" version 2>"$L/err"; }
t "launcher: missing binary exits 3 with the install line, no download" 'rm -f "$L/curl.args"; LM a; [ $? -eq 3 ] && grep -q "not installed; the smithy skills need it" "$L/err" && grep -q "install.sh | sh -s 0.0.0" "$L/err" && [ ! -e "$L/curl.args" ]'
t "launcher: ignores the old ~/.smithy/bin" 'mkdir -p "$L/emptyb/bin"; ln -sfn "$L/Hf/.local/share/smithy/versions/0.0.0/smithy" "$L/emptyb/bin/smithy"; LM b; [ $? -eq 3 ]'
t "launcher: finds smithy on PATH" 'mkdir -p "$L/onpath"; ln -sfn "$L/Hf/.local/share/smithy/versions/0.0.0/smithy" "$L/onpath/smithy"; echo 9.9.9 > "$L/VERSION"; [ "$(env -u SMITHY_BIN -u XDG_DATA_HOME -u XDG_BIN_HOME HOME="$L/emptyHc" SMITHY_HOME="$L/emptyc" PATH="$L/onpath:$L/sys" "$L/smithy" version 2>"$L/err")" = fake-smithy ] && grep -q "smithy update 9.9.9" "$L/err"'
echo 0.0.0 > "$L/VERSION"
t "skills contain no download commands" '! grep -rlE "gh release download|releases/download" "$S/../skills"'
echo "shared: $pass passed, $fail failed"; [ $fail -eq 0 ]
