# Orchard user guide

[Back to the quick start](../README.md)

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

## Unmounted disks and volumes

Start with `orchard --unmounted`, or press **u** in the disk picker to include
unmounted volumes. Press **r** to refresh after plugging in a drive. Mounted
volumes remain available if discovery fails. Unmounted entries show capacity;
used and free space become available after mounting.

Select an unmounted filesystem with Enter or a click. Orchard proposes a new
mountpoint in your home directory. Edit it with Backspace, or press Ctrl-U and
type another absolute path. The parent must already exist and be writable by
your user; the mountpoint itself must not exist. Enter mounts and starts the
scan; Esc cancels without creating anything.

Orchard requests a read-only mount. Linux and Raspberry Pi OS use `lsblk` and
`mount` from util-linux, with `sudo` for the mount command when you are not root.
The TUI temporarily restores the terminal for the system password prompt.
macOS uses the built-in `diskutil` and its system authorization. Orchard does
not collect passwords. You do not need to run the entire scanner with sudo.

The volume **stays mounted after Orchard exits**. Unmount it using your system's
disk utility, `sudo umount /your/mountpoint` on Linux, or
`diskutil unmount /your/mountpoint` on macOS; then remove the empty mountpoint if
no longer needed. Orchard does not change fstab or create an automatic mount.

Locked/encrypted volumes must be unlocked with system tools first. Raw disks,
swap, and storage-pool members cannot be browsed directly; Orchard does not
format, repair, assemble storage pools, or install filesystem drivers. The OS
must support the filesystem. Read-only mounting is not a forensic write-block
procedure: some filesystems may replay their journal while mounting, as
explained in the [Linux mount manual](https://www.man7.org/linux/man-pages/man8/mount.8.html).

## Navigate

| Action | Keyboard / mouse |
| --- | --- |
| Select disk | Up/Down, then Enter; or click a disk |
| Show/hide unmounted volumes in picker | u |
| Select entry | Up/Down, j/k, click a tile/list row |
| Open directory | Enter, l, Right, or double-click |
| Parent directory | Left, h, Backspace, right-click, or click path bar |
| Return to scan root | g |
| Move through list | Mouse wheel, Page Up/Down, Home/End |
| Expand treemap / next smaller entries | Space |
| Previous treemap page / restore sidebar | b |
| View selected file / start at end | v / t |
| Filter current directory | / then type; Enter applies, Esc clears |
| Search recursively across scanned disk | f or Ctrl-F |
| Show/hide hidden files and directories (default: shown) | . or H |
| Toggle allocated/apparent bytes | a |
| Show/hide disk free space and unaccounted usage | i |
| Stop scan; keep partial results | s |
| Rescan selected disk | r |
| Choose another disk | d |
| Help / redraw / quit | ? / Ctrl-L / q or Ctrl-C |

Use a terminal at least 50 columns × 16 rows; 120 × 32 or larger is recommended.
Mouse support depends on your terminal and multiplexer configuration. Everything
is accessible from the keyboard. Press `?` for the full control reference; a
larger terminal fits more help text.

Tiles represent the **immediate children of the current directory**, with area
proportional to their size. Enter a directory for a new treemap of its children.
File extensions share colors; directory colors are stable by name. The bright
border marks the selected entry. The sidebar lists every individual file and directory directly inside the current
directory, with a right-aligned size column. Scroll with the arrow keys, mouse
wheel, or Page Up/Down to reach every entry, including small and zero-byte files
that cannot occupy a visible terminal tile. Sizes use decimal units: KB = 1,000
bytes, MB = 1,000,000 bytes, and GB = 1,000,000,000 bytes. Files below 1 KB
are shown in bytes.

Hidden files and directories (names starting with `.`) are included by default.
Press `.` or `H` to show or hide them in both the sidebar and treemap. The footer
shows the current setting, which persists while navigating or rescanning during
the session. This is a display toggle: scanning still includes hidden entries,
and directory totals still include hidden data. A name filter limits both
the list and the treemap; the directory total still represents the whole directory.
The treemap can rearrange during scanning as sizes become known.

## Disk free space and unaccounted usage

The treemap shows a black **Disk free** tile and a disk-space header by default.
The tile occupies the filesystem’s free-space percentage; the remaining treemap
shows the current directory’s entries, including in expanded and paged views.
Press **i** to hide or restore both the tile and summary. On wider terminals it
also shows total capacity and space available to an ordinary user; available
space can be lower than free space because some blocks are reserved. Values
refresh in the background every five seconds while the summary is enabled.

**Outside scan/other** estimates filesystem used space minus the whole scan's
allocated bytes. It can include inaccessible files, filesystem metadata,
snapshots, deleted-but-open files, and data outside the selected scan path.
During an incomplete or stopped scan it is labeled **Unscanned/other**; narrow
terminals abbreviate either label to **Other**. `~` marks an estimate.

Unreadable-entry counts remain separate: their exact byte size cannot generally
be determined. Scanning a single folder does not measure all disk usage, so its
remainder includes other folders. The figures stay disk-wide while navigating,
filtering, hiding dotfiles, paging, or switching to apparent sizes. Filesystem
capacity and per-file allocation are not always directly comparable, especially
with shared blocks or concurrent changes; if scanned allocation exceeds reported
used space, the remainder is shown as **unknown**, not a negative number or a
claim that everything was scanned. These statistics describe the filesystem
containing the scan root, not the sum of partitions on a physical drive.

## Expand and page the treemap

Press **Space** to expand the treemap across the terminal, hiding the sidebar.
Press Space again to start at the first entry whose full name and size do not
fit, including entries too small to draw. Larger entries disappear and the
remaining entries expand, keeping their relative sizes. Continue until the
smallest entries are readable. **b** retraces the pages and finally restores
the sidebar and its selection. The footer shows the page and entry range;
percentages still refer to the entire directory.

If the first entry is already clipped, it gets an individual page with a
wrapped name before paging continues. Exceptionally long names can still be
clipped in a small terminal. Zero-byte entries appear on a separate, labeled
page with equal tiles; those tile areas do not represent disk usage.

Arrow keys, the mouse, Enter, search, and file viewing work in the expanded treemap.
Page anchors follow their entries as scan results reorder. Changing directory,
filter, hidden visibility, or size metric returns to the split view. In the
file viewer, Space and b continue to page through file contents.

## View file contents

Select a regular text file in a directory, then press **v** to open its contents
or **t** to start on the final page. This works for logs, configs, READMEs, and
other text files regardless of their extension. To view a search result, first
press Enter to reveal it in the directory, then press v or t.

| Viewer action | Control |
| --- | --- |
| Scroll | Up/Down, j/k, or mouse wheel |
| Previous / next page | Page Up/Down, b/Space |
| Start / end | Home/End, g/G, or v/t |
| Pan long lines | Left/Right or h/l |
| Refresh contents | r |
| Return to the selected file | Esc, q, or right-click |
| Open disk search | f or Ctrl-F |

`t` starts at the end; it does **not** automatically follow new writes. Press
`r` to refresh; if you were at the end, the viewer stays at the new end. File
rotation requires closing and reopening the viewer because it keeps the same
open file descriptor. The viewer is read-only and never launches a shell or editor.

Pages are limited to 256 KiB and 512 rows, and very long lines are split into
16 KiB segments. The viewer seeks backwards for the final page, avoiding a full
read or line index even for large logs. Positions are shown as byte offsets.
Tabs display as four spaces; terminal controls and invalid UTF-8 are replaced
for display. NUL-containing binary data, symlinks, directories, and special
files are rejected. Permissions still apply. In-flight reads on a stalled
filesystem can delay navigation.

## Recursive search and dates

Press **f** or **Ctrl-F** from a directory view to search all discovered files and
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
selected entry** below the treemap, with date columns in the sidebar when the
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
  usable, with an incomplete-scan indication. The scanner never elevates privileges; the optional mount command may request system authorization.
  macOS privacy protections can restrict access even when Unix permissions allow it.
- Disk selection shows filesystem capacity and free space. The treemap shows
  a black disk-free tile alongside discoverable file allocations. File tiles
  exclude snapshots, filesystem overhead,
  deleted-but-open files, or inaccessible data. Directory metadata is included
  in totals but has no separate tile. APFS clones/reflinks/compression can prevent
  per-file reported allocations from matching physical space used. Shared APFS
  container capacities are not additive across volumes.
- Filesystem changes during scanning can produce partial or inconsistent results;
  this is a live traversal, not a filesystem snapshot. Scanning does not modify files.



## Storage picker

The opening screen groups volumes by their reported parent disk. Each volume
shows the disk → partition → container/device chain when the OS supplies it.
Linux and Raspberry Pi OS use `lsblk`; macOS uses `diskutil`. Network filesystems
and storage with unavailable ancestry remain selectable with an explicit unknown
label. Press **u** to include unmounted volumes; **r** refreshes discovery.

Disk and volume bars share one linear capacity scale across the list. Green
solid cells show used space, black-backed light cells show filesystem free space,
and `?` shows unknown usage. Tiny capacities occupy at least one terminal cell.
Disk summaries count each visible filesystem or shared APFS container once;
unmounted, undiscovered, and unallocated capacity stays unknown. APFS volume
rows show shared container capacity, not independently additive volume sizes.
Multi-disk storage is identified without assigning its capacity to one disk.

The compact terminal-art Orchard tree and wordmark appear on taller terminals.
Wide terminals add a three-disk overview beside the wordmark; additional disks
remain available in the scrolling list.
A small discovery indicator animates while storage information loads; there is
no splash-screen delay. Short terminals use a one-line wordmark. Arrow keys,
Page Up/Down, Home/End, and mouse selection work throughout the grouped list.
