package ui

import (
	"fmt"
	"strings"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/treemap"
)

// Anchors survive the size ordering changing as the scanner discovers entries.
// A nil anchor is the first, full-directory page.
type mapPage struct {
	start    *scan.Node
	solo     bool
	selected *scan.Node
}

func (a *App) mapBounds() (int, int) {
	lo, hi := 0, len(a.entries)
	if len(a.mapPages) == 0 {
		return lo, hi
	}
	p := a.mapPages[len(a.mapPages)-1]
	if p.start != nil {
		for i, e := range a.entries {
			if e.Node == p.start {
				lo = i
				break
			}
		}
	}
	if p.solo {
		hi = min(hi, lo+1)
	}
	return lo, hi
}

func (a *App) mapLayout(w, h int) []treemap.Tile {
	lo, hi := a.mapBounds()
	weights := make([]uint64, hi-lo)
	for i := lo; i < hi; i++ {
		weights[i-lo] = a.entries[i].Size
	}
	// Zero-byte entries have no proportional area. A separately labelled final
	// page uses equal tiles, so even empty files remain reachable.
	if len(weights) > 0 && weights[0] == 0 {
		for i := range weights {
			weights[i] = 1
		}
	}
	tiles := treemap.Layout(weights, treemap.Rect{X: 2, Y: 5, W: w - 4, H: h - 10})
	for i := range tiles {
		tiles[i].Index += lo
	}
	return tiles
}

func mapName(e scan.Entry) string {
	name := clean(e.Name)
	if e.Dir {
		name += "/"
	}
	return name
}

func readableTile(t treemap.Tile, e scan.Entry) bool {
	// Reserve the selection marker even when unselected: moving the cursor must
	// not change where the next page begins.
	return t.H >= 4 && t.W-2 >= max(displaywidth.String(mapName(e))+2, displaywidth.String(Bytes(e.Size)))
}

func (a *App) firstClipped(tiles []treemap.Tile) int {
	lo, hi := a.mapBounds()
	readable := make([]bool, hi-lo)
	for _, t := range tiles {
		readable[t.Index-lo] = readableTile(t, a.entries[t.Index])
	}
	for i, ok := range readable {
		if !ok {
			return lo + i
		}
	}
	return hi
}

func (a *App) rememberMapSelection() {
	if len(a.mapPages) > 0 && a.selected < len(a.entries) {
		a.mapPages[len(a.mapPages)-1].selected = a.entries[a.selected].Node
	}
}

func (a *App) nextMapPage() {
	if a.screen == nil {
		return
	}
	w, h := a.screen.Size()
	if w < 50 || h < 16 {
		return
	}
	if len(a.mapPages) == 0 {
		if a.selected < len(a.entries) {
			a.mapReturnSelected = a.entries[a.selected].Node
		}
		a.mapPages = append(a.mapPages, mapPage{})
		a.rememberMapSelection()
		return
	}
	a.rememberMapSelection()
	lo, hi := a.mapBounds()
	next := a.firstClipped(a.mapLayout(w, h))
	solo := false
	if a.mapPages[len(a.mapPages)-1].solo {
		next = hi
	} else if next == lo && hi-lo > 1 {
		// If the first tile is already clipped, give it a whole page before
		// proceeding. This guarantees forward progress for very long names.
		solo = true
	}
	if next >= len(a.entries) || (next == hi && !a.mapPages[len(a.mapPages)-1].solo) || (next == lo && hi-lo <= 1) {
		return
	}
	a.mapPages = append(a.mapPages, mapPage{start: a.entries[next].Node, solo: solo})
	a.selected = next
	a.lastNode = nil
}

func (a *App) previousMapPage() {
	if len(a.mapPages) == 0 {
		return
	}
	p := a.mapPages[len(a.mapPages)-1]
	a.mapPages = a.mapPages[:len(a.mapPages)-1]
	if len(a.mapPages) > 0 {
		p = a.mapPages[len(a.mapPages)-1]
	} else {
		p.selected = a.mapReturnSelected
	}
	for i, e := range a.entries {
		if e.Node == p.selected {
			a.selected = i
			break
		}
	}
	a.lastNode = nil
}

func (a *App) drawFullMap(w, h int) {
	a.listWidth = 0
	a.listTop, a.listHeight = 5, h-10
	lo, hi := a.mapBounds()
	a.selected = max(lo, min(a.selected, max(lo, hi-1)))
	a.tiles = a.mapLayout(w, h)
	title := fmt.Sprintf("FULL MAP · page %d · entries %d–%d of %d", len(a.mapPages), min(lo+1, hi), hi, len(a.entries))
	if lo < hi && a.entries[lo].Size == 0 {
		title += " · zero bytes: equal tiles"
	}
	if a.hideHidden || a.filter != "" {
		title += " · filtered"
	}
	a.text(2, 4, w-4, title, base.Foreground(muted))
	for _, t := range a.tiles {
		a.drawTile(t, a.entries[t.Index], t.Index == a.selected, hi-lo == 1)
	}
	if lo == hi {
		a.text(3, 6, w-6, "No entries here", base.Foreground(muted))
	}
	if a.selected < len(a.entries) {
		e := a.entries[a.selected]
		a.text(2, h-4, w-4, fmt.Sprintf("%s · %s · %.2f%% of directory", e.Node.Path(), Bytes(e.Size), percent(e.Size, a.view.Size)), base.Bold(true))
	}
	a.text(2, h-5, w-4, a.notice, base.Foreground(tcell.ColorYellow))
	hint := "Space next smaller entries"
	if a.firstClipped(a.tiles) == hi && hi == len(a.entries) {
		hint = "Smallest entries reached"
	}
	if hi-lo == 1 && hi == len(a.entries) {
		hint = "Smallest entry reached"
	}
	a.text(2, h-3, w-4, hint+" · b previous view", base.Foreground(accent))
	a.text(2, h-2, w-4, "↑↓ select · Enter / double-click open · ← parent · f find · v view · t tail · ? help", base.Foreground(muted))
}

// An isolated entry can use multiple lines for a long filename, keeping its
// size on a separate row. Truncation still applies if the terminal is too small.
func (a *App) drawSoloName(t treemap.Tile, e scan.Entry, style tcell.Style) {
	name := mapName(e)
	width := t.W - 4
	if width < 1 || t.H < 4 {
		return
	}
	for row := 1; row < t.H-2; row++ {
		part := displaywidth.TruncateString(name, width, "")
		if row == t.H-3 && len(part) < len(name) {
			part = displaywidth.TruncateString(name, width, "…")
		}
		a.text(t.X+2, t.Y+row, width, part, style.Bold(true))
		name = strings.TrimPrefix(name, part)
	}
	a.text(t.X+1, t.Y+t.H-2, t.W-2, Bytes(e.Size), style)
}
