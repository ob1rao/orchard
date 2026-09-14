package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
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
