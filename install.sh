#!/bin/sh
# Install a published orchard binary. No root access or Go toolchain required.
set -eu
REPO=${ORCHARD_REPO:-ob1rao/orchard}
VERSION=${ORCHARD_VERSION:-latest}
DEST=${ORCHARD_INSTALL_DIR:-"$HOME/.local/bin"}
fail() { printf '%s\n' "orchard: $*" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || fail 'curl is required'
command -v tar >/dev/null 2>&1 || fail 'tar is required'
case "$(uname -s)" in
 Linux) os=linux ;;
 Darwin) os=darwin ;;
 *) fail 'supported systems: Linux, macOS, Raspberry Pi OS' ;;
esac
case "$(uname -m)" in
 x86_64|amd64) arch=amd64 ;;
 aarch64|arm64) arch=arm64 ;;
 armv7l|armv8l) arch=armv7 ;;
 armv6l) arch=armv6 ;;
 *) fail "unsupported architecture: $(uname -m)" ;;
esac
# A 32-bit Pi userland can run under a 64-bit kernel.
if [ "$os" = linux ] && [ "$arch" = arm64 ] && command -v getconf >/dev/null 2>&1; then
 if [ "$(getconf LONG_BIT)" = 32 ]; then arch=armv7; fi
fi
case "$os/$arch" in darwin/armv6|darwin/armv7) fail 'unsupported macOS architecture' ;; esac
asset="orchard_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
staged=''
cleanup() { rm -rf "$tmp"; if [ -n "$staged" ]; then rm -f "$staged"; fi; }
trap cleanup EXIT HUP INT TERM
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
 if [ "$VERSION" = latest ]; then
  gh release download --repo "$REPO" --pattern "$asset" --pattern checksums.txt --dir "$tmp" || fail 'release download failed; ensure repository access and a published release'
 else
  gh release download "$VERSION" --repo "$REPO" --pattern "$asset" --pattern checksums.txt --dir "$tmp" || fail 'release download failed'
 fi
else
 if [ "$VERSION" = latest ]; then base="https://github.com/$REPO/releases/latest/download"; else base="https://github.com/$REPO/releases/download/$VERSION"; fi
 curl --proto '=https' --tlsv1.2 -fsSL "$base/$asset" -o "$tmp/$asset" || fail 'download failed; private repositories require an authenticated GitHub CLI (gh auth login)'
 curl --proto '=https' --tlsv1.2 -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || fail 'checksum download failed'
fi
awk -v asset="$asset" '$2 == asset {print}' "$tmp/checksums.txt" > "$tmp/selected.sha256"
[ "$(wc -l < "$tmp/selected.sha256" | tr -d ' ')" = 1 ] || fail 'missing or ambiguous checksum'
if command -v sha256sum >/dev/null 2>&1; then
 (cd "$tmp" && sha256sum -c selected.sha256) || fail 'checksum mismatch'
elif command -v shasum >/dev/null 2>&1; then
 (cd "$tmp" && shasum -a 256 -c selected.sha256) || fail 'checksum mismatch'
else
 fail 'sha256sum or shasum is required'
fi
tar -xzf "$tmp/$asset" -C "$tmp" orchard
[ -f "$tmp/orchard" ] && [ ! -L "$tmp/orchard" ] || fail 'archive does not contain a regular binary'
mkdir -p "$DEST"
staged=$(mktemp "$DEST/.orchard.XXXXXX")
cp "$tmp/orchard" "$staged"
chmod 755 "$staged"
mv -f "$staged" "$DEST/orchard"
staged=''
printf '\nInstalled %s\n' "$DEST/orchard"
case ":$PATH:" in *":$DEST:"*) ;; *) printf 'Add to your shell profile: export PATH="%s:$PATH"\n' "$DEST" ;; esac
printf 'Run orchard to select a disk, or orchard /path/to/folder.\n'
