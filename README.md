# orchard

A fast, read-only disk-usage treemap for your terminal. Select a mounted disk,
watch its map populate while it scans, and drill into directories with the mouse
or keyboard. Built with Go and [Tcell v3](https://github.com/gdamore/tcell), with
true-color tiles, Unicode labels, grapheme-aware clipping, and terminal color
fallbacks. No GUI, browser, daemon, or runtime installation required.

## Run it now

From a source checkout, with Go **1.27.1+**:

```sh
go build -trimpath -o bin/orchard ./cmd/orchard
./bin/orchard
```

Launch without arguments to choose a disk. You can also start with a directory:

```sh
./bin/orchard "$HOME"
./bin/orchard --apparent /path/to/folder
./bin/orchard --workers 4 /path/to/folder
./bin/orchard --scan /path/to/folder   # JSON, without a terminal
```

Options precede the directory argument. `--scan` returns 0 on a complete scan,
1 for unreadable entries or another error, 2 for invalid arguments, and 130 when
cancelled. A partial JSON report is still emitted for scan errors/cancellation.
`stats.Elapsed` is a duration in nanoseconds; `bytes` uses the selected metric.

## Install a release

Release binaries target:

| System | Architectures |
| --- | --- |
| Linux | x86-64, ARM64 |
| Raspberry Pi OS | ARM64, ARMv7, ARMv6 (including Pi Zero / original Pi) |
| macOS | Apple Silicon, Intel |

Linux builds are static (`CGO_ENABLED=0`), including for musl-based distributions.
The installer detects the OS/architecture, downloads a release, verifies its
SHA-256 checksum, and atomically installs to `~/.local/bin`. It never uses sudo.
The installer needs `curl`, `tar`, and `sha256sum` or `shasum`.

Install the latest release without a GitHub account or login:

```sh
curl --proto '=https' --tlsv1.2 -fsSL \
  https://raw.githubusercontent.com/ob1rao/orchard/main/install.sh | sh
```

To review the script before running it, save it locally instead of piping to
`sh`, then run `sh install.sh`. From a checkout, customize a release install:

```sh
ORCHARD_VERSION=v0.1.0 ORCHARD_INSTALL_DIR="$HOME/bin" sh install.sh
```

`ORCHARD_REPO` overrides the release repository. Uninstall by removing the
installed `orchard` binary. There are no configuration files or background services.

## Navigate

| Action | Keyboard / mouse |
| --- | --- |
| Select disk | Up/Down, then Enter; or click a disk |
| Select entry | Up/Down, j/k, click a tile/list row |
| Open directory | Enter, l, Right, or double-click |
| Parent directory | Left, h, Backspace, right-click, or click path bar |
| Return to scan root | g |
| Move through list | Mouse wheel, Page Up/Down, Home/End |
| Filter current directory | / then type; Enter applies, Esc clears |
| Toggle allocated/apparent bytes | a |
| Stop scan; keep partial results | s |
| Rescan selected disk | r |
| Choose another disk | d |
| Help / redraw / quit | ? / Ctrl-L / q or Ctrl-C |

Use a terminal at least 50 columns × 16 rows; 120 × 32 or larger is recommended.
Mouse support depends on your terminal and multiplexer configuration. Everything
is accessible from the keyboard. Press `?` for the full control reference; a
larger terminal fits more help text.

Tiles represent the **immediate children of the current directory**, with area
proportional to their size. Enter a directory for a new map of its children.
File extensions share colors; directory colors are stable by name. The bright
border marks the selected entry. The ranked list includes small and zero-byte
entries that cannot occupy a visible terminal tile. A name filter limits both
the list and the map; the directory total still represents the whole directory.
The map can rearrange during scanning as sizes become known.

## What the numbers mean

- **Allocated** (default): filesystem-reported blocks × 512, including directory
  metadata. This distinguishes sparse files from their logical lengths.
- **Apparent**: logical lengths reported by the filesystem, also including
  directory metadata.
- Hard-linked data is counted once per scan. Which directory receives that
  allocation depends on traversal order; duplicate names remain in the list
  with zero size. Symlinks are counted themselves and never traversed. An
  explicitly supplied root symlink is resolved before scanning.
- Scans stay on the root device. Other mounted filesystems and previously seen
  directory inodes are skipped. Select those disks separately. Pseudo filesystems
  are omitted from the Linux disk picker. On macOS, select
  `/System/Volumes/Data` to inspect the user-data volume when it appears separately.
- Unreadable entries are counted and the latest error is displayed. Results remain
  usable, with an incomplete-scan indication. The scanner never elevates privileges.
  macOS privacy protections can restrict access even when Unix permissions allow it.
- Disk selection shows filesystem capacity and free space. The treemap shows
  discoverable file allocations, not free space, snapshots, filesystem overhead,
  deleted-but-open files, or inaccessible data. Directory metadata is included
  in totals but has no separate tile. APFS clones/reflinks/compression can prevent
  per-file reported allocations from matching physical space used. Shared APFS
  container capacities are not additive across volumes.
- Filesystem changes during scanning can produce partial or inconsistent results;
  this is a live traversal, not a filesystem snapshot. No files are modified.

## Performance and implementation

A bounded worker pool reads directories in 256-entry batches. One coordinator
owns tree updates, deduplicates inode identities, and propagates batch totals
through the ancestors. A mutex protects brief snapshot copies; sorting happens
outside the lock. The terminal refreshes live results five times per second and
stops periodic redraws once scanning finishes. Tcell sends terminal-cell diffs.

The map uses balanced binary weighted partitioning, adjusted for terminal cells
being taller than they are wide. It clips sub-cell items rather than inflating
their apparent sizes. The tree retains a node per discovered entry, so memory is
linear in file count. Very large single directories also cost more to sort.
Choose a smaller subtree on memory-constrained Pis. `--workers 1` can help on
seek-sensitive disks; the default is 2–8 workers depending on available CPUs.
Cancellation stops scheduling immediately; an in-flight filesystem call can
still delay exit on a stalled disk or network mount.

A development benchmark on Linux amd64 / Intel Xeon 6975P-C, averaged over three
warm-cache iterations, scanned 10,000 small files in **13.8 ms** (~724k files/s),
with 3.34 MB of Go allocations per scan. A 10,000-entry map in a 160 × 50 viewport
took **0.30 ms**. These synthetic results exclude fixture creation and do not
predict cold-disk, network, macOS, or Raspberry Pi performance.

## Test and release

```sh
make test       # race tests, vet, installer fixtures, real PTY smoke test
make bench      # synthetic scanner and layout benchmarks
make release VERSION=v0.1.0
```

`make test` also needs Python 3; its integration tests use only the standard
library. `make release` runs on Linux and cross-compiles all six targets into
`dist/`, with `checksums.txt`. CI runs tests on Linux and macOS and produces
cross-platform build artifacts. A pushed `v*` tag publishes release archives.

Validated: Linux execution locally and in GitHub Actions; native macOS ARM64
execution in GitHub Actions; scanner race tests; geometry invariants;
keyboard/mouse navigation in a real PTY; resize and terminal restoration; nine
installer scenarios; and compilation of all six targets. Raspberry Pi and Intel
Mac binaries still require native device smoke testing.

Publish a new version from this checkout with GitHub CLI authenticated and
repository write access (replace `vX.Y.Z` with the next version):

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

Wait for the Release workflow before testing the curl installer. Dependency
licenses are included in `THIRD_PARTY_NOTICES.txt` and every release archive.
