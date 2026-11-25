#!/bin/sh
# Downloads ripgrep for every release target, checks each archive against the sha256 pinned
# below and writes dist/smithy-rg-<os>-<arch>(.exe, else .gz) plus dist/ripgrep-LICENSE.txt
# (ripgrep is MIT or Unlicense). The release workflow runs it; bump version and sums together.
set -eu
version=14.1.1
out=${1:-dist}
base=https://github.com/BurntSushi/ripgrep/releases/download/$version
tmp=$(mktemp -d "${TMPDIR:-/tmp}/rg.XXXXXX"); trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"
while read -r target triple ext sum; do
  os=${target%/*}; arch=${target#*/}
  name=ripgrep-$version-$triple
  curl -fsSL --proto "=https" --retry 3 -o "$tmp/$name.$ext" "$base/$name.$ext"
  got=$( (command -v sha256sum >/dev/null 2>&1 && sha256sum "$tmp/$name.$ext" || shasum -a 256 "$tmp/$name.$ext") | awk '{print $1}')
  [ "$got" = "$sum" ] || { echo "fetch-ripgrep: checksum mismatch for $name.$ext" >&2; exit 1; }
  case $ext in
    zip) unzip -q -o "$tmp/$name.$ext" -d "$tmp" ;;
    *) tar -xzf "$tmp/$name.$ext" -C "$tmp" ;;
  esac
  if [ "$os" = windows ]; then
    cp "$tmp/$name/rg.exe" "$out/smithy-rg-$os-$arch.exe"
  else
    gzip -9 -c "$tmp/$name/rg" > "$out/smithy-rg-$os-$arch.gz"
  fi
  if [ ! -f "$out/ripgrep-LICENSE.txt" ]; then
    { echo "ripgrep $version, https://github.com/BurntSushi/ripgrep, dual-licensed MIT or Unlicense."
      echo; cat "$tmp/$name/LICENSE-MIT"; echo; cat "$tmp/$name/UNLICENSE"; } > "$out/ripgrep-LICENSE.txt"
  fi
done <<'TARGETS'
darwin/arm64 aarch64-apple-darwin tar.gz 24ad76777745fbff131c8fbc466742b011f925bfa4fffa2ded6def23b5b937be
darwin/amd64 x86_64-apple-darwin tar.gz fc87e78f7cb3fea12d69072e7ef3b21509754717b746368fd40d88963630e2b3
linux/arm64 aarch64-unknown-linux-gnu tar.gz c827481c4ff4ea10c9dc7a4022c8de5db34a5737cb74484d62eb94a95841ab2f
linux/amd64 x86_64-unknown-linux-musl tar.gz 4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e
windows/amd64 x86_64-pc-windows-msvc zip d0f534024c42afd6cb4d38907c25cd2b249b79bbe6cc1dbee8e3e37c2b6e25a1
TARGETS
