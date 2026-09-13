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
Each child also includes `modified` and `created` as UTC RFC 3339 timestamps.
`created` is `null` when the filesystem does not provide a birth time.

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
ORCHARD_VERSION=v0.2.0 ORCHARD_INSTALL_DIR="$HOME/bin" sh install.sh
```

`ORCHARD_REPO` overrides the release repository. Uninstall by removing the
installed `orchard` binary. There are no configuration files or background services.

## Raspberry Pi OS / Raspbian

Install on the Pi itself (no GitHub login or Go compiler needed):

```sh
curl -fsSL https://raw.githubusercontent.com/ob1rao/orchard/main/install.sh | sh
"$HOME/.local/bin/orchard"
```

The explicit path works immediately, even if `~/.local/bin` is not yet on your
shell's PATH. To use the shorter command in the current shell:

```sh
export PATH="$HOME/.local/bin:$PATH"
orchard
```

Installer selection follows the machine architecture and userland word size:

| Pi environment | Release binary |
| --- | --- |
| Pi 1 / original Zero, `armv6l` | `linux_armv6` |
| 32-bit Pi OS, `armv7l` or `armv8l` | `linux_armv7` |
| 64-bit kernel (`aarch64`) with 32-bit userland | `linux_armv7` |
| 64-bit Pi OS, `aarch64` / `arm64` | `linux_arm64` |

The binaries are static and do not require a particular glibc version. Use a
maintained Raspberry Pi OS installation with `curl`, CA certificates, `tar`, and
`sha256sum` installed. The project's current ARM release binaries are tested
under CPU emulation, including interactive navigation; physical-device testing
is still needed. See [Raspberry Pi OS documentation](https://www.raspberrypi.com/documentation/computers/os.html)
for the supported 32-bit and 64-bit OS variants.

For a quick test over SSH, use an interactive terminal (`ssh -t` if needed),
then select the root filesystem `/` in Orchard. Allow the scan to complete,
enter a large directory, and return with Backspace. Mouse navigation requires a
terminal that forwards mouse events; keyboard navigation works independently.
For a smaller initial scan or slower SD card:

```sh
"$HOME/.local/bin/orchard" --workers 2 "$HOME"
```

If you report an issue, include the output of `uname -m`, `getconf LONG_BIT`,
`cat /etc/os-release`, and `"$HOME/.local/bin/orchard" --version`.

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
| Search recursively across scanned disk | Ctrl-F |
| Show/hide hidden files and directories (default: shown) | . or H |
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
border marks the selected entry. The sidebar lists every individual file and directory directly inside the current
directory, with a right-aligned size column. Scroll with the arrow keys, mouse
wheel, or Page Up/Down to reach every entry, including small and zero-byte files
that cannot occupy a visible terminal tile. Sizes use decimal units: KB = 1,000
bytes, MB = 1,000,000 bytes, and GB = 1,000,000,000 bytes. Files below 1 KB
are shown in bytes.

Hidden files and directories (names starting with `.`) are included by default.
Press `.` or `H` to show or hide them in both the sidebar and map. The footer
shows the current setting, which persists while navigating or rescanning during
the session. This is a display toggle: scanning still includes hidden entries,
and directory totals still include hidden data. A name filter limits both
the list and the map; the directory total still represents the whole directory.
The map can rearrange during scanning as sizes become known.

## Recursive search and dates

Press **Ctrl-F** from a directory view to search all discovered files and
folders under the selected scan root. Searching uses the in-memory index and
continues to refresh while the disk is being scanned; it does not reread files.
The current directory's `/` filter does not limit recursive search.

| Search action | Control |
| --- | --- |
| Type or edit query | Type / Backspace; Ctrl-U clears |
| Plain text / regex | Tab or F2 |
| Filename / full path | F3 |
| Include / hide hidden entries | F4 |
| Select result | Up/Down, Page Up/Down, Home/End, mouse wheel or click |
| Reveal result in its containing directory | Enter or double-click |
| Return to previous directory view | Esc or right-click |

Plain-text matching ignores case. Regex matching is case-sensitive by default;
use `(?i)` for case-insensitive matching. Examples:

- `invoice` — plain-text substring in filenames.
- `(?i)\.(jpg|png)$` — regex matching image filename extensions.
- `/logs/.*\.log$` — regex with full-path mode enabled.

Regex follows [Go's regular-expression syntax](https://pkg.go.dev/regexp),
which does not support lookaround or backreferences. Invalid expressions show
an error in the search view. The query is limited to 4,096 UTF-8 bytes. Typing
cancels outdated searches; matching runs outside the scanner's lock. When hiding
hidden entries, files inside hidden ancestor directories are also excluded.

Results show full paths and sizes, plus modified/created columns when the
terminal is at least 100 columns wide. Enter selects the matching entry in its
parent's directory view; Enter again opens it if it is a directory. Search lists
up to 10,000 matches in discovery order, then sorts those results by size. It
reports the full match count and asks you to narrow the query when capped.
Results reflect the scan, not subsequent filesystem changes; rescan with `r`
after closing search to refresh the index.

The normal directory view shows **modified and created timestamps for the
selected entry** below the map, with date columns in the sidebar when the
terminal is at least 150 columns wide. Dates use the local timezone, to the
nearest second. Directory timestamps describe the directory itself, not the
newest descendant's timestamp.

Creation time is read from Linux `statx` when supported and from macOS birth
time. Older Linux kernels fall back to ordinary metadata reads. If creation
time is unsupported, Orchard displays **unavailable** (or `—` in compact date
columns); it never substitutes Unix ctime, which is metadata-change time.
See the [Linux statx reference](https://www.man7.org/linux/man-pages/man2/statx.2.html).

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
their apparent sizes. The tree retains a node per discovered entry plus an append-only pointer index
for search. Timestamps are stored as Unix seconds to keep overhead small. Memory is
linear in file count. Very large single directories also cost more to sort.
Choose a smaller subtree on memory-constrained Pis. `--workers 1` can help on
seek-sensitive disks; the default is 2–8 workers depending on available CPUs.
Cancellation stops scheduling immediately; an in-flight filesystem call can
still delay exit on a stalled disk or network mount.

A development benchmark on Linux amd64 / Intel Xeon 6975P-C, averaged over three
warm-cache iterations, scanned 10,000 small files in **13.9 ms** (~718k files/s),
with 4.16 MB of Go allocations per scan. A 10,000-entry map in a 160 × 50 viewport
took **0.30 ms**. Searching 100,000 indexed filenames averaged **3.5 ms**
for plain text and **4.0 ms** for regex (query `999`, three iterations).
These synthetic results exclude fixture creation and do not
predict cold-disk, network, macOS, or Raspberry Pi performance.

## Test and release

```sh
make test       # race tests, vet, installer fixtures, real PTY smoke test
make bench      # synthetic scanner and layout benchmarks
make release VERSION=v0.2.0
```

`make test` also needs Python 3; its integration tests use only the standard
library. `make release` runs on Linux and cross-compiles all six targets into
`dist/`, with `checksums.txt`. CI runs tests on Linux and macOS and produces
cross-platform build artifacts. Both CI and release publication require ARM
emulation tests to pass. To run those locally after building archives, install
`qemu-user` and run `python3 scripts/test_arm.py dist`. A pushed `v*` tag
publishes release archives.

Validated: Linux execution locally and in GitHub Actions; native macOS ARM64
execution in GitHub Actions; scanner race tests; geometry invariants;
keyboard/mouse navigation in a real PTY; resize and terminal restoration; eleven
installer scenarios; and compilation of all six targets. ARMv6 (ARM1176), ARMv7
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
