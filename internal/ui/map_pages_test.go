package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/treemap"
)

func pagingApp() *App {
	a := &App{screen: newTestScreen(100, 32)}
	for i, size := range []uint64{1000000000, 1000000, 10000, 100, 10, 1, 0, 0} {
		n := &scan.Node{Name: fmt.Sprintf("entry-%d", i)}
		a.entries = append(a.entries, scan.Entry{Node: n, Name: n.Name, Size: size})
	}
	return a
}

func TestMapPagingReachesEveryEntryAndRetraces(t *testing.T) {
	a := pagingApp()
	a.selected = 2
	a.nextMapPage()
	if lo, hi := a.mapBounds(); lo != 0 || hi != len(a.entries) {
		t.Fatal("first Space must expand all entries")
	}
	seen := make(map[int]bool)
	var pages []mapPage
	for attempts := 0; attempts < 3*len(a.entries); attempts++ {
		pages = append(pages, a.mapPages[len(a.mapPages)-1])
		tiles := a.mapLayout(100, 32)
		for _, tile := range tiles {
			if readableTile(tile, a.entries[tile.Index]) {
				seen[tile.Index] = true
			}
		}
		depth := len(a.mapPages)
		clipped := a.firstClipped(tiles)
		lo, hi := a.mapBounds()
		solo := a.mapPages[depth-1].solo
		a.nextMapPage()
		if len(a.mapPages) == depth {
			break
		}
		next, _ := a.mapBounds()
		if !solo && clipped > lo && clipped < hi && next != clipped {
			t.Fatalf("next page starts %d, want first clipped %d", next, clipped)
		}
	}
	if len(seen) != len(a.entries) {
		t.Fatalf("not every entry became readable: %v", seen)
	}
	for i := len(pages) - 1; i >= 0; i-- {
		p := a.mapPages[len(a.mapPages)-1]
		if p.start != pages[i].start || p.solo != pages[i].solo {
			t.Fatal("back did not retrace pages")
		}
		a.previousMapPage()
	}
	if len(a.mapPages) != 0 || a.selected != 2 {
		t.Fatal("back must restore split view and selection")
	}
}

func TestMapLongNameProgressAndResize(t *testing.T) {
	a := pagingApp()
	a.entries[0].Name = strings.Repeat("界", 85)
	a.nextMapPage()
	a.nextMapPage()
	if !a.mapPages[len(a.mapPages)-1].solo {
		t.Fatal("clipped first entry must get isolated page")
	}
	a.screen = newTestScreen(50, 16)
	a.nextMapPage()
	if lo, _ := a.mapBounds(); lo != 1 {
		t.Fatal("long name trapped paging")
	}
	a.entries[0], a.entries[1] = a.entries[1], a.entries[0]
	if lo, _ := a.mapBounds(); lo != 0 {
		t.Fatal("page did not follow node after scan reordering")
	}
}

func TestMapReadabilityAndOmittedTiles(t *testing.T) {
	e := scan.Entry{Name: "界界", Dir: true, Size: 1000}
	tile := treemap.Tile{Rect: treemap.Rect{W: 9, H: 4}}
	if !readableTile(tile, e) {
		t.Fatal("full label should fit")
	}
	tile.W--
	if readableTile(tile, e) {
		t.Fatal("wide filename marked readable")
	}
	tile.W = 30
	tile.H = 3
	if readableTile(tile, e) {
		t.Fatal("missing size marked readable")
	}
	a := pagingApp()
	a.nextMapPage()
	if a.firstClipped(nil) != 0 {
		t.Fatal("omitted tile must count as clipped")
	}
}

func TestMapKeyboardBoundsAndReset(t *testing.T) {
	a := pagingApp()
	ctx := context.Background()
	key := func(k tcell.Key, s string) { a.key(ctx, tcell.NewEventKey(k, s, tcell.ModNone)) }
	key(tcell.KeyRune, " ")
	key(tcell.KeyRune, " ")
	lo, _ := a.mapBounds()
	if lo == 0 {
		t.Fatal("expected smaller entry page")
	}
	key(tcell.KeyHome, "")
	key(tcell.KeyUp, "")
	if a.selected != lo {
		t.Fatal("cursor escaped current page")
	}
	key(tcell.KeyRune, "b")
	if len(a.mapPages) != 1 {
		t.Fatal("b did not go back")
	}
	// Using a real tree verifies reset during directory navigation/filtering.
	real := testApp(t)
	real.screen = newTestScreen(100, 32)
	real.nextMapPage()
	real.enter(real.entries[0].Node)
	if len(real.mapPages) != 0 {
		t.Fatal("directory navigation retained old page")
	}
	real.nextMapPage()
	real.key(ctx, tcell.NewEventKey(tcell.KeyRune, ".", tcell.ModNone))
	if len(real.mapPages) != 0 {
		t.Fatal("hidden toggle retained old page")
	}
}

func TestFullMapMouseUsesGlobalEntryIndex(t *testing.T) {
	a := pagingApp()
	a.nextMapPage()
	a.nextMapPage()
	a.drawFullMap(100, 32)
	tile := a.tiles[0]
	a.selected = len(a.entries) - 1
	a.mouse(context.Background(), tcell.NewEventMouse(tile.X, tile.Y, tcell.Button1, tcell.ModNone))
	if a.selected != tile.Index {
		t.Fatal("mouse selected index outside paged tile")
	}
}

func TestMapEmptyAndSingleEntryStop(t *testing.T) {
	for _, count := range []int{0, 1} {
		a := pagingApp()
		a.entries = a.entries[:count]
		a.nextMapPage()
		for i := 0; i < 5; i++ {
			a.nextMapPage()
		}
		if len(a.mapPages) != 1 {
			t.Fatal("terminal page kept growing history")
		}
		a.drawFullMap(100, 32)
		a.previousMapPage()
		if len(a.mapPages) != 0 {
			t.Fatal("could not restore empty/single directory")
		}
	}
}

func TestSoloPageEndAndNotice(t *testing.T) {
	a := pagingApp()
	a.entries[0].Name = strings.Repeat("long-name-", 20)
	a.nextMapPage()
	a.nextMapPage()
	a.key(context.Background(), tcell.NewEventKey(tcell.KeyEnd, "", tcell.ModNone))
	if a.selected != 0 {
		t.Fatal("End escaped isolated page")
	}
	a.notice = "Cannot view a directory"
	a.drawFullMap(100, 32)
	screen := a.screen.(*testScreen)
	if !strings.Contains(string(screen.rows[27]), a.notice) {
		t.Fatal("viewer error hidden in full map")
	}
	if !strings.Contains(string(screen.rows[25]), "1.0 GB") {
		t.Fatal("wrapped filename displaced size")
	}
}
