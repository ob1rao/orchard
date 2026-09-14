package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/treemap"
	"github.com/ob1rao/orchard/internal/volumes"
)

func TestUnaccountedUsesAllocatedAndDoesNotUnderflow(t *testing.T) {
	s := volumes.Space{Total: 1000, Free: 400, Available: 300}
	for _, c := range []struct {
		allocated, want uint64
		valid           bool
	}{{0, 600, true}, {500, 100, true}, {600, 0, true}, {800, 0, false}} {
		got, ok := unaccounted(s, c.allocated)
		if got != c.want || ok != c.valid {
			t.Fatalf("allocated %d: got %d %v", c.allocated, got, ok)
		}
	}
}

func TestDiskSpaceToggleAndNavigation(t *testing.T) {
	a := testApp(t)
	a.screen = newTestScreen(120, 32)
	a.spaceReady = true
	a.diskSpace = volumes.Space{Total: 1000000, Free: 400000, Available: 300000}
	a.spaceNext = time.Now().Add(time.Hour)
	a.draw()
	before := string(a.screen.(*testScreen).rows[1])
	if !strings.Contains(before, "Disk free 400.0 KB / 1.0 MB") || !strings.Contains(before, "Available 300.0 KB") || !strings.Contains(before, "Outside scan/other") {
		t.Fatalf("missing disk summary: %q", before)
	}
	a.enter(a.entries[0].Node)
	a.apparent = true
	a.refresh()
	a.draw()
	if string(a.screen.(*testScreen).rows[1]) != before {
		t.Fatal("directory or apparent metric changed disk accounting")
	}
	a.nextMapPage()
	a.draw()
	if string(a.screen.(*testScreen).rows[1]) != before {
		t.Fatal("full map hid disk summary")
	}
	key := tcell.NewEventKey(tcell.KeyRune, "i", tcell.ModNone)
	a.key(context.Background(), key)
	a.draw()
	if strings.TrimSpace(string(a.screen.(*testScreen).rows[1])) != "" {
		t.Fatal("i did not hide summary")
	}
	a.key(context.Background(), key)
	a.draw()
	if string(a.screen.(*testScreen).rows[1]) != before {
		t.Fatal("i did not restore summary")
	}
}

func TestDiskSpaceIncompleteAndUnavailable(t *testing.T) {
	screen := newTestScreen(120, 32)
	a := &App{screen: screen, spaceReady: true, diskSpace: volumes.Space{Total: 1000, Free: 100}}
	a.drawDiskSpace(120)
	if !strings.Contains(string(screen.rows[1]), "Unscanned/other ~900 B") {
		t.Fatal("in-progress bytes presented as inaccessible")
	}
	screen.Clear()
	a.view.Stats.Allocated = 1001
	a.drawDiskSpace(120)
	if !strings.Contains(string(screen.rows[1]), "unknown") {
		t.Fatal("overcount reported as zero unknown usage")
	}
	screen.Clear()
	a.spaceErr = errors.New("unmounted")
	a.drawDiskSpace(120)
	if !strings.Contains(string(screen.rows[1]), "Disk space unavailable") {
		t.Fatal("failed statfs displayed stale space")
	}
}

func TestStaleDiskSpaceCannotReplaceNewScan(t *testing.T) {
	a := &App{spaceSerial: 2, spaceLoading: true}
	a.acceptSpace(spaceUpdate{serial: 1, space: volumes.Space{Total: 1}})
	if a.spaceReady || !a.spaceLoading {
		t.Fatal("stale stats replaced current request")
	}
	a.acceptSpace(spaceUpdate{serial: 2, space: volumes.Space{Total: 100}})
	if !a.spaceReady || a.spaceLoading || a.diskSpace.Total != 100 {
		t.Fatal("current stats not accepted")
	}
}

// Capture the fill independently of labels and borders to check the tile color.
type freeTileScreen struct {
	*testScreen
	fills []tcell.Style
}

func (s *freeTileScreen) FillArea(x, y, w, h int, r rune, style tcell.Style) {
	s.fills = append(s.fills, style)
	s.testScreen.FillArea(x, y, w, h, r, style)
}

func TestFreeSpaceTileDefaultColorAndViews(t *testing.T) {
	a := testApp(t)
	screen := &freeTileScreen{testScreen: newTestScreen(120, 32)}
	a.screen = screen
	a.spaceReady = true
	a.diskSpace = volumes.Space{Total: 1000, Free: 400}
	a.spaceNext = time.Now().Add(time.Hour)
	for _, expanded := range []bool{false, true} {
		if expanded {
			a.nextMapPage()
		}
		a.draw()
		var free *treemap.Tile
		for i := range a.tiles {
			if a.tiles[i].Index == freeSpaceTile {
				free = &a.tiles[i]
			}
		}
		if free == nil {
			t.Fatal("default map omitted free space")
		}
		screen.fills = nil
		a.drawMapTile(*free, false)
		background := screen.fills[0].GetBackground()
		if background != tcell.NewHexColor(0x000000) {
			t.Fatalf("free tile background is %v", background)
		}
		current, selected := a.current, a.selected
		for range 2 {
			a.mouse(context.Background(), tcell.NewEventMouse(free.X, free.Y, tcell.Button1, tcell.ModNone))
			a.mouse(context.Background(), tcell.NewEventMouse(free.X, free.Y, tcell.ButtonNone, tcell.ModNone))
		}
		if a.current != current || a.selected != selected {
			t.Fatal("free space selected a filesystem entry")
		}
	}
	a.key(context.Background(), tcell.NewEventKey(tcell.KeyRune, "i", tcell.ModNone))
	a.draw()
	for _, tile := range a.tiles {
		if tile.Index == freeSpaceTile {
			t.Fatal("hidden disk space still mapped")
		}
	}
}

func TestFreeSpaceLayoutCapacityAndUnavailable(t *testing.T) {
	a := &App{spaceReady: true, diskSpace: volumes.Space{Total: 1000, Free: 400}}
	bounds := treemap.Rect{W: 100, H: 20}
	for _, weights := range [][]uint64{{600}, {1}, {100000}, {0}, nil} {
		area := 0
		for _, tile := range a.layoutWithFreeSpace(weights, bounds) {
			if tile.Index == freeSpaceTile {
				area += tile.W * tile.H
			}
		}
		if area != 800 {
			t.Fatalf("free area %d, want 40%% regardless of directory weights", area)
		}
	}
	for _, state := range []string{"loading", "error", "zero total", "full", "hidden"} {
		b := *a
		switch state {
		case "loading":
			b.spaceReady = false
		case "error":
			b.spaceErr = errors.New("unavailable")
		case "zero total":
			b.diskSpace.Total = 0
		case "full":
			b.diskSpace.Free = 0
		case "hidden":
			b.hideDiskSpace = true
		}
		for _, tile := range b.layoutWithFreeSpace([]uint64{600}, bounds) {
			if tile.Index == freeSpaceTile {
				t.Fatalf("%s displayed free tile", state)
			}
		}
	}
}

func TestFreeSpaceTileOnLaterMapPage(t *testing.T) {
	a := testApp(t)
	// A second entry lets the page start at a nonzero index.
	a.entries = append(a.entries, a.entries[0])
	a.entries[0].Node = nil
	a.screen = newTestScreen(120, 32)
	a.spaceReady = true
	a.diskSpace = volumes.Space{Total: 1000, Free: 400}
	a.mapPages = []mapPage{{}, {start: a.entries[1].Node, solo: true}}
	tiles := a.mapLayout(120, 32)
	found := false
	for _, tile := range tiles {
		if tile.Index == freeSpaceTile {
			found = true
		} else if tile.Index != 1 {
			t.Fatalf("page mapped wrong entry: %d", tile.Index)
		}
	}
	if !found {
		t.Fatal("later page omitted disk free tile")
	}
	if got := a.firstClipped(tiles); got != 2 {
		t.Fatalf("free tile affected pagination: %d", got)
	}
}

func TestFreeSpaceDoesNotHideEmptyFilterMessage(t *testing.T) {
	a := testApp(t)
	screen := newTestScreen(120, 32)
	a.screen = screen
	a.spaceReady = true
	a.diskSpace = volumes.Space{Total: 1000, Free: 400}
	a.filter = "no-such-entry"
	a.refresh()
	a.draw()
	text := ""
	for _, row := range screen.rows {
		text += string(row)
	}
	if !strings.Contains(text, "No measurable matches") || !strings.Contains(text, "Disk free") {
		t.Fatal("empty filter must retain both its message and disk free tile")
	}
}
