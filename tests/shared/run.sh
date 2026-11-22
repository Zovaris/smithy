route --kind review --text x |#!/usr/bin/env bash
route --kind review --text x |# Offline tests for shared/ scripts. Builds throwaway repos; needs git, node, jq.
route --kind review --text x |set -uo pipefail
route --kind review --text x |S="$(cd "$(dirname "$0")/../../shared" && pwd -P)"
route --kind review --text x |P="$(cd "$(dirname "$0")/../../skills/use-smithy/scripts" && pwd -P)"
route --kind review --text x |pass=0; fail=0
route --kind review --text x |t() { if eval "$2" >/dev/null 2>&1; then pass=$((pass+1)); else fail=$((fail+1)); echo "FAIL: $1"; fi; }
route --kind review --text x |T="$(mktemp -d)"; trap 'rm -rf "$T"' EXIT
route --kind review --text x |if [ -z "${SMITHY_BIN:-}" ]; then
route --kind review --text x |  (cd "$S/.." && go build -trimpath -o "$T/smithy-bin" ./cmd/smithy) || exit 1
route --kind review --text x |  export SMITHY_BIN="$T/smithy-bin"
route --kind review --text x |fi
route --kind review --text x |# The host agent session must not leak into the tests.
route --kind review --text x |unset CLAUDECODE CLAUDE_CODE_ENTRYPOINT CODEX_THREAD_ID CODEX_CI CODEX_SANDBOX CODEX_SANDBOX_NETWORK_DISABLED OPENCODE OPENCODE_SESSION_ID GEMINI_CLI PI_CODING_AGENT PI_SESSION_ID DSH_SESSION_ID AI_AGENT CURSOR_AGENT CURSOR_TRACE_ID AGENT_CLI AGENT_CMD AGENT_MODEL AGENT_EFFORT AGENT_ARGS CONSULT_CMD CONSULT_MODEL
route --kind review --text x |export SMITHY_CONFIG="$T/config.json" WARDEN_HARNESS=codex
route --kind review --text x |
route --kind review --text x |git init -q --bare "$T/remote.git"; git clone -q "$T/remote.git" "$T/repo" 2>/dev/null; cd "$T/repo"
route --kind review --text x |git config user.email t@t; git config user.name t
route --kind review --text x |git switch -q -c main 2>/dev/null || git checkout -q -b main  # CI runners default to master
route --kind review --text x |git commit -q --allow-empty -m "feat(core): init"; git commit -q --allow-empty -m "fix(api): handle nulls"
route --kind review --text x |git push -q origin HEAD:main 2>/dev/null; git remote set-head origin main >/dev/null 2>&1
route --kind review --text x |
route --kind review --text x |# smithy names
route --kind review --text x |t "branch from ticket" '[ "$($S/smithy names branch PROJ-123 "Add login with Google")" = feat/PROJ-123-add-login-google ]'
route --kind review --text x |t "branch from issue, type from title" '[ "$($S/smithy names branch "#42" "Fix null crash")" = fix/42-null-crash ]'
route --kind review --text x |t "branch without key" '[ "$($S/smithy names branch - "Update README")" = docs/update-readme ]'
route --kind review --text x |t "branch slug capped at 40" 'b="$($S/smithy names branch 03 "a very long title that keeps going and going past forty chars")" && [ ${#b} -le 52 ]'
route --kind review --text x |t "worktree is a sibling" '[ "$($S/smithy names worktree feat/x)" = "$(cd "$T" && pwd -P)/worktrees/repo-feat-x" ]'
route --kind review --text x |git worktree add -q -b nw "$T/repo/.claude/wt-nw" HEAD 2>/dev/null
route --kind review --text x |t "worktree name from inside a worktree uses the main checkout" '[ "$(cd "$T/repo/.claude/wt-nw" && $S/smithy names worktree feat/y)" = "$(cd "$T" && pwd -P)/worktrees/repo-feat-y" ]'
route --kind review --text x |git worktree remove --force "$T/repo/.claude/wt-nw"; git branch -q -D nw; rm -rf "$T/repo/.claude"
route --kind review --text x |t "branch template setting" '$S/smithy config set git.branch "agent/{key}-{slug}" && [ "$($S/smithy names branch 7 "Add box")" = agent/7-add-box ] && $S/smithy config set git.branch "{type}/{key}-{slug}"'
route --kind review --text x |
route --kind review --text x |# smithy wait with a fake gh
route --kind review --text x |mkdir -p "$T/bin"
route --kind review --text x |cat > "$T/bin/gh" <<'GH'
route --kind review --text x |#!/usr/bin/env bash
route --kind review --text x |case "$1 $2" in
route --kind review --text x |  "auth status") exit 0 ;;
route --kind review --text x |  "pr view") cat "$T_PR" ;;
route --kind review --text x |  "run view") cat "$T_RUN" ;;
route --kind review --text x |esac
route --kind review --text x |GH
route --kind review --text x |chmod +x "$T/bin/gh"; export T_PR="$T/pr.json" T_RUN="$T/run.json"
route --kind review --text x |W() { PATH="$T/bin:$PATH" "$S/smithy" wait "$@"; }
route --kind review --text x |echo '{"number":5,"url":"u5","state":"OPEN","mergeable":"MERGEABLE","headRefOid":"abc1234","statusCheckRollup":[{"name":"unit","status":"COMPLETED","conclusion":"SUCCESS"}]}' > "$T/pr.json"
route --kind review --text x |t "pr: finished checks reported at once" '[ "$(W pr:5 --max 0)" = "pr 5 checks=green u5" ]'
route --kind review --text x |echo '{"number":5,"url":"u5","state":"OPEN","mergeable":"MERGEABLE","headRefOid":"abc1234","statusCheckRollup":[{"name":"unit","status":"COMPLETED","conclusion":"FAILURE"},{"name":"lint","status":"COMPLETED","conclusion":"SUCCESS"}]}' > "$T/pr.json"
route --kind review --text x |t "pr: red names the failed checks" '[ "$(W pr:5 --max 0)" = "pr 5 checks=red failed=unit u5" ]'
route --kind review --text x |echo '{"number":5,"url":"u5","state":"OPEN","mergeable":"MERGEABLE","headRefOid":"abc1234","statusCheckRollup":[{"name":"unit","status":"IN_PROGRESS"}]}' > "$T/pr.json"
route --kind review --text x |t "pr: pending → heartbeat" 'W pr:5 --max 0; [ $? -eq 4 ]'
route --kind review --text x |( sleep 2; echo '{"number":5,"url":"u5","state":"MERGED","mergeable":"UNKNOWN","headRefOid":"abc1234","statusCheckRollup":[{"name":"unit","status":"IN_PROGRESS"}]}' > "$T/pr.json" ) &
route --kind review --text x |t "pr: merge wakes it" '[ "$(W pr:5 --every 1 --max 1)" = "pr 5 merged u5" ]'
route --kind review --text x |wait
route --kind review --text x |echo '{"status":"completed","conclusion":"failure","name":"CI","url":"r1","jobs":[{"name":"build","conclusion":"failure"},{"name":"lint","conclusion":"success"}]}' > "$T/run.json"
route --kind review --text x |t "run: finished with failed jobs" '[ "$(W run:9 --max 0)" = "run 9 failure failed=build r1" ]'
route --kind review --text x |t "unknown target exits 2" 'W nope:1 --max 0; [ $? -eq 2 ]'
route --kind review --text x |
route --kind review --text x |# smithy tasks add / board
route --kind review --text x |export SMITHY_HOME="$T/home"
route --kind review --text x |t "add numbers the next task" '[ "$(basename "$("$S/smithy" tasks add docs/specs/b "Add login")")" = 01-add-login.md ] && [ "$(basename "$("$S/smithy" tasks add docs/specs/b "Rate limit" --blocked-by 01)")" = 02-rate-limit.md ]'
route --kind review --text x |t "added task is todo and blocked" 'grep -q "^Blocked by: 01" docs/specs/b/02-rate-limit.md && [ "$(basename "$("$S/smithy" tasks next docs/specs/b)")" = 01-add-login.md ]'
route --kind review --text x |t "board shows the list" '"$S/smithy" tasks board | grep -q "0/2 done, 0 doing, 1 ready"'
route --kind review --text x |t "board --all finds it from elsewhere" '(cd / && "$S/smithy" tasks board --all) | grep -q "repo/b"'
route --kind review --text x |rm -rf docs/specs/b
route --kind review --text x |
route --kind review --text x |# smithy commit
route --kind review --text x |t "style is conventional" '[ "$($S/smithy commit style | head -1)" = "style: conventional" ]'
route --kind review --text x |t "rejects trailing period" '! $S/smithy commit check "feat: add x."'
route --kind review --text x |t "rejects non-conventional" '! $S/smithy commit check "Added stuff"'
route --kind review --text x |t "rejects > 72 chars" '! $S/smithy commit check "feat: $(printf "a%.0s" {1..80})"'
route --kind review --text x |t "accepts good message" '$S/smithy commit check "feat(api): add pagination"'
route --kind review --text x |echo a > a.txt; echo b > b.txt
route --kind review --text x |t "requires paths" '! $S/smithy commit "feat(api): add a"'
route --kind review --text x |t "commits only given paths" '$S/smithy commit "feat(api): add a" a.txt && [ "$(git status --short)" = "?? b.txt" ]'
route --kind review --text x |
route --kind review --text x |# smithy pr
route --kind review --text x |t "no template → none" '$S/smithy pr template | grep -q "^none:"'
route --kind review --text x |mkdir -p .github; printf '## Summary\n\n## Testing\n' > .github/pull_request_template.md
route --kind review --text x |t "template found once" '[ "$($S/smithy pr template | wc -l | tr -d " ")" = 1 ]'
route --kind review --text x |
route --kind review --text x |# smithy cleanup
route --kind review --text x |git worktree add -q -b t1 "$T/wt1" main; (cd "$T/wt1" && git commit -q --allow-empty -m "feat(x): wip")
route --kind review --text x |t "keeps unpushed branch" '! $S/smithy cleanup . t1 && git show-ref -q --verify refs/heads/t1'
route --kind review --text x |git push -q origin t1 2>/dev/null
route --kind review --text x |t "keeps a branch checked out in a worktree it wasn't given" '! $S/smithy cleanup . t1 && [ -d "$T/wt1" ] && git show-ref -q --verify refs/heads/t1'
route --kind review --text x |t "never removes the folder it runs in" '! (cd "$T/wt1" && $S/smithy cleanup "$T/repo" t1 "$T/wt1") && [ -d "$T/wt1" ]'
route --kind review --text x |t "cleans pushed branch" '$S/smithy cleanup . t1 "$T/wt1" && ! git show-ref -q --verify refs/heads/t1 && [ ! -d "$T/wt1" ]'
route --kind review --text x |git worktree add -q -b t2 "$T/wt2" main; echo dirty > "$T/wt2/x"
route --kind review --text x |t "keeps dirty worktree" '! $S/smithy cleanup . t2 && [ -d "$T/wt2" ]'
route --kind review --text x |git worktree add -q -b t3 "$T/wt3" origin/main; (cd "$T/wt3" && echo r > r.txt && git add r.txt && git commit -q -m "feat(x): rebased")
route --kind review --text x |git worktree add -q --detach "$T/wt4" origin/main; (cd "$T/wt4" && git commit -q --allow-empty -m "chore: other" && git cherry-pick t3 >/dev/null && git push -q origin HEAD:main 2>/dev/null); git worktree remove --force "$T/wt4"; git fetch -q origin
route --kind review --text x |t "cleans a branch whose commits landed rebased" '$S/smithy cleanup . t3 "$T/wt3" && ! git show-ref -q --verify refs/heads/t3'
route --kind review --text x |
route --kind review --text x |# smithy tasks
route --kind review --text x |mkdir -p docs/specs/pay
route --kind review --text x |printf '# 01 · A\nStatus: todo\nBlocked by: none\n' > docs/specs/pay/01-a.md
route --kind review --text x |printf '# 02 · B\nStatus: todo\nBlocked by: 01\n' > docs/specs/pay/02-b.md
route --kind review --text x |t "next is 01" '"$S/smithy" tasks next | grep -q 01-a.md'
route --kind review --text x |t "02 blocked" '[ "$("$S/smithy" tasks ready | wc -l | tr -d " ")" = 1 ]'
route --kind review --text x |"$S/smithy" tasks set docs/specs/pay/01-a.md status=done pr=https://x/1 >/dev/null
route --kind review --text x |t "02 ready after 01 done" '"$S/smithy" tasks next | grep -q 02-b.md'
route --kind review --text x |t "rejects bad status" '! "$S/smithy" tasks set docs/specs/pay/02-b.md status=bogus'
route --kind review --text x |
route --kind review --text x |mkdir -p .docs/specs/priv elsewhere/x
route --kind review --text x |printf '# 01 · P\nStatus: todo\nBlocked by: none\n' > .docs/specs/priv/01-p.md
route --kind review --text x |printf '# 01 · E\nStatus: todo\nBlocked by: none\n' > elsewhere/x/01-e.md
route --kind review --text x |t "where finds .docs/specs" '"$S/smithy" tasks where | grep -q "/.docs/specs/priv$"'
route --kind review --text x |t "where honours SMELT_TASKS_DIRS" 'SMELT_TASKS_DIRS=elsewhere "$S/smithy" tasks where | grep -q "/elsewhere/x$"'
route --kind review --text x |
route --kind review --text x |# smithy config + smithy harness
route --kind review --text x |t "harness override" '[ "$($S/smithy harness)" = "codex any" ]'
route --kind review --text x |t "model unset → exit 3" '$S/smithy config model cheap; [ $? -eq 3 ]'
route --kind review --text x |$S/smithy config set-model cheap mini >/dev/null
route --kind review --text x |t "model saved per harness" '[ "$($S/smithy config model cheap)" = mini ] && jq -e ".models.codex.cheap" "$SMITHY_CONFIG"'
route --kind review --text x |
route --kind review --text x |(cd "$T/repo" && $S/smithy config set --repo tasks.commit false >/dev/null)
route --kind review --text x |t "repo setting overrides, false kept as false" '[ "$(cd "$T/repo" && $S/smithy config get tasks.commit)" = false ]'
route --kind review --text x |t "default applies when unset" '[ "$(cd "$T/repo" && $S/smithy config get warden.cleanup)" = auto ]'
route --kind review --text x |t "no default → exit 3" '(cd "$T/repo" && $S/smithy config get tasks.ignore); [ $? -eq 3 ]'
route --kind review --text x |t "task-dir asks for tasks.ignore" 'o="$(cd "$T/repo" && $S/smithy task-dir pay)"; [ $? -eq 3 ] && grep -q "^ask tasks.ignore" <<<"$o"'
route --kind review --text x |(cd "$T/repo" && $S/smithy config set --repo tasks.ignore exclude >/dev/null && $S/smithy task-dir pay >/dev/null && $S/smithy task-dir pay >/dev/null)
route --kind review --text x |t "exclude adds .docs/ once" '[ "$(grep -c "^.docs/$" "$(cd "$T/repo" && git rev-parse --git-path info/exclude)")" = 1 ] && [ -d "$T/repo/.docs/specs/pay" ]'
route --kind review --text x |
route --kind review --text x |# no model names in smithy: the user names each tier
route --kind review --text x |t "no suggest command" 'WARDEN_HARNESS=claude-cli $S/smithy config suggest-models 2>/dev/null; [ $? -eq 2 ]'
route --kind review --text x |WARDEN_HARNESS=claude-cli $S/smithy config set-model strong opus >/dev/null
route --kind review --text x |t "set-model saves it for that harness" '[ "$(WARDEN_HARNESS=claude-cli $S/smithy config model strong)" = opus ]'
route --kind review --text x |
route --kind review --text x |# smithy route
route --kind review --text x |$S/smithy config set-model mid midm >/dev/null
route --kind review --text x |t "router: typo is cheap" '[ "$($S/smithy route --text "fix typo in README" | cut -d" " -f1)" = cheap ]'
route --kind review --text x |t "router: auth is strong" '[ "$($S/smithy route --text "add OAuth login with tokens" | cut -d" " -f1)" = strong ]'
route --kind review --text x |t "router: plain feature is mid, model from config" '[ "$($S/smithy route --text "add CSV export to reports" | cut -d" " -f1-2)" = "mid midm" ]'
route --kind review --text x |t "router: full speed is strong" '[ "$($S/smithy route --text "add CSV export" --speed full | cut -d" " -f1)" = strong ]'
route --kind review --text x |t "router: review is cheap" '[ "$($S/smithy route --kind review | cut -d" " -f1)" = cheap ]'
route --kind review --text x |t "router: unset tier prints -" '[ "$($S/smithy route --text "add OAuth login" | cut -d" " -f2)" = - ]'
route --kind review --text x |t "router: needs input" '$S/smithy route; [ $? -eq 2 ]'
route --kind review --text x |
route --kind review --text x |# smithy consult
route --kind review --text x |t "consult builds a tool-less prompt" 'CONSULT_CMD=cat $S/smithy consult "Q1" "A; B" | grep -q "Question: Q1"'
route --kind review --text x |echo "ctx-line" > "$T/ctx.md"
route --kind review --text x |t "consult adds context" 'CONSULT_CMD=cat $S/smithy consult "Q1" "A; B" --context "$T/ctx.md" | grep -q ctx-line'
route --kind review --text x |t "consult falls back when the harness CLI is missing" 'WARDEN_HARNESS=pi AGENT_CMD="grep -c Question" PATH="/usr/bin:/bin:$(dirname "$(command -v node)")" $S/smithy consult "Q" "A; B" | grep -q 1'
route --kind review --text x |
route --kind review --text x |# use-smithy smithy setup block
route --kind review --text x |printf '# Rules\n' > "$T/AGENTS.md"
route --kind review --text x |t "block missing" '! $P/smithy setup block status "$T/AGENTS.md"'
route --kind review --text x |$P/smithy setup block add "$T/AGENTS.md" >/dev/null; $P/smithy setup block add "$T/AGENTS.md" >/dev/null
route --kind review --text x |t "block added once" '[ "$(grep -c use-smithy:start "$T/AGENTS.md")" = 1 ]'
route --kind review --text x |$P/smithy setup block remove "$T/AGENTS.md" >/dev/null
route --kind review --text x |t "block removed, rest kept" '! grep -q use-smithy "$T/AGENTS.md" && grep -q "# Rules" "$T/AGENTS.md"'
route --kind review --text x |
route --kind review --text x |# smithy agent with a fake CLI
route --kind review --text x |echo hi > "$T/p.md"
route --kind review --text x |t "smithy agent strips parent harness vars" '[ "$(CLAUDECODE=1 WARDEN_HARNESS=pi AGENT_CMD="echo \${CLAUDECODE:-none} \$WARDEN_HARNESS" $S/smithy agent "$T/p.md")" = "none pi" ]'
route --kind review --text x |t "harness ignores CODEX_HOME" '[ "$(env -i PATH=/usr/bin:/bin CODEX_HOME=/x SMITHY_BIN="$SMITHY_BIN" $S/smithy harness)" = "unknown any" ]'
route --kind review --text x |t "smithy agent pipes prompt to AGENT_CMD" '[ "$(AGENT_CMD=cat $S/smithy agent "$T/p.md")" = hi ]'
route --kind review --text x |
route --kind review --text x |# launcher download failures (copied away from the checkout so it can't build)
route --kind review --text x |L="$T/launch"; mkdir -p "$L/curl" "$L/gh"; cp "$S/smithy" "$L/smithy"; echo 0.0.0 > "$L/VERSION"
route --kind review --text x |printf '#!/bin/sh\necho "$@" > "%s/curl.args"\necho "curl: (22) The requested URL returned error: 404" >&2\nexit 22\n' "$L" > "$L/curl/curl"
route --kind review --text x |printf '#!/bin/sh\necho "release not found" >&2\nexit 1\n' > "$L/gh/gh"
route --kind review --text x |chmod +x "$L/curl/curl" "$L/gh/gh"
route --kind review --text x |mkdir -p "$L/sys"
route --kind review --text x |for p in /bin/* /usr/bin/*; do
route --kind review --text x |  n=${p##*/}; [ "$n" = gh ] || [ "$n" = curl ] || [ -e "$L/sys/$n" ] || ln -s "$p" "$L/sys/$n"
route --kind review --text x |done
route --kind review --text x |LR() { env -u SMITHY_BIN SMITHY_HOME="$L/home" PATH="$1:$L/sys" "$L/smithy" version 2>"$L/err"; }
route --kind review --text x |t "launcher: curl 404 exits 3 with one line" 'LR "$L/curl"; [ $? -eq 3 ] && [ "$(wc -l < "$L/err")" -eq 2 ] && grep -q "^smithy: can.t download v0.0.0 (smithy-.*\.gz): .*404" "$L/err" && ! grep -q awk "$L/err"'
route --kind review --text x |t "launcher: curl has timeouts" 'grep -q -- "--connect-timeout 10 --max-time 120" "$L/curl.args"'
route --kind review --text x |t "launcher: gh failure exits 3 with one line" 'LR "$L/gh"; [ $? -eq 3 ] && grep -q "^smithy: can.t download v0.0.0 (smithy-.*\.gz): release not found" "$L/err" && ! grep -q awk "$L/err"'
route --kind review --text x |
route --kind review --text x |# launcher watchdog: a hanging gh is stopped (SMITHY_DOWNLOAD_TIMEOUT shortens the wait)
route --kind review --text x |mkdir -p "$L/hang" "$L/slowatt" "$L/okgh" "$L/tmp"
route --kind review --text x |printf '#!/bin/sh\nexec sleep 30\n' > "$L/hang/gh"
route --kind review --text x |cat > "$L/fakegh" <<'EOF'
route --kind review --text x |#!/bin/sh
route --kind review --text x |if [ "$1" = release ]; then
route --kind review --text x |  d=; a=; while [ $# -gt 0 ]; do case $1 in -D) d=$2; shift ;; -p) [ -z "$a" ] && a=$2; shift ;; esac; shift; done
route --kind review --text x |  printf '#!/bin/sh\necho fake-smithy\n' | gzip > "$d/$a"
route --kind review --text x |  echo "$( (command -v sha256sum >/dev/null 2>&1 && sha256sum "$d/$a" || shasum -a 256 "$d/$a") | awk '{print $1}')  $a" > "$d/checksums.txt"
route --kind review --text x |  exit 0
route --kind review --text x |fi
route --kind review --text x |[ "$3" = --help ] && exit 0
route --kind review --text x |[ -n "$HANG_ATTEST" ] && exec sleep 30
route --kind review --text x |exit 0
route --kind review --text x |EOF
route --kind review --text x |cp "$L/fakegh" "$L/slowatt/gh"; cp "$L/fakegh" "$L/okgh/gh"; chmod +x "$L/hang/gh" "$L/slowatt/gh" "$L/okgh/gh"
route --kind review --text x |LW() { env -u SMITHY_BIN SMITHY_HOME="$L/home$2" TMPDIR="$L/tmp" SMITHY_DOWNLOAD_TIMEOUT=1 HANG_ATTEST="$3" PATH="$1:$L/sys" "$L/smithy" version 2>"$L/err"; }
route --kind review --text x |t "launcher: hanging gh download times out with exit 3" 's=$(date +%s); LW "$L/hang" a ""; c=$?; [ $c -eq 3 ] && [ $(( $(date +%s) - s )) -lt 10 ] && grep -q "^smithy: can.t download v0.0.0 (smithy-.*\.gz): timed out" "$L/err"'
route --kind review --text x |t "launcher: hanging attestation times out with exit 3" 'LW "$L/slowatt" b 1; [ $? -eq 3 ] && grep -q "^smithy: can.t verify .*timed out" "$L/err"'
route --kind review --text x |t "launcher: install leaves no temp dir" 'rm -rf "$L/tmp"/*; [ "$(LW "$L/okgh" c "")" = fake-smithy ] && [ -z "$(ls "$L/tmp")" ]'
route --kind review --text x |
route --kind review --text x |# launcher without gh: curl download, checksum only, warning once
route --kind review --text x |mkdir -p "$L/nogh" "$L/nogh-bad"
route --kind review --text x |cat > "$L/fakecurl" <<'EOF'
route --kind review --text x |#!/bin/sh
route --kind review --text x |o=; u=; while [ $# -gt 0 ]; do case $1 in -o) o=$2; shift ;; http*) u=$1 ;; esac; shift; done
route --kind review --text x |f=${u##*/}
route --kind review --text x |case $f in
route --kind review --text x |  checksums.txt)
route --kind review --text x |    d=$(dirname "$o"); a=$(ls "$d" | grep '\.gz$' | head -n 1)
route --kind review --text x |    h=$( (command -v sha256sum >/dev/null 2>&1 && sha256sum "$d/$a" || shasum -a 256 "$d/$a") | awk '{print $1}')
route --kind review --text x |    [ "$BAD_SUM" = empty ] && : > "$o" && exit 0
route --kind review --text x |    [ -n "$BAD_SUM" ] && h=0000000000000000000000000000000000000000000000000000000000000000
route --kind review --text x |    echo "$h  $a" > "$o" ;;
route --kind review --text x |  *) printf '#!/bin/sh\necho fake-smithy\n' | gzip > "$o" ;;
route --kind review --text x |esac
route --kind review --text x |EOF
route --kind review --text x |cp "$L/fakecurl" "$L/nogh/curl"; cp "$L/fakecurl" "$L/nogh-bad/curl"; chmod +x "$L/nogh/curl" "$L/nogh-bad/curl"
route --kind review --text x |LN() { env -u SMITHY_BIN -u SMITHY_ALLOW_UNATTESTED SMITHY_HOME="$L/home$2" TMPDIR="$L/tmp" BAD_SUM="$3" PATH="$1:$L/sys" "$L/smithy" version 2>"$L/err"; }
route --kind review --text x |t "launcher: no gh installs on checksum with one warning" '[ "$(LN "$L/nogh" d "")" = fake-smithy ] && [ "$(grep -c "installed with checksum only: install the GitHub CLI to verify the build.s origin" "$L/err")" -eq 1 ]'
route --kind review --text x |t "launcher: no gh, second run is silent" '[ "$(LN "$L/nogh" d "")" = fake-smithy ] && ! grep -q "checksum only" "$L/err"'
route --kind review --text x |t "launcher: no gh + bad checksum refuses" 'LN "$L/nogh-bad" e 1; [ $? -eq 3 ] && grep -q "checksum mismatch" "$L/err" && [ ! -e "$L/home"e/bin/0.0.0/smithy ]'
route --kind review --text x |t "launcher: no gh + empty checksums.txt refuses" 'LN "$L/nogh-bad" g empty; [ $? -eq 3 ] && [ ! -e "$L/home"g/bin/0.0.0/smithy ]'
route --kind review --text x |
route --kind review --text x |echo "shared: $pass passed, $fail failed"; [ $fail -eq 0 ]
