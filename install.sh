#!/bin/sh
# Installs the smithy binary that the smithy skills run. Run it yourself, once per version:
#   curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh            # latest
#   curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s 0.14.1   # pinned
# Downloads smithy-<os>-<arch> from the GitHub release, checks it against the release's
# checksums.txt and its build attestation (gh), and puts it in ~/.smithy/bin/<version>/, with
# ~/.smithy/bin/smithy pointing at it. Without gh (or a gh too old for `gh attestation`) nothing
# is installed unless SMITHY_ALLOW_UNATTESTED=1 (checksum only). The skills never download it.
set -eu
repo=Zovaris/smithy
home=${SMITHY_HOME:-$HOME/.smithy}
limit=${SMITHY_DOWNLOAD_TIMEOUT:-120}
version=${1:-${SMITHY_VERSION:-}}
version=${version#v}

say() { echo "smithy install: $*" >&2; }
die() { say "$*"; exit 3; }

case "$(uname -s)" in
  Darwin) os=darwin ;; Linux) os=linux ;; MINGW*|MSYS*|CYGWIN*) os=windows ;;
  *) die "unsupported OS $(uname -s)" ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;; x86_64|amd64) arch=amd64 ;; *) die "unsupported CPU $(uname -m)" ;;
esac
if [ "$os" = windows ]; then asset="smithy-windows-amd64.exe"; exe=smithy.exe; else asset="smithy-$os-$arch.gz"; exe=smithy; fi

case $version in ""|*[!0-9A-Za-z.-]*|.*) [ -z "$version" ] || die "bad version: $version" ;; esac

tmp=$(mktemp -d "${TMPDIR:-/tmp}/smithy.XXXXXX"); trap 'rm -rf "$tmp"' EXIT

# watch runs a command with a watchdog: after $limit seconds it is killed and watch returns 124.
watch() {
  "$@" & pid=$!
  ( sleep "$limit"; : > "$tmp/timedout"; kill "$pid" ) >/dev/null 2>&1 & dog=$!
  rc=0; wait "$pid" || rc=$?
  kill "$dog" 2>/dev/null || true; wait "$dog" 2>/dev/null || true
  [ -f "$tmp/timedout" ] && { rm -f "$tmp/timedout"; return 124; }
  return $rc
}
fail() { why=$(tr '\n' ' ' < "$tmp/err" 2>/dev/null | sed 's/  */ /g; s/ $//'); die "can't download v$version ($asset): ${why:-unknown error}"; }
fetch() { curl -fsSL --proto "=https" --proto-redir "=https" --connect-timeout 10 --max-time "$limit" "$@"; }
have_gh() { command -v gh >/dev/null 2>&1; }

if [ -z "$version" ]; then
  if have_gh; then version=$(gh release view -R "$repo" --json tagName --jq .tagName 2>/dev/null || true); fi
  [ -n "$version" ] || version=$(fetch -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest" 2>/dev/null | sed 's|.*/tag/||' || true)
  version=${version#v}
  [ -n "$version" ] || die "can't find the latest release; pass a version: sh -s 0.14.1"
fi

say "downloading v$version ($asset)"
if have_gh; then
  rc=0; watch gh release download "v$version" -R "$repo" -p "$asset" -p checksums.txt -D "$tmp" >/dev/null 2>"$tmp/err" || rc=$?
  case $rc in 0) ;; 124) echo "timed out after ${limit}s" > "$tmp/err"; fail ;; *) fail ;; esac
else
  url="https://github.com/$repo/releases/download/v$version"
  for f in "$asset" checksums.txt; do fetch -o "$tmp/$f" "$url/$f" 2>"$tmp/err" || fail; done
fi
[ -s "$tmp/$asset" ] && [ -s "$tmp/checksums.txt" ] || { echo "release is missing $asset or checksums.txt" > "$tmp/err"; fail; }
want=$(awk -v a="$asset" '$2 == a || $2 == "*"a {print $1}' "$tmp/checksums.txt")
got=$( (command -v sha256sum >/dev/null 2>&1 && sha256sum "$tmp/$asset" || shasum -a 256 "$tmp/$asset") | awk '{print $1}')
[ -n "$want" ] && [ "$want" = "$got" ] || die "checksum mismatch for $asset, not installed"
if have_gh && gh attestation verify --help >/dev/null 2>&1; then
  rc=0; watch gh attestation verify "$tmp/$asset" -R "$repo" >/dev/null 2>&1 || rc=$?
  case $rc in
    0) ;;
    124) die "can't verify the build attestation of $asset: timed out after ${limit}s; not installed" ;;
    *) die "$asset has no valid build attestation for $repo (or gh is offline or signed out: gh auth status), not installed" ;;
  esac
elif [ "${SMITHY_ALLOW_UNATTESTED:-}" = 1 ]; then
  say "build attestation not checked; installing on checksum only (SMITHY_ALLOW_UNATTESTED=1)"
else
  die "can't verify the build attestation of $asset: install or update gh (https://cli.github.com) and sign in, or set SMITHY_ALLOW_UNATTESTED=1 to accept a checksum-only install; not installed"
fi

case $asset in *.gz) gunzip -f "$tmp/$asset"; src="$tmp/${asset%.gz}" ;; *) src="$tmp/$asset" ;; esac
chmod +x "$src"
mkdir -p "$home/bin/$version"; mv "$src" "$home/bin/$version/$exe"
if [ "$os" = windows ]; then cp -f "$home/bin/$version/$exe" "$home/bin/$exe"; else ln -sfn "$home/bin/$version/$exe" "$home/bin/$exe"; fi
say "installed v$version to $home/bin/$version/$exe (add $home/bin to PATH to run smithy yourself)"
