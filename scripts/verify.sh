#!/bin/sh
# Build Orchard, run every check, then hand over to the TUI.
#
# Runs each step even when an earlier one fails, so a single pass reports
# everything that is broken rather than only the first thing. POSIX sh and
# macOS's system tools only; no dependencies beyond Go and Python 3.
#
#   scripts/verify.sh            build, full checks, then launch the TUI
#   scripts/verify.sh --quick    skip the race, installer and PTY suites
#   scripts/verify.sh --no-run   stop after the checks

set -u

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT" || exit 1

QUICK=no
RUN=yes
for arg in "$@"; do
	case $arg in
	--quick) QUICK=yes ;;
	--no-run) RUN=no ;;
	-h | --help) awk 'NR>1 && !/^#/{exit} NR>1{print substr($0, 3)}' "$0"; exit 0 ;;
	*) echo "unknown option: $arg (try --help)" >&2; exit 2 ;;
	esac
done

FAILED=""
BOLD=$(printf '\033[1m')
DIM=$(printf '\033[2m')
RED=$(printf '\033[31m')
GREEN=$(printf '\033[32m')
YELLOW=$(printf '\033[33m')
OFF=$(printf '\033[0m')
if [ ! -t 1 ]; then BOLD=; DIM=; RED=; GREEN=; YELLOW=; OFF=; fi

step() { printf '\n%s== %s%s\n' "$BOLD" "$1" "$OFF"; }
note() { printf '%s   %s%s\n' "$DIM" "$1" "$OFF"; }
warn() { printf '%s   ! %s%s\n' "$YELLOW" "$1" "$OFF"; }

# run <name> <command...>: report the outcome and remember any failure.
run() {
	name=$1
	shift
	if "$@"; then
		printf '%s   ok%s  %s\n' "$GREEN" "$OFF" "$name"
	else
		printf '%s   FAILED%s  %s\n' "$RED" "$OFF" "$name"
		FAILED="$FAILED
  - $name"
	fi
}

if ! command -v go >/dev/null 2>&1; then
	# A Go installed from go.dev is not on a login shell's default PATH.
	for dir in /usr/local/go/bin /opt/homebrew/bin "$HOME/go/bin" "$HOME/sdk/go/bin"; do
		[ -x "$dir/go" ] && PATH="$PATH:$dir" && export PATH && break
	done
fi
command -v go >/dev/null 2>&1 || { echo "go is not installed or not on PATH" >&2; exit 1; }

step "Build  ($(go version | cut -d' ' -f3-4))"
# The checked-in bin/ may hold a binary for another machine; always rebuild.
run "go build" go build -trimpath -o bin/orchard ./cmd/orchard
[ -x bin/orchard ] || { echo "${RED}build produced no binary; stopping${OFF}" >&2; exit 1; }
note "$(file bin/orchard | cut -d: -f2- | cut -c2-)"

step "Vet"
run "go vet" go vet ./...
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 linux/arm; do
	run "vet $target" env GOOS="${target%/*}" GOARCH="${target#*/}" go vet ./...
done

step "Tests"
if [ "$QUICK" = yes ]; then
	run "go test" go test ./...
	warn "--quick: skipped the race detector, installer and PTY suites"
elif command -v cc >/dev/null 2>&1 || command -v clang >/dev/null 2>&1 || command -v gcc >/dev/null 2>&1; then
	run "go test -race" go test -race ./...
else
	run "go test" go test ./...
	warn "no C compiler found, so the race detector was skipped"
	warn "on macOS: xcode-select --install"
fi

step "Storage picker and volume detail"
run "picker tests" go test ./internal/ui -run 'Picker|Storage|Shared|Band|Disk|Usage' -v
run "volume tests" go test ./internal/volumes -run 'APFS|Topology|Space|Volumes' -v

if [ "$QUICK" = no ]; then
	step "Installer and real-terminal suites"
	if command -v python3 >/dev/null 2>&1; then
		run "installer" python3 scripts/test_installer.py
		run "PTY" python3 scripts/test_tui.py
	else
		warn "python3 not found, so the installer and PTY suites were skipped"
	fi
fi

if [ "$(uname -s)" = Darwin ]; then
	step "APFS cross-check"
	note "Orchard's per-volume sizes come from these numbers. Compare them"
	note "with the volume list on screen in a moment."
	diskutil apfs list 2>/dev/null |
		grep -E 'APFS Container Reference|APFS Volume Disk|Capacity (In Use|Quota)|Name:' |
		sed 's/^ */   /'
	printf '\n'
	df -h 2>/dev/null | sed -n '1p;/^\/dev\//p' | sed 's/^/   /'
fi

step "Result"
if [ -n "$FAILED" ]; then
	printf '%sFailed:%s%s\n\n' "$RED" "$OFF" "$FAILED"
	exit 1
fi
printf '%sEverything passed.%s\n' "$GREEN" "$OFF"

[ "$RUN" = no ] && exit 0
[ -t 0 ] || { note "not a terminal; skipping the TUI"; exit 0; }

cols=$(tput cols 2>/dev/null || echo 0)
lines=$(tput lines 2>/dev/null || echo 0)
printf '\n%sCheck on screen:%s\n' "$BOLD" "$OFF"
cat <<'CHECKS'
   1. Each physical disk is one frame, with the volumes on it nested inside.
   2. APFS volumes in a container are sized by their own bytes, not four
      tiles each claiming the whole disk.
   3. "/" has a real size and a real tile. It is mounted from a snapshot
      device, which is exactly the case that used to come out empty.
   4. One click on a map tile only selects it; double-click explores.
      One click on a list row explores.
   5. Press u for unmounted volumes, r to refresh, q to quit.
CHECKS
if [ "$cols" -lt 92 ] || [ "$lines" -lt 22 ]; then
	warn "this window is ${cols}x${lines}; the map needs 92x22 (120x36 is better)"
	warn "resize and rerun, or continue to see the narrow list-only layout"
fi
printf '\n%sPress Enter to start Orchard, or Ctrl-C to stop here.%s ' "$BOLD" "$OFF"
read -r _ignored
exec ./bin/orchard
