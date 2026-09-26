#!/bin/sh
# Build this checkout and install it where install.sh would, so `orchard`
# on your PATH is the working tree rather than a published release.
#
# Unlike install.sh, this never touches the network and needs a Go toolchain.
# The binary reports a git-derived version, so `orchard --version` tells a
# local build apart from a release at a glance.
#
#   scripts/install-local.sh              build and install to ~/.local/bin
#   ORCHARD_INSTALL_DIR=... scripts/install-local.sh    install elsewhere
#   scripts/install-local.sh --uninstall  remove the installed binary

set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT" || exit 1

DEST=${ORCHARD_INSTALL_DIR:-"$HOME/.local/bin"}
UNINSTALL=no
for arg in "$@"; do
	case $arg in
	--uninstall) UNINSTALL=yes ;;
	-h | --help) awk 'NR>1 && !/^#/{exit} NR>1{print substr($0, 3)}' "$0"; exit 0 ;;
	*) echo "unknown option: $arg (try --help)" >&2; exit 2 ;;
	esac
done

fail() { printf '%s\n' "orchard: $*" >&2; exit 1; }

if [ "$UNINSTALL" = yes ]; then
	if [ -e "$DEST/orchard" ]; then
		rm -f "$DEST/orchard"
		printf 'Removed %s\n' "$DEST/orchard"
		printf 'PATH entries added by an installer were left in your shell profile.\n'
	else
		printf 'Nothing to remove at %s\n' "$DEST/orchard"
	fi
	exit 0
fi

if ! command -v go >/dev/null 2>&1; then
	# A Go installed from go.dev is not on a login shell's default PATH.
	for dir in /usr/local/go/bin /opt/homebrew/bin "$HOME/go/bin" "$HOME/sdk/go/bin"; do
		[ -x "$dir/go" ] && PATH="$PATH:$dir" && export PATH && break
	done
fi
command -v go >/dev/null 2>&1 || fail 'go is not installed or not on PATH'

# Name the build after the checkout it came from, marking uncommitted work.
version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
case "$version" in *-dirty | dev) ;; *) version="$version-local" ;; esac

printf 'Building %s with %s\n' "$version" "$(go version | cut -d' ' -f3)"
go build -trimpath -ldflags="-X main.version=$version" -o bin/orchard ./cmd/orchard
[ -x bin/orchard ] || fail 'build produced no binary'

mkdir -p "$DEST"
# Stage and rename so a running orchard is replaced atomically.
staged=$(mktemp "$DEST/.orchard.XXXXXX")
trap 'rm -f "$staged"' EXIT HUP INT TERM
cp bin/orchard "$staged"
chmod 755 "$staged"
mv -f "$staged" "$DEST/orchard"
trap - EXIT HUP INT TERM

# Use an absolute, shell-quoted path so spaces and shell metacharacters are safe.
DEST=$(cd "$DEST" && pwd -P)
quoted_dest=$(printf '%s' "$DEST" | sed "s/'/'\\\\''/g")
path_command="export PATH='$quoted_dest':\$PATH"
startup_files=''
if [ "$(uname -s)" = Linux ]; then
	# Bash login shells read only the first existing profile in this order.
	# Interactive non-login shells (including desktop terminals) read .bashrc.
	shell_name=${SHELL:-sh}
	case "${shell_name##*/}" in
	bash)
		profile="$HOME/.profile"
		if [ -f "$HOME/.bash_profile" ]; then profile="$HOME/.bash_profile"
		elif [ -f "$HOME/.bash_login" ]; then profile="$HOME/.bash_login"; fi
		startup_files="$profile
$HOME/.bashrc"
		;;
	zsh) startup_files="${ZDOTDIR:-$HOME}/.zshrc" ;;
	sh | dash | '') startup_files="$HOME/.profile" ;;
	esac
	# Written exactly as install.sh writes it, so the two never double up.
	path_entry="case \":\$PATH:\" in *:'$quoted_dest':*) ;; *) $path_command ;; esac"
	printf '%s\n' "$startup_files" | while IFS= read -r profile; do
		[ -n "$profile" ] || continue
		if ! grep -Fqx "$path_entry" "$profile" 2>/dev/null; then
			if printf '\n# Added by the orchard installer.\n%s\n' "$path_entry" >> "$profile"; then
				printf 'Updated PATH in %s\n' "$profile"
			else
				printf 'Could not update %s; add this manually: %s\n' "$profile" "$path_command" >&2
			fi
		fi
	done
fi

printf '\nInstalled %s\n' "$DEST/orchard"
"$DEST/orchard" --version
case ":$PATH:" in *":$DEST:"*) ;; *)
	printf 'For this terminal, run: %s\n' "$path_command"
	[ -n "$startup_files" ] || printf 'Also add that command to your shell profile for future sessions.\n'
	;;
esac
printf 'Rerun this script after every change; install.sh would overwrite it with a release.\n'
