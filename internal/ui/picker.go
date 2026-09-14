package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/volumes"
)

func diskKey(v volumes.Volume) string {
	if v.Disk != "" {
		return v.Disk
	}
	return v.Device
}

// Count each filesystem/container once. Unmounted, undiscovered and unallocated
// capacity stays unknown; it must never be painted as filesystem free space.
func diskUsage(vs []volumes.Volume, key string) (total, used, free uint64) {
	pools := map[string]volumes.Volume{}
	for _, v := range vs {
		if diskKey(v) != key {
			continue
		}
		total = max(total, v.DiskSize)
		if v.Path == "" {
			continue
		}
		pool := v.Pool
		if pool == "" {
			pool = v.Device
		}
		old, ok := pools[pool]
		if !ok || v.Total > old.Total {
			pools[pool] = v
		}
	}
	for _, v := range pools {
		if total == 0 {
			continue
		}
		f := min(v.Free, v.Total)
		used += min(v.Total-f, total-min(used, total))
		free += min(f, total-min(free, total))
	}
	used = min(used, total)
	free = min(free, total-used)
	return
}

func barCells(value, scale uint64, width int) int {
	if scale == 0 || value == 0 || width <= 0 {
		return 0
	}
	return min(width, max(1, int(float64(value)/float64(scale)*float64(width)+0.5)))
}

func (a *App) capacityBar(x, y, width int, total, used, free, scale uint64) {
	n := barCells(total, scale, width)
	if n == 0 {
		return
	}
	u := min(n, int(float64(min(used, total))/float64(total)*float64(n)+0.5))
	f := min(n-u, int(float64(min(free, total))/float64(total)*float64(n)+0.5))
	a.text(x, y, n, strings.Repeat("█", u), base.Foreground(accent))
	a.text(x+u, y, n-u, strings.Repeat("░", f), base.Foreground(fg).Background(tcell.NewHexColor(0)))
	a.text(x+u+f, y, n-u-f, strings.Repeat("?", n-u-f), base.Foreground(muted))
}

func (a *App) drawPicker(w, h int) {
	top := 4
	if h >= 28 && w >= 70 {
		art := []string{"    .oOo.     O R C H A R D", "  .oOooOo.    See where your space goes.", "    /|\\       A treemap explorer for your disks", "   _/ \\_"}
		artWidth := w - 4
		if w >= 100 {
			artWidth = 55
		}
		if a.diskLoading && a.brandFrame%2 == 1 {
			art[0] = "    .OoO.     O R C H A R D"
		}
		for i, line := range art {
			a.text(2, 1+i, artWidth, line, base.Foreground(accent).Bold(i == 0))
		}
		if w >= 100 {
			a.drawDiskOverview(w)
		}
		top = 7
	} else {
		a.text(2, 1, w-4, "ORCHARD · your storage, in treemaps", base.Foreground(accent).Bold(true))
	}
	subtitle := "Choose a volume · u show unmounted"
	if a.showUnmounted {
		subtitle = "Choose a volume · mounted + unmounted · u hide unmounted"
	}
	if a.diskLoading {
		subtitle += " · discovering " + []string{".", "o", "O", "o"}[a.brandFrame%4]
	}
	a.text(2, top-2, w-4, subtitle, base)
	a.text(2, top-1, w-4, "Common capacity scale · █ used  ░ free  ? unknown · tiny bars ≥1 cell", base.Foreground(muted))
	a.disk = max(0, min(a.disk, max(0, len(a.volumes)-1)))
	a.offset = max(0, min(a.offset, a.disk))
	bottom := h - 5
	// Each scrolling window repeats its disk header, keeping ancestry in view.
	height := func(lo, hi int) int {
		n := 0
		last := ""
		for i := lo; i <= hi && i < len(a.volumes); i++ {
			key := diskKey(a.volumes[i])
			if i == lo || key != last {
				n += 3
			}
			n += 4
			last = key
		}
		return n
	}
	for a.offset < a.disk && height(a.offset, a.disk) > bottom-top {
		a.offset++
	}
	scale := uint64(0)
	for _, v := range a.volumes {
		scale = max(scale, max(v.DiskSize, v.Total))
	}
	a.pickerRows = map[int]int{}
	y, last, visible := top, "", 0
	for i := a.offset; i < len(a.volumes); i++ {
		v := a.volumes[i]
		key := diskKey(v)
		header := i == a.offset || key != last
		need := 4
		if header {
			need += 3
		}
		if y+need > bottom {
			break
		}
		if header {
			total, used, free := diskUsage(a.volumes, key)
			label := key + " · " + Bytes(total) + " disk"
			if total == 0 {
				label = key + " · physical capacity / ancestry unknown"
			}
			a.text(2, y, w-4, label, base.Bold(true))
			a.capacityBar(6, y+2, w-10, total, used, free, scale)
			summary := fmt.Sprintf("%s used · %s free · %s ?", Bytes(used), Bytes(free), Bytes(total-used-free))
			if total == 0 {
				summary = "Usage shown per volume below"
			}
			a.text(4, y+1, w-6, summary, base.Foreground(muted))
			y += 3
		}
		style := base
		marker := "  "
		if i == a.disk {
			style = base.Background(tcell.NewHexColor(0x203448))
			marker = "› "
			a.screen.FillArea(2, y, w-4, 4, ' ', style)
		}
		name := v.Path
		if name == "" {
			name = v.Label + " · UNMOUNTED"
		}
		a.text(4, y, w-6, marker+name+"  ["+v.Type+"] · "+v.Device, style.Bold(true))
		chain := v.Chain
		if chain == "" {
			chain = "Ancestry unavailable · " + v.Device
		}
		a.text(6, y+1, w-8, strings.ReplaceAll(chain, "/dev/", ""), style.Foreground(muted))
		used, free := uint64(0), uint64(0)
		detail := Bytes(v.Total) + " capacity · usage unknown until mounted"
		if v.Path != "" {
			free = min(v.Free, v.Total)
			used = v.Total - free
			detail = fmt.Sprintf("%s total · %s used · %s free", Bytes(v.Total), Bytes(used), Bytes(free))
			if v.Pool != "" {
				detail += " · shared container, not additive"
			}
		} else if v.MountIssue != "" {
			detail = Bytes(v.Total) + " capacity · " + v.MountIssue
		}
		a.text(6, y+2, w-8, detail, style.Foreground(muted))
		a.capacityBar(6, y+3, w-10, v.Total, used, free, scale)
		for row := y; row < y+4; row++ {
			a.pickerRows[row] = i
		}
		y += 4
		last = key
		visible++
	}
	a.listHeight = max(1, visible)
	if len(a.volumes) == 0 {
		a.text(2, top, w-4, "No volumes found. Try: orchard /path/to/folder", base)
	}
	a.text(2, h-4, w-4, fmt.Sprintf("Volumes %d–%d of %d · disk totals count shared space once", min(a.offset+1, len(a.volumes)), a.offset+visible, len(a.volumes)), base.Foreground(muted))
	a.text(2, h-3, w-4, a.notice, base.Foreground(tcell.ColorYellow))
	a.text(2, h-2, w-4, "↑↓ select · Enter/click explore · u unmounted · r refresh · ? help · q quit", base.Foreground(accent))
}

// A compact overview lets a large system-volume group coexist with a quick
// comparison of the other disks, even before scrolling the volume list.
func (a *App) drawDiskOverview(w int) {
	keys := []string{}
	seen := map[string]bool{}
	scale := uint64(0)
	for _, v := range a.volumes {
		key := diskKey(v)
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
		scale = max(scale, v.DiskSize)
	}
	title := "DISKS · relative capacity"
	if len(keys) > 3 {
		title += fmt.Sprintf(" · +%d below", len(keys)-3)
	}
	a.text(62, 1, w-64, title, base.Foreground(muted))
	for i, key := range keys {
		if i == 3 {
			break
		}
		total, used, free := diskUsage(a.volumes, key)
		label := strings.TrimPrefix(key, "/dev/") + " " + Bytes(total)
		if total == 0 {
			label = strings.TrimPrefix(key, "/dev/") + " ?"
		}
		a.text(62, 2+i, 20, label, base)
		a.capacityBar(83, 2+i, w-85, total, used, free, scale)
	}
}
