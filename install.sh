#!/bin/sh
# Installs the smithy binary that the smithy skills run. Run it yourself, once per version:
#   curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh            # latest
#   curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s 0.14.1   # pinned
#   curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/install.sh | sh -s -- --update  # same, as an update
#   ... | sh -s -- --no-skills   # binary only, skip `smithy skills install`
# Installs the one shipped build (smithy-harness-*), then runs `smithy skills install`, which copies the
# skills into every agent it finds. --harness and --skills are accepted and ignored (older scripts).
# Downloads smithy-harness-<os>-<arch> from the GitHub release, checks it against the release's
# checksums.txt and its build attestation (gh), and puts it in ~/.local/share/smithy/versions/<version>/
# ($XDG_DATA_HOME), with ~/.local/bin/smithy ($XDG_BIN_HOME) linking to it; on Windows
# %LOCALAPPDATA%\smithy\versions and %LOCALAPPDATA%\Programs\smithy. An old ~/.smithy/bin install
# moves there and keeps links for one release. ~/.smithy keeps data and config. Without gh (or a gh too old for `gh attestation`) nothing
# is installed unless SMITHY_ALLOW_UNATTESTED=1 (checksum only). The skills never download it.
set -eu
repo=Zovaris/smithy
home=${SMITHY_HOME:-$HOME/.smithy}
case ${XDG_DATA_HOME:-} in /*) data=$XDG_DATA_HOME/smithy ;; *) data=$HOME/.local/share/smithy ;; esac
case ${XDG_BIN_HOME:-} in /*) bindir=$XDG_BIN_HOME ;; *) bindir=$HOME/.local/bin ;; esac
limit=${SMITHY_DOWNLOAD_TIMEOUT:-120}
noskills=; version=
for a in "$@"; do
  case $a in
    --update) ;;
    --no-skills) noskills=1 ;;
    --harness|--skills) echo "smithy install: $a is ignored; smithy ships one build (harness) with the skills" >&2 ;;
    *) version=$a ;;
  esac
done
version=${version:-${SMITHY_VERSION:-}}
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
pre=smithy-harness
if [ "$os" = windows ]; then
  lad=${LOCALAPPDATA:-$HOME/AppData/Local}; command -v cygpath >/dev/null 2>&1 && lad=$(cygpath -u "$lad")
  data="$lad/smithy"; bindir="$lad/Programs/smithy"
fi
vdir="$data/versions"
if [ "$os" = windows ]; then asset="$pre-windows-amd64.exe"; exe=smithy.exe; else asset="$pre-$os-$arch.gz"; exe=smithy; fi

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
# Migration: an old ~/.smithy/bin/<version> moves to $vdir and leaves a link behind, so the skills'
# launchers and PATH entries that point there keep working for one release.
legacy=
if [ -d "$home/bin" ] && [ ! -L "$home/bin" ]; then
  for d in "$home"/bin/*/; do
    d=${d%/}; n=${d##*/}
    case $n in dev|"*") continue ;; esac
    [ -d "$d" ] && [ ! -L "$d" ] || continue
    mkdir -p "$vdir"
    # never delete: a version already at the target keeps both copies (the old one stays put)
    if [ ! -e "$vdir/$n" ]; then mv "$d" "$vdir/$n" && ln -sfn "$vdir/$n" "$d"; fi
    legacy=1
  done
  if [ -d "$home/bin/dev" ] && [ ! -L "$home/bin/dev" ]; then
    mkdir -p "$data/dev"
    for d in "$home"/bin/dev/*/; do
      d=${d%/}; [ -d "$d" ] || continue
      [ -e "$data/dev/${d##*/}" ] || mv "$d" "$data/dev/${d##*/}"
    done
    rmdir "$home/bin/dev" 2>/dev/null || true; legacy=1
  fi
  [ -e "$home/bin/$exe" ] || [ -L "$home/bin/$exe" ] && legacy=1
fi
mkdir -p "$vdir/$version" "$bindir"; mv "$src" "$vdir/$version/$exe"
if [ "$os" = windows ]; then cp -f "$vdir/$version/$exe" "$bindir/$exe"; else ln -sfn "$vdir/$version/$exe" "$bindir/$exe"; fi
if [ -n "$legacy" ]; then
  { [ -L "$home/bin/$version" ] || [ ! -e "$home/bin/$version" ]; } && { rm -f "$home/bin/$version"; ln -sfn "$vdir/$version" "$home/bin/$version"; }
  if [ "$os" = windows ]; then cp -f "$vdir/$version/$exe" "$home/bin/$exe"; else rm -f "$home/bin/$exe"; ln -s "$bindir/$exe" "$home/bin/$exe"; fi
  say "moved the old install from $home/bin to $vdir; $home/bin keeps links to it for one release, so older skills and PATH entries keep working"
fi
rgx=; [ "$os" = windows ] && rgx=.exe
rgasset="smithy-rg-$os-$arch${rgx:-.gz}"
rgok=
if have_gh; then
  watch gh release download "v$version" -R "$repo" -p "$rgasset" -D "$tmp" >/dev/null 2>&1 && rgok=1
else
  fetch -o "$tmp/$rgasset" "$url/$rgasset" 2>/dev/null && rgok=1
fi
if [ -n "$rgok" ] && [ -s "$tmp/$rgasset" ]; then
  want=$(awk -v a="$rgasset" '$2 == a || $2 == "*"a {print $1}' "$tmp/checksums.txt")
  got=$( (command -v sha256sum >/dev/null 2>&1 && sha256sum "$tmp/$rgasset" || shasum -a 256 "$tmp/$rgasset") | awk '{print $1}')
  if [ -z "$want" ] || [ "$want" != "$got" ]; then
    say "ripgrep checksum mismatch, not installed"
  elif have_gh && gh attestation verify --help >/dev/null 2>&1 && ! watch gh attestation verify "$tmp/$rgasset" -R "$repo" >/dev/null 2>&1; then
    say "ripgrep has no valid build attestation, not installed"
  else
    case $rgasset in *.gz) gunzip -f "$tmp/$rgasset"; rgsrc="$tmp/${rgasset%.gz}" ;; *) rgsrc="$tmp/$rgasset" ;; esac
    chmod +x "$rgsrc"; mv "$rgsrc" "$vdir/$version/rg$rgx"
  fi
else
  say "this release ships no ripgrep for $os/$arch; smithy search --code uses git ls-files or rg on PATH"
fi
[ -e "$home" ] || { mkdir -p "$home" && : > "$home/.smithy-home"; }  # the marker lets uninstall --purge know the folder is smithy's
say "installed v$version to $vdir/$version/$exe, command $bindir/$exe"
tty=${SMITHY_TTY:-/dev/tty}
ontty() { { [ -n "${SMITHY_TTY:-}" ] || [ -t 2 ]; } && { : < "$tty"; } 2>/dev/null; }
case ":$PATH:" in
  *":$bindir:"*) ;;
  *)
    if [ "$os" = windows ]; then
      say "$bindir is not on PATH; add it to your user PATH (System Properties > Environment Variables)"
    else
      case ${SHELL##*/} in
        zsh) rc=$HOME/.zshrc; line="export PATH=\"$bindir:\$PATH\"" ;;
        bash) rc=$HOME/.bashrc; [ "$os" = darwin ] && rc=$HOME/.bash_profile; line="export PATH=\"$bindir:\$PATH\"" ;;
        fish) rc=${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish; line="fish_add_path $bindir" ;;
        *) rc=$HOME/.profile; line="export PATH=\"$bindir:\$PATH\"" ;;
      esac
      say "$bindir is not on PATH; add this line to $rc:"
      echo "  $line" >&2
      if ontty && ! grep -qF "$line" "$rc" 2>/dev/null; then
        printf 'smithy install: add it to %s now? [y/N] ' "$rc" >&2
        ans=; read -r ans < "$tty" || true
        case $ans in
          y|Y|yes)
            mkdir -p "$(dirname "$rc")"
            printf '\n%s\n%s\n' '# smithy: run `smithy` from any terminal' "$line" >> "$rc"
            grep -qxF "$rc" "$home/path-added" 2>/dev/null || echo "$rc" >> "$home/path-added"
            say "added to $rc; open a new terminal" ;;
        esac
      fi
    fi ;;
esac
if [ -n "$noskills" ]; then
  say "skills not installed (--no-skills); install them with: $bindir/$exe skills install"
else
  say "installing the skills into every agent found (smithy skills install)"
  "$vdir/$version/$exe" skills install >&2 || die "smithy skills install failed"
fi
