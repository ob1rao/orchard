# Orchard

A fast, visual disk-usage explorer for your terminal. Works on **Linux, macOS, and Raspberry Pi OS**.

```sh
curl -fsSL https://raw.githubusercontent.com/ob1rao/orchard/main/install.sh | sh
```

No GitHub login, Go compiler, or sudo needed. Start it with:

```sh
"$HOME/.local/bin/orchard"
```

![Orchard TUI showing directory sizes and a colorful storage treemap](docs/assets/orchard-tui.png)
*Actual TUI with demo files, showing apparent sizes.*

## Explore your storage

Select a disk and watch the treemap fill as Orchard scans. Bigger tiles mean more space used. Open a directory to see what's inside—Orchard never modifies your files.

- Browse files and sizes with the keyboard or mouse.
- Search across the disk using plain text or regex.
- View modified and created dates when available.
- Show or hide dotfiles; they're included by default.

To scan a particular folder:

```sh
"$HOME/.local/bin/orchard" "$HOME/Downloads"
```

## Essential controls

| Action | Control |
| --- | --- |
| Select / scroll | ↑ ↓ or mouse wheel |
| Open directory | Enter or double-click |
| Expand map / page smaller entries | Space; b goes back |
| Go back | Backspace or right-click |
| Search the disk | f or Ctrl-F; Tab toggles regex |
| View selected file / start at end | v / t |
| Filter this directory | / |
| Show / hide dotfiles | . |
| Help / quit | ? / q |

If `~/.local/bin` is on your PATH, you can simply run `orchard`.
To update, rerun the install command above.

[Full user guide](docs/reference.md) · [Development](docs/development.md) · [Releases](https://github.com/ob1rao/orchard/releases)
