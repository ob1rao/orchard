# Orchard development

[Back to the quick start](../README.md) · [User guide](reference.md)

## Build from source

From a checkout with Go 1.27.1 or newer:

```sh
make build
./bin/orchard
```

## Test your checkout as an installed command

`install.sh` always downloads a published release; it is the end-user path and
never builds from source, so running it from a clone will replace your build
with whatever is on GitHub. To put *this* working tree on your PATH instead:

```sh
make install     # builds, then installs to ~/.local/bin/orchard
orchard --version
```

The version string comes from `git describe`, so a local build reports
something like `v0.7.0-3-g0b0df5c-local` or `-dirty`, while a release reports a
bare tag. That is the quickest way to tell which binary you are running. Set
`ORCHARD_INSTALL_DIR` to install elsewhere, and rerun `make install` after
every change. `make uninstall` removes the binary; the PATH line that either
installer adds to your shell profile stays.

## Performance and implementation

A bounded worker pool reads directories in 256-entry batches. One coordinator
owns tree updates, deduplicates inode identities, and propagates batch totals
through the ancestors. A mutex protects brief snapshot copies; sorting happens
outside the lock. The terminal refreshes live results five times per second and
stops periodic redraws once scanning finishes. Tcell sends terminal-cell diffs.

The treemap uses balanced binary weighted partitioning, adjusted for terminal cells
being taller than they are wide. It clips sub-cell items rather than inflating
their apparent sizes. The tree retains a node per discovered entry plus an append-only pointer index
for search. Timestamps are stored as Unix seconds to keep overhead small. Memory is
linear in file count. Very large single directories also cost more to sort.
Choose a smaller subtree on memory-constrained Pis. `--workers 1` can help on
seek-sensitive disks; the default is 2–8 workers depending on available CPUs.
Cancellation stops scheduling immediately; an in-flight filesystem call can
still delay exit on a stalled disk or network mount.

A development benchmark on Linux amd64 / Intel Xeon 6975P-C, averaged over three
warm-cache iterations, scanned 10,000 small files in **13.9 ms** (~718k files/s),
with 4.16 MB of Go allocations per scan. A 10,000-entry treemap in a 160 × 50 viewport
took **0.30 ms**. Searching 100,000 indexed filenames averaged **3.5 ms**
for plain text and **4.0 ms** for regex (query `999`, three iterations).
These synthetic results exclude fixture creation and do not
predict cold-disk, network, macOS, or Raspberry Pi performance.

## Test and release

```sh
make test       # race tests, vet, installer fixtures, real PTY smoke test
make verify     # the above, cross-target vet, then launch the TUI to check by eye
make bench      # synthetic scanner and layout benchmarks
make release VERSION=v0.2.0
```

`make verify` runs every check in one pass, continuing past a failure so one
run reports everything that is broken, and prints what to confirm on screen
before starting Orchard. It always rebuilds first, because a checked-in
`bin/orchard` may have been built for another machine. `scripts/verify.sh
--quick` skips the race, installer and PTY suites for a faster loop, and
`--no-run` stops before the TUI.

`make test` also needs Python 3; its integration tests use only the standard
library. `make release` runs on Linux and cross-compiles all six targets into
`dist/`, with `checksums.txt`. CI runs tests on Linux and macOS and produces
cross-platform build artifacts. Both CI and release publication require ARM
emulation tests to pass. To run those locally after building archives, install
`qemu-user` and run `python3 scripts/test_arm.py dist`. A pushed `v*` tag
publishes release archives.

Validated: Linux execution locally and in GitHub Actions; native macOS ARM64
execution in GitHub Actions; scanner race tests; geometry invariants;
keyboard/mouse navigation in a real PTY; resize and terminal restoration; fifteen
installer scenarios; and compilation of all six targets. The storage picker's
grouping, band heights, tile areas and mouse behaviour are covered by cell-buffer
tests. `diskutil`'s APFS container report is parsed against plist fixtures only:
per-volume usage inside a shared container still needs testing on real hardware. ARMv6 (ARM1176), ARMv7
(Cortex-A7), and ARM64 (Cortex-A53) release binaries also
pass scan accounting and interactive PTY tests under QEMU. Physical Raspberry Pi
and Intel Mac testing remains necessary; emulation does not measure SD-card
performance or reproduce a complete Raspberry Pi OS installation.

Publish a new version from this checkout with GitHub CLI authenticated and
repository write access (replace `vX.Y.Z` with the next version):

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

Wait for the Release workflow before testing the curl installer. Dependency
licenses are included in `THIRD_PARTY_NOTICES.txt` and every release archive.

## Refresh the README preview

The preview captures the real TUI in a pseudo-terminal against sparse demo files.
It does not allocate the large apparent file sizes shown in the image.
With Pillow, pyte, fontconfig, and DejaVu Sans Mono installed:

```sh
make build
python3 scripts/capture_preview.py
```

When capturing the TUI over a remote shell, check the terminal environment
first. `NO_COLOR=1` disables Orchard's color output, and `TERM=unknown` can
prevent tcell from detecting color capabilities. Run the session with a real
terminal type and color enabled:

```sh
tmux new-session -d -s orchard -x 120 -y 32 \
  "sh -c 'unset NO_COLOR; export TERM=xterm-256color COLORTERM=truecolor; exec ./bin/orchard /'"
tmux capture-pane -p -e -t orchard:0.0 -S -
```

The `-e` flag preserves ANSI styling in the capture. Verify that the captured
stream contains `38;2;` truecolor sequences before rendering it as an image.

Unmounted-volume discovery and mount failures are covered by fixtures and fake
command runners. On Linux CI, the opt-in mount test also creates a temporary ext4 image, attaches
it to a loop device, mounts it read-only, checks its contents, and detaches it.
This opt-in test requires passwordless sudo, util-linux, e2fsprogs, and udev;
ordinary tests never mount anything. Physical USB disks and macOS mounting still
need device testing.

```sh
ORCHARD_TEST_MOUNT=1 go test -v ./internal/volumes -run TestMountLoopbackIntegration
```
