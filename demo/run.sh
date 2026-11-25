#!/usr/bin/env bash
# Records the v0.23 demo: a VHS tape of the TUI and Playwright screenshots of the web, both on a
# throwaway SMITHY_HOME and `smithy daemon --demo` (a scripted agent: no CLI, key or network).
# Needs a harness build with the web UI, vhs, and Playwright's chromium (npx playwright install chromium in web/).
# DEMO_CHROME=<browser path> uses an installed browser instead of Playwright's chromium.
# Usage: demo/run.sh [out-dir]   (default demo/out)
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$(mkdir -p "${1:-$root/demo/out}" && cd "${1:-$root/demo/out}" && pwd)"
work="$(mktemp -d)"
trap '"$work/bin/smithy" stop >/dev/null 2>&1 || true; rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/home" "$work/repo"
go build -tags harness -o "$work/bin/smithy" "$root/cmd/smithy"
# A stand-in claude on PATH so first-run detection finds one agent ready; --demo never runs it.
printf '#!/bin/sh\necho "claude 2.0.0 (demo stand-in)"\n' >"$work/bin/claude"
chmod +x "$work/bin/claude"
git -C "$work/repo" init -q && printf 'package main\n' >"$work/repo/main.go"
# The stub the demo agent fills in, committed so the session's Changes show the diff.
printf 'package main\n' >"$work/repo/health.go"
git -C "$work/repo" add main.go health.go && git -C "$work/repo" -c user.name=demo -c user.email=demo@example.com commit -qm init
# Only the stand-in agent is on the demo's PATH, so the host's own agents never show.
demopath="$work/bin:/usr/bin:/bin"
export SMITHY_HOME="$work/home" TERM=xterm-256color
smithy() { PATH="$demopath" "$work/bin/smithy" "$@"; }
port="${DEMO_PORT:-7599}"

# TUI: first run, a session, tool calls, a decision answered. smithy finds the demo daemon.
smithy daemon --bg --demo --port "$port" --idle-exit 10m >/dev/null
sed -e "s|@OUT@|$out|g" -e "s|@PATH@|$demopath|g" "$root/demo/tui.tape" >"$work/tui.tape"
(cd "$work/repo" && vhs "$work/tui.tape")
smithy stop >/dev/null

# Web: same flow on a fresh store, then the phone width.
rm -rf "$SMITHY_HOME" && mkdir -p "$SMITHY_HOME"
smithy daemon --bg --demo --port "$port" --idle-exit 10m >/dev/null
url="$(smithy web </dev/null)"
(cd "$root/web" && node scripts/demo-shots.mjs "$url" "$work/repo" "$out")
echo "demo: $out"
