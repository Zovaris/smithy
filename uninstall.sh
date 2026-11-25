#!/bin/sh
# Removes smithy. Run it yourself:
#   curl -fsSL https://raw.githubusercontent.com/Zovaris/smithy/main/uninstall.sh | sh
#   ... | sh -s -- --yes                       # no question (needed without a terminal)
#   ... | sh -s -- --purge                     # also ~/.smithy (keys, db, plans), asked twice
#   ... | sh -s -- --yes --purge --confirm-purge   # the same without a terminal
# It first lists what it would remove, with sizes: the installed versions and the smithy command
# (~/.local/share/smithy, ~/.local/bin/smithy, the old ~/.smithy/bin), the skill folders
# `smithy skills install` wrote (listed in ~/.smithy/installed-skills), a running daemon, smithy-*
# container images and the smithy-internal/smithy-egress networks, and the PATH line install.sh
# added. ~/.smithy stays unless --purge. Nothing outside these paths is touched.
set -eu
home=${SMITHY_HOME:-$HOME/.smithy}
case ${XDG_DATA_HOME:-} in /*) data=$XDG_DATA_HOME/smithy ;; *) data=$HOME/.local/share/smithy ;; esac
case ${XDG_BIN_HOME:-} in /*) bindir=$XDG_BIN_HOME ;; *) bindir=$HOME/.local/bin ;; esac
known=" use-smithy smelt warden ledger scout tend hush simple "
marker='# smithy: run `smithy` from any terminal'
yes=; purge=; confirm=
for a in "$@"; do
  case $a in
    --yes|-y) yes=1 ;; --purge) purge=1 ;; --confirm-purge) confirm=1 ;;
    *) echo "smithy uninstall: unknown argument $a (--yes, --purge, --confirm-purge)" >&2; exit 2 ;;
  esac
done
say() { echo "smithy uninstall: $*" >&2; }
size() { du -sk "$1" 2>/dev/null | awk '{ k=$1; if (k >= 1048576) printf "%.1fG", k/1048576; else if (k >= 1024) printf "%.1fM", k/1024; else printf "%dK", k }'; }
tty=${SMITHY_TTY:-/dev/tty}
ontty() { { [ -n "${SMITHY_TTY:-}" ] || [ -t 2 ]; } && { : < "$tty"; } 2>/dev/null; }
inside() { case $1 in "$data"/*|"$home"/bin/*) return 0 ;; esac; return 1; }

paths=
add() { paths="$paths
$1"; printf '  %-8s %s\n' "$(size "$1")" "$1" >&2; }
echo "smithy uninstall: would remove" >&2
[ -d "$data" ] && add "$data"
if [ -L "$bindir/smithy" ]; then
  t=$(readlink "$bindir/smithy"); inside "$t" && add "$bindir/smithy"
elif [ -f "$bindir/smithy" ]; then
  # a copy, not a link: smithy's own only when it matches an installed version byte for byte
  for v in "$data"/versions/*/smithy; do
    [ -f "$v" ] && cmp -s "$v" "$bindir/smithy" && { add "$bindir/smithy"; break; }
  done
fi
# Windows (Git Bash): install.sh copies smithy.exe into %LOCALAPPDATA%\Programs\smithy, a folder of its own
case $(uname -s 2>/dev/null) in MINGW*|MSYS*|CYGWIN*)
  lad=${LOCALAPPDATA:-$HOME/AppData/Local}; command -v cygpath >/dev/null 2>&1 && lad=$(cygpath -u "$lad")
  [ -d "$lad/smithy/versions" ] && add "$lad/smithy"
  [ -f "$lad/Programs/smithy/smithy.exe" ] && add "$lad/Programs/smithy" ;;
esac
[ -d "$home/bin" ] || [ -L "$home/bin" ] && add "$home/bin"
skills=
if [ -f "$home/installed-skills" ]; then
  while IFS= read -r d; do
    case $d in "$HOME"/.*/skills/*) ;; *) continue ;; esac
    case $d in */../*|*/./*) continue ;; esac
    case $known in *" ${d##*/} "*) ;; *) continue ;; esac
    [ "$(basename "$(dirname "$d")")" = skills ] || continue
    [ -d "$d" ] && [ ! -L "$d" ] && [ -f "$d/SKILL.md" ] && { add "$d"; skills=1; }
  done < "$home/installed-skills"
fi
pid=
if [ -f "$home/daemon.json" ]; then
  pid=$(sed -n 's/.*"pid": *\([0-9][0-9]*\).*/\1/p' "$home/daemon.json" | head -n 1)
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null && ps -p "$pid" -o command= 2>/dev/null | grep -q 'smithy.* daemon'; then echo "  daemon   pid $pid (stopped)" >&2; else pid=; fi
fi
engine=; images=; nets=
for e in docker podman; do command -v $e >/dev/null 2>&1 && { engine=$e; break; }; done
if [ -n "$engine" ]; then
  images=$($engine image ls --format '{{.Repository}}:{{.Tag}}' 2>/dev/null | grep '^smithy-' || true)
  for i in $images; do echo "  image    $i" >&2; done
  for n in smithy-internal smithy-egress; do
    $engine network inspect "$n" >/dev/null 2>&1 && { nets="$nets $n"; echo "  network  $n" >&2; }
  done
fi
rcs=
if [ -f "$home/path-added" ]; then
  while IFS= read -r rc; do
    [ -f "$rc" ] && grep -qxF "$marker" "$rc" && { rcs="$rcs
$rc"; echo "  PATH line in $rc" >&2; }
  done < "$home/path-added"
fi
case $home in "$HOME"|"$HOME"/|/) [ -z "$purge" ] || say "refusing to purge $home"; purge= ;; esac
# ownhome: $home is smithy's when it holds the marker, or smithy's key files and nothing else
ownhome() {
  [ -e "$home/.smithy-home" ] && return 0
  key=; odd=
  for e in "$home"/* "$home"/.[!.]* "$home"/..?*; do
    [ -e "$e" ] || [ -L "$e" ] || continue
    n=${e##*/}
    case $n in auth.json|config.json|kind|installed-skills|smithy.db) key=1 ;; esac
    case $n in auth.json|config.json|kind|installed-skills|path-added|accounts|pool.json|pool-state.json|bin|plans|warden|decisions|connect.json|notify.json|tasks-index.json|telegram.offset|tui.seen|checkout|tend|loose|auto|skills|smithy.db*|events*|daemon*|decide*|proxy*) ;;
      *) odd="$odd $n" ;;
    esac
  done
  [ -n "$odd" ] && { why="it holds files smithy did not write:$odd"; return 1; }
  [ -n "$key" ] || { why="it holds none of smithy's files"; return 1; }
  return 0
}
if [ -n "$purge" ] && [ -d "$home" ] && ! ownhome; then
  say "refusing to purge $home: not smithy's home ($why); remove it yourself if you mean to"; purge=
fi
if [ -n "$purge" ] && [ -d "$home" ]; then
  add "$home"
  echo "           holds: $(ls -A "$home" | tr '\n' ' ')" >&2
fi
if [ -z "$paths$pid$images$nets$rcs" ]; then say "nothing to remove"; exit 0; fi

if [ -z "$yes" ]; then
  ontty || { say "no terminal to ask; nothing removed. Re-run with --yes"; exit 1; }
  printf 'smithy uninstall: remove all of the above? [y/N] ' >&2
  ans=; read -r ans < "$tty" || true
  case $ans in y|Y|yes) ;; *) say "nothing removed"; exit 1 ;; esac
fi
if [ -n "$purge" ] && [ -d "$home" ]; then
  if [ -n "$confirm" ]; then :
  elif ontty; then
    printf 'smithy uninstall: %s holds your keys (auth.json), the db and plans; type purge to delete it: ' "$home" >&2
    ans=; read -r ans < "$tty" || true
    [ "$ans" = purge ] || { say "keeping $home"; purge=; }
  else
    say "keeping $home: --purge without a terminal also needs --confirm-purge"; purge=
  fi
fi

[ -n "$pid" ] && { kill "$pid" 2>/dev/null || true; say "stopped the daemon (pid $pid)"; }
for i in $images; do $engine image rm "$i" >/dev/null 2>&1 && say "removed image $i" || say "could not remove image $i"; done
for n in $nets; do $engine network rm "$n" >/dev/null 2>&1 && say "removed network $n" || say "could not remove network $n"; done
rcok=1
oifs=$IFS; IFS='
'
for rc in $rcs; do
  if awk -v m="$marker" 'skip { skip=0; if ($0 ~ /^(export PATH=|fish_add_path )/) next } $0 == m { skip=1; next } { print }' "$rc" > "$rc.smithy-tmp" && cat "$rc.smithy-tmp" > "$rc"; then
    say "removed the PATH line from $rc"
  else
    rcok=; say "could not edit $rc"
  fi
  rm -f "$rc.smithy-tmp"
done
IFS=$oifs
[ -z "$rcs" ] || [ -z "$rcok" ] || rm -f "$home/path-added"
[ -z "$skills" ] || rm -f "$home/installed-skills"
IFS='
'
for p in $paths; do
  [ "$p" = "$home" ] && [ -z "$purge" ] && continue
  rm -rf "$p" && say "removed $p"
done
IFS=$oifs
[ -n "$purge" ] || say "kept $home (keys, db, plans); --purge removes it"
