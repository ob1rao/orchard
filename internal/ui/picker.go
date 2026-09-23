package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/treemap"
)

// pickerTile links a rectangle in the storage map back to the volume it draws.
type pickerTile struct {
	treemap.Rect
	volume int
}

// pickerRow is one line of the volume list: a heading, or a volume to pick.
type pickerRow struct {
	volume  int
	heading string
}

func listRows(physical, other []storageDisk) []pickerRow {
	var rows []pickerRow
	add := func(disks []storageDisk, heading func(storageDisk) string) {
		for _, d := range disks {
			if heading != nil {
				rows = append(rows, pickerRow{volume: -1, heading: heading(d)})
			}
			for _, p := range d.pools {
				for _, i := range p.members {
					rows = append(rows, pickerRow{volume: i})
				}
			}
		}
	}
	add(physical, storageDisk.heading)
	if len(other) > 0 {
		mounts := 0
		for _, d := range other {
			mounts += d.volumeCount()
		}
		rows = append(rows, pickerRow{volume: -1, heading: plural(mounts, "mount") + " · no disk ancestry reported"})
		add(other, nil)
	}
	return rows
}

var brandArt = []string{
	"    .oOo.     O R C H A R D",
	"  .oOooOo.    See where your space goes.",
	"    /|\\       A treemap explorer for your disks",
	"   _/ \\_",
}

func (a *App) drawPicker(w, h int) {
	physical, other := splitStorage(groupStorage(a.volumes))
	a.disk = max(0, min(a.disk, max(0, len(a.volumes)-1)))
	a.pickerRows, a.pickerTiles = map[int]int{}, nil

	// The map earns its place only when it can carry readable tiles; below
	// that the list is the whole screen rather than a squeezed pair of panels.
	mapped := w >= 92 && h >= 22
	listWidth := w - 4
	if mapped {
		listWidth = min(46, max(30, w*2/5))
	}
	// Rows 1-4 carry the brand, then the subtitle and legend sit at top-2 and
	// top-1, so the list starts clear of both.
	top := 3
	if h >= 24 {
		for i, line := range brandArt {
			art := line
			if i == 0 && a.diskLoading && a.brandFrame%2 == 1 {
				art = "    .OoO.     O R C H A R D"
			}
			a.text(2, 1+i, max(listWidth, min(w-4, 48)), art, base.Foreground(accent).Bold(i == 0))
		}
		top = 7
	} else {
		a.text(2, 0, w-4, "ORCHARD · your storage, in treemaps", base.Foreground(accent).Bold(true))
	}
	subtitle := "Choose a volume · u show unmounted"
	if a.showUnmounted {
		subtitle = "Choose a volume · mounted + unmounted · u hide unmounted"
	}
	if a.diskLoading {
		subtitle += " · discovering " + []string{".", "o", "O", "o"}[a.brandFrame%4]
	}
	a.text(2, top-2, listWidth, subtitle, base)
	a.drawLegend(2, top-1, listWidth)

	rows := listRows(physical, other)
	listTop, listBottom := top, h-6
	height := max(1, listBottom-listTop+1)
	// The list starts at column 2, so this is the column the map begins after.
	a.listWidth = listWidth + 2
	selected := 0
	for i, row := range rows {
		if row.volume == a.disk {
			selected = i
		}
	}
	a.offset = max(0, min(a.offset, max(0, len(rows)-height)))
	a.offset = min(a.offset, selected)
	a.offset = max(a.offset, selected-height+1)
	for i := a.offset; i < len(rows) && i-a.offset < height; i++ {
		a.drawPickerRow(rows[i], 2, listTop+i-a.offset, listWidth)
	}
	// Paging moves by volumes, so it must count the volumes on screen rather
	// than the rows, which include a heading for every disk.
	a.listHeight = max(1, len(a.pickerRows))
	if len(a.volumes) == 0 {
		a.text(2, listTop, listWidth, "No volumes found. Try: orchard /path/to/folder", base)
	}
	if mapped {
		title := "STORAGE MAP · tile area is bytes · colour is used, black is free"
		if len(physical) > 1 {
			// Say that bands are floored: a reader comparing two disks by eye
			// should know the smallest one is drawn larger than it really is.
			group := "disk"
			if reportedDisks(physical) == 0 {
				group = "mount"
			}
			title = fmt.Sprintf("STORAGE MAP · %s · band height tracks capacity, floored to stay readable", plural(len(physical), group))
		}
		a.text(listWidth+4, top-1, w-listWidth-6, title, base.Foreground(muted))
		a.drawStorageMap(physical, treemap.Rect{X: listWidth + 4, Y: top, W: w - listWidth - 6, H: listBottom - top + 1})
	}
	a.drawPickerFooter(w, h, physical, other)
}

// The legend names the three fills the map uses, in the colours it uses, so
// the reader never has to guess what black or hatching mean.
func (a *App) drawLegend(x, y, width int) {
	if width < 34 {
		return
	}
	swatch := tcell.NewHexColor(0x4f6fb8)
	if a.disk < len(a.volumes) {
		swatch = fsColor(a.volumes[a.disk].Type)
	}
	a.screen.FillArea(x, y, 2, 1, ' ', base.Background(swatch))
	a.text(x+3, y, 5, "used", base.Foreground(muted))
	a.screen.FillArea(x+8, y, 2, 1, ' ', base.Background(tcell.NewHexColor(freeHex)))
	a.text(x+11, y, 5, "free", base.Foreground(muted))
	a.text(x+16, y, 2, "░░", base.Foreground(muted))
	a.text(x+19, y, width-19, "unallocated", base.Foreground(muted))
}

func (a *App) drawPickerRow(row pickerRow, x, y, width int) {
	if row.volume < 0 {
		a.text(x, y, width, row.heading, base.Bold(true))
		return
	}
	a.pickerRows[y] = row.volume
	v := a.volumes[row.volume]
	style := base
	marker := "  "
	if row.volume == a.disk {
		style = base.Background(tcell.NewHexColor(0x203448))
		a.screen.FillArea(x, y, width, 1, ' ', style)
		marker = "› "
	}
	// Size and meter are pinned to the right so volumes stay comparable down
	// the column however long their mount paths are.
	meter, size := 12, 10
	if width < 40 {
		meter = 0
	}
	nameWidth := width - meter - size
	// A container member's own bytes beat repeating the container's size down
	// every row of the group, which says nothing about the volume itself. The
	// meter then has to measure the same thing the size column names, or the
	// two disagree: the volume's share of its container, not the container's
	// occupancy, which is identical for every member and drawn once in the map.
	capacity, level := v.Total, fraction(v.Total-min(v.Free, v.Total), v.Total)
	if v.Pool != "" && v.Owned > 0 {
		capacity, level = v.Owned, fraction(v.Owned, v.Total)
	}
	a.text(x, y, nameWidth, marker+volumeName(v), style)
	a.text(x+nameWidth, y, size, fmt.Sprintf("%*s", size, Bytes(capacity)), style)
	if meter == 0 {
		return
	}
	if v.Path == "" {
		a.text(x+nameWidth+size+1, y, meter, "unmounted", style.Foreground(muted))
		return
	}
	filled, rest := bar(level, meter-3)
	a.text(x+nameWidth+size+1, y, 1, "▕", style.Foreground(muted))
	a.text(x+nameWidth+size+2, y, meter-3, filled, style.Foreground(fsColor(v.Type)))
	a.text(x+nameWidth+size+2+len([]rune(filled)), y, meter-3, rest, style.Foreground(muted))
	a.text(x+nameWidth+size+meter-1, y, 1, "▏", style.Foreground(muted))
}

// The footer carries the selected volume's detail, so the list can stay one
// line per volume instead of repeating ancestry the reader is not looking at.
func (a *App) drawPickerFooter(w, h int, physical, other []storageDisk) {
	if a.disk < len(a.volumes) {
		v := a.volumes[a.disk]
		detail := fmt.Sprintf("%s · %s · %s", volumeName(v), v.Type, v.Device)
		if v.Path != "" {
			used := v.Total - min(v.Free, v.Total)
			// Say the volume's own bytes first. Its total and usage are the
			// container's, shared with every other volume in it.
			if v.Pool != "" && v.Owned > 0 {
				detail += fmt.Sprintf(" · %s in this volume", Bytes(v.Owned))
			}
			detail += fmt.Sprintf(" · %s total · %s used (%.0f%%) · %s free", Bytes(v.Total), Bytes(used), percent(used, v.Total), Bytes(min(v.Free, v.Total)))
			if v.Pool != "" {
				detail += " · shared with the rest of " + strings.TrimPrefix(v.Pool, "/dev/")
			}
		} else if v.MountIssue != "" {
			detail += " · " + Bytes(v.Total) + " capacity · " + v.MountIssue
		} else {
			detail += " · " + Bytes(v.Total) + " capacity · usage unknown until mounted"
		}
		a.text(2, h-4, w-4, detail, base.Bold(true))
		chain := v.Chain
		if chain == "" {
			chain = "Ancestry unavailable · " + v.Device
		}
		a.text(2, h-3, w-4, strings.ReplaceAll(chain, "/dev/", ""), base.Foreground(muted))
	}
	// With no ancestry anywhere, the map is drawn from mounts standing in for
	// disks. Counting them as disks would claim hardware nothing reported.
	group := "disk"
	if reportedDisks(physical) == 0 {
		group = "mount"
	}
	summary := fmt.Sprintf("%s on %s · shared capacity counted once", plural(len(a.volumes), "volume"), plural(len(physical), group))
	if len(other) > 0 {
		summary = fmt.Sprintf("%s on %s · %s · shared capacity counted once", plural(len(a.volumes), "volume"), plural(len(physical), group), plural(len(other), "other mount"))
	}
	a.text(2, h-5, w-4, summary, base.Foreground(muted))
	a.text(2, h-2, w-4, a.notice, base.Foreground(tcell.ColorYellow))
	a.text(2, h-1, w-4, "↑↓ select · Enter/click explore · double-click a tile · u unmounted · r refresh · ? help · q quit", base.Foreground(accent))
}
