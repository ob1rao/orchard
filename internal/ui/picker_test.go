package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/treemap"
	"github.com/ob1rao/orchard/internal/volumes"
)

func TestDiskUsageCountsSharedSpaceOnce(t *testing.T) {
	vs := []volumes.Volume{
		{Topology: volumes.Topology{Disk: "disk0", DiskSize: 1000, Pool: "pool"}, Device: "a", Path: "/", Total: 800, Free: 300},
		{Topology: volumes.Topology{Disk: "disk0", DiskSize: 1000, Pool: "pool"}, Device: "b", Path: "/data", Total: 800, Free: 300},
		{Topology: volumes.Topology{Disk: "disk0", DiskSize: 1000}, Device: "c", Total: 200},
	}
	disks := groupStorage(vs)
	if len(disks) != 1 {
		t.Fatalf("one physical disk split into %d", len(disks))
	}
	used, free := disks[0].usage()
	if disks[0].capacity != 1000 || used != 500 || free != 300 {
		t.Fatalf("shared space double counted: %d %d %d", disks[0].capacity, used, free)
	}
	if len(disks[0].pools) != 2 {
		t.Fatalf("shared container not grouped: %+v", disks[0].pools)
	}
}

// Capacity a disk cannot actually contain would draw a volume outside its own
// frame, so an under-reported disk grows to hold what is mounted on it.
func TestDiskCapacityCoversItsVolumes(t *testing.T) {
	disks := groupStorage([]volumes.Volume{
		{Topology: volumes.Topology{Disk: "disk0", DiskSize: 100}, Device: "a", Path: "/", Total: 400, Free: 100},
		{Device: "net", Path: "/mnt", Total: 900, Free: 100},
	})
	for _, d := range disks {
		if d.capacity < 400 {
			t.Fatalf("disk smaller than its volumes: %+v", d)
		}
	}
	if disks[0].key != "disk0" || !disks[0].reported {
		t.Fatal("the disk holding / should come first")
	}
	if disks[1].reported {
		t.Fatal("a mount with no ancestry must not claim a reported disk size")
	}
}

func pickerApp(w, h int, vs ...volumes.Volume) *App {
	a := &App{screen: newTestScreen(w, h), picker: true, volumes: vs}
	a.draw()
	return a
}

func screenText(a *App) string {
	var b strings.Builder
	for _, row := range a.screen.(*testScreen).rows {
		b.WriteString(string(row) + "\n")
	}
	return b.String()
}

// The whole point of the map: two volumes on one physical disk occupy one
// region, and a volume on another disk never appears inside it.
func TestStorageMapGroupsVolumesByPhysicalDisk(t *testing.T) {
	a := pickerApp(120, 36,
		volumes.Volume{Topology: volumes.Topology{Disk: "/dev/sda", DiskSize: 2000, Chain: "/dev/sda > /dev/sda1"}, Device: "/dev/sda1", Path: "/", Type: "ext4", Total: 1200, Free: 400},
		volumes.Volume{Topology: volumes.Topology{Disk: "/dev/sda", DiskSize: 2000, Chain: "/dev/sda > /dev/sda2"}, Device: "/dev/sda2", Path: "/home", Type: "ext4", Total: 700, Free: 100},
		volumes.Volume{Topology: volumes.Topology{Disk: "/dev/sdb", DiskSize: 500, Chain: "/dev/sdb > /dev/sdb1"}, Device: "/dev/sdb1", Path: "/backup", Type: "xfs", Total: 480, Free: 40},
	)
	if len(a.pickerTiles) != 3 {
		t.Fatalf("expected one tile per volume, got %d", len(a.pickerTiles))
	}
	bounds := map[string]treemap.Rect{}
	for _, tile := range a.pickerTiles {
		key := diskKey(a.volumes[tile.volume])
		if tile.W <= 0 || tile.H <= 0 {
			t.Fatalf("empty tile for %s", a.volumes[tile.volume].Path)
		}
		box, ok := bounds[key]
		if !ok {
			bounds[key] = tile.Rect
			continue
		}
		x, y := min(box.X, tile.X), min(box.Y, tile.Y)
		bounds[key] = treemap.Rect{X: x, Y: y, W: max(box.X+box.W, tile.X+tile.W) - x, H: max(box.Y+box.H, tile.Y+tile.H) - y}
	}
	sda, sdb := bounds["/dev/sda"], bounds["/dev/sdb"]
	if sda.X+sda.W > sdb.X && sdb.X+sdb.W > sda.X && sda.Y+sda.H > sdb.Y && sdb.Y+sdb.H > sda.Y {
		t.Fatalf("volumes from different disks overlap: %+v %+v", sda, sdb)
	}
	text := screenText(a)
	for _, want := range []string{"sda · 2.0 KB", "sdb · 500 B", "/home", "/backup"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in map:\n%s", want, text)
		}
	}
}

// A larger disk must draw larger. Comparing tile areas is what a reader
// actually does when deciding which disk holds their space.
func TestStorageMapAreaTracksCapacity(t *testing.T) {
	a := pickerApp(140, 40,
		volumes.Volume{Topology: volumes.Topology{Disk: "/dev/sda", DiskSize: 4000}, Device: "/dev/sda1", Path: "/", Type: "ext4", Total: 4000, Free: 0},
		volumes.Volume{Topology: volumes.Topology{Disk: "/dev/sdb", DiskSize: 1000}, Device: "/dev/sdb1", Path: "/small", Type: "ext4", Total: 1000, Free: 0},
	)
	area := map[string]int{}
	for _, tile := range a.pickerTiles {
		area[a.volumes[tile.volume].Path] = tile.W * tile.H
	}
	if area["/"] == 0 || area["/small"] == 0 {
		t.Fatalf("a volume was not drawn: %v", area)
	}
	if ratio := float64(area["/"]) / float64(area["/small"]); ratio < 3 {
		t.Fatalf("a 4x disk drew only %.1fx the area: %v", ratio, area)
	}
}

// Disks in a comparable range keep their proportion; only a disk too thin to
// draw is floored, and it is paid for by the tallest band.
func TestBandHeightsFloorOnlyWhatIsTooThin(t *testing.T) {
	got := bandHeights([]uint64{4000, 1000}, 30, 6)
	if got[0] != 24 || got[1] != 6 {
		t.Fatalf("comparable disks lost their proportion: %v", got)
	}
	got = bandHeights([]uint64{64_000_000_000, 281_000_000}, 26, 6)
	if got[0] != 20 || got[1] != 6 {
		t.Fatalf("a tiny disk was not floored into view: %v", got)
	}
	if total := got[0] + got[1]; total != 26 {
		t.Fatalf("bands do not fill the map: %d", total)
	}
	if got := bandHeights([]uint64{1, 1, 1}, 4, 6); got[0]+got[1]+got[2] > 4 {
		t.Fatalf("bands overflowed a short map: %v", got)
	}
	if got := bandHeights(nil, 20, 6); len(got) != 0 {
		t.Fatalf("bands invented for no disks: %v", got)
	}
}

// Volumes in one APFS container each report the container's size, so their
// tiles must come from the bytes they alone hold.
func TestSharedContainerSizesVolumesByOwnedBytes(t *testing.T) {
	shared := volumes.Topology{Disk: "/dev/disk0", DiskSize: 1000, Pool: "/dev/disk1"}
	big, small := shared, shared
	big.Owned, small.Owned = 600, 100
	a := pickerApp(120, 36,
		volumes.Volume{Topology: big, Device: "/dev/disk1s1", Path: "/", Type: "apfs", Total: 900, Free: 200},
		volumes.Volume{Topology: small, Device: "/dev/disk1s2", Path: "/System/Volumes/Data", Type: "apfs", Total: 900, Free: 200},
	)
	area := map[string]int{}
	for _, tile := range a.pickerTiles {
		area[a.volumes[tile.volume].Path] = tile.W * tile.H
	}
	if area["/"] <= area["/System/Volumes/Data"] {
		t.Fatalf("container members not sized by their own bytes: %v", area)
	}
	if !strings.Contains(screenText(a), "disk1 · shared") {
		t.Fatalf("shared container not labelled:\n%s", screenText(a))
	}
}

// Without a container report every member reports the container's occupancy,
// so the picker must show the shared bytes rather than invent a split.
func TestSharedContainerWithoutOwnedBytes(t *testing.T) {
	shared := volumes.Topology{Disk: "/dev/disk0", DiskSize: 1000, Pool: "/dev/disk1"}
	a := pickerApp(120, 36,
		volumes.Volume{Topology: shared, Device: "/dev/disk1s1", Path: "/", Type: "apfs", Total: 900, Free: 300},
		volumes.Volume{Topology: shared, Device: "/dev/disk1s2", Path: "/data", Type: "apfs", Total: 900, Free: 300},
	)
	for _, tile := range a.pickerTiles {
		if tile.W*tile.H != 0 {
			t.Fatalf("member drawn at a size nothing reported: %+v", tile)
		}
	}
	if !strings.Contains(screenText(a), "in use") {
		t.Fatalf("shared occupancy not shown:\n%s", screenText(a))
	}
}

func TestPickerScrollingAndAncestry(t *testing.T) {
	for _, size := range [][2]int{{50, 16}, {80, 24}, {120, 32}} {
		var vs []volumes.Volume
		for i := 0; i < 8; i++ {
			vs = append(vs, volumes.Volume{
				Topology: volumes.Topology{Disk: fmt.Sprintf("disk%d", i/2), DiskSize: 1000, Chain: fmt.Sprintf("disk%d > part%d", i/2, i)},
				Device:   fmt.Sprintf("part%d", i), Path: fmt.Sprintf("/volume%d", i), Type: "ext4", Total: 500, Free: 200,
			})
		}
		a := pickerApp(size[0], size[1], vs...)
		for selected := range a.volumes {
			a.disk = selected
			a.draw()
			visible := false
			for y, index := range a.pickerRows {
				if y < 0 || y >= size[1]-5 {
					t.Fatalf("row outside list: %d", y)
				}
				if index == selected {
					visible = true
				}
			}
			if !visible {
				t.Fatalf("selected volume %d hidden at %v", selected, size)
			}
			if text := screenText(a); !strings.Contains(text, a.volumes[selected].Chain) {
				t.Fatalf("missing ancestry at %v: %s", size, text)
			}
		}
	}
}

// Arrow keys must walk the grouped order on screen, not the order volumes
// happened to be discovered in.
func TestPickerStepFollowsTheDrawnOrder(t *testing.T) {
	a := pickerApp(120, 36,
		volumes.Volume{Topology: volumes.Topology{Disk: "/dev/small", DiskSize: 10}, Device: "/dev/small1", Path: "/tiny", Type: "ext4", Total: 10, Free: 5},
		volumes.Volume{Topology: volumes.Topology{Disk: "/dev/big", DiskSize: 900}, Device: "/dev/big1", Path: "/", Type: "ext4", Total: 900, Free: 100},
		volumes.Volume{Topology: volumes.Topology{Disk: "/dev/big", DiskSize: 900}, Device: "/dev/big2", Path: "/home", Type: "ext4", Total: 900, Free: 100},
	)
	a.disk = 1
	var walk []string
	for i := 0; i < 3; i++ {
		walk = append(walk, a.volumes[a.disk].Path)
		a.disk = a.pickerStep(1)
	}
	if strings.Join(walk, ",") != "/,/home,/tiny" {
		t.Fatalf("selection jumped between disks: %v", walk)
	}
	if a.pickerStep(1) != a.disk {
		t.Fatal("selection ran past the end of the list")
	}
}

// Header rows are placed by arithmetic on the terminal height, so a line left
// over from a taller layout shows up as text trailing a shorter one.
func TestPickerHeaderRowsDoNotCollide(t *testing.T) {
	for _, size := range [][2]int{{50, 16}, {60, 20}, {80, 24}, {92, 22}, {120, 36}, {200, 60}} {
		a := pickerApp(size[0], size[1],
			volumes.Volume{Topology: volumes.Topology{Disk: "/dev/sda", DiskSize: 900, Chain: "/dev/sda > /dev/sda1"}, Device: "/dev/sda1", Path: "/", Type: "ext4", Total: 800, Free: 200},
		)
		found := false
		for _, row := range a.screen.(*testScreen).rows {
			line := strings.TrimRight(string(row), " ")
			if !strings.Contains(line, "Choose a volume") {
				continue
			}
			found = true
			if !strings.HasSuffix(line, "u show unmounted") {
				t.Fatalf("at %v another line overdrew the subtitle: %q", size, line)
			}
		}
		if !found {
			t.Fatalf("no subtitle at %v", size)
		}
	}
}

// A map tile is for comparing, so one click only selects; the list is where a
// single click commits. Exploring from the map takes a second click.
func TestPickerMouseSelectsOnTheMapAndExploresFromTheList(t *testing.T) {
	locked := volumes.Volume{
		Topology: volumes.Topology{Disk: "/dev/sdb", DiskSize: 500, Chain: "/dev/sdb > /dev/sdb1"},
		Device:   "/dev/sdb1", Label: "BACKUP", Type: "ext4", Total: 400, MountIssue: "volume is locked",
	}
	a := pickerApp(120, 36,
		volumes.Volume{Topology: volumes.Topology{Disk: "/dev/sda", DiskSize: 900, Chain: "/dev/sda > /dev/sda1"}, Device: "/dev/sda1", Path: "/", Type: "ext4", Total: 800, Free: 300},
		locked,
	)
	click := func(x, y int) {
		a.mouse(context.Background(), tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone))
		a.mouse(context.Background(), tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone))
	}
	var tile pickerTile
	for _, candidate := range a.pickerTiles {
		if a.volumes[candidate.volume].Device == "/dev/sdb1" {
			tile = candidate
		}
	}
	if tile.W < 3 || tile.H < 3 {
		t.Fatalf("no tile to click: %+v", tile)
	}
	click(tile.X+1, tile.Y+1)
	if a.disk != 1 {
		t.Fatalf("clicking a tile did not select it: %d", a.disk)
	}
	if a.notice != "" {
		t.Fatalf("a single click on the map explored: %q", a.notice)
	}
	click(tile.X+1, tile.Y+1)
	if a.notice != "volume is locked" {
		t.Fatalf("a double click on the map did not explore: %q", a.notice)
	}
	a.notice, a.disk = "", 0
	a.draw()
	row := -1
	for y, index := range a.pickerRows {
		if index == 1 {
			row = y
		}
	}
	if row < 0 {
		t.Fatal("the locked volume has no list row")
	}
	click(4, row)
	if a.notice != "volume is locked" {
		t.Fatalf("a single click in the list did not explore: %q", a.notice)
	}
}

// The list and the map occupy the same rows, so a click has to be routed by
// column. With a full list every map row also has a list row behind it.
func TestPickerMapClickIsNotStolenByTheList(t *testing.T) {
	var vs []volumes.Volume
	for i := 0; i < 20; i++ {
		vs = append(vs, volumes.Volume{
			Topology: volumes.Topology{Disk: fmt.Sprintf("/dev/sd%d", i/4), DiskSize: 1000, Chain: "chain"},
			Device:   fmt.Sprintf("/dev/sd%d%d", i/4, i%4), Path: fmt.Sprintf("/v%d", i), Type: "ext4", Total: 200, Free: 50,
		})
	}
	a := pickerApp(140, 40, vs...)
	if len(a.pickerRows) == 0 || len(a.pickerTiles) == 0 {
		t.Fatal("expected both a populated list and a populated map")
	}
	for _, tile := range a.pickerTiles {
		if tile.W < 3 || tile.H < 3 {
			continue
		}
		x, y := tile.X+1, tile.Y+1
		if _, behind := a.pickerRows[y]; !behind {
			continue
		}
		a.disk, a.notice, a.lastPick = 0, "", ""
		a.mouse(context.Background(), tcell.NewEventMouse(x, y, tcell.Button1, tcell.ModNone))
		a.mouse(context.Background(), tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone))
		if a.disk != tile.volume {
			t.Fatalf("a list row behind the map took the click: selected %d, wanted %d", a.disk, tile.volume)
		}
		if a.notice != "" {
			t.Fatalf("clicking the map started a scan: %q", a.notice)
		}
		return
	}
	t.Fatal("no map tile shares a row with the list; the test proves nothing")
}

func TestUsageBarResolvesSmallFractions(t *testing.T) {
	if filled, _ := bar(0, 10); filled != "" {
		t.Fatalf("empty volume drew a bar: %q", filled)
	}
	// A nearly empty volume must still read as occupied rather than as unused.
	if filled, _ := bar(0.001, 10); filled != "▏" {
		t.Fatalf("tiny usage rounded away: %q", filled)
	}
	if filled, rest := bar(1, 10); filled != strings.Repeat("█", 10) || rest != "" {
		t.Fatalf("full volume drew %q%q", filled, rest)
	}
	if filled, _ := bar(0.5, 10); filled != strings.Repeat("█", 5) {
		t.Fatalf("half-full volume drew %q", filled)
	}
	if halves := gaugeHalves(4, 0.5); halves != 4 {
		t.Fatalf("gauge level wrong: %d", halves)
	}
	if gaugeHalves(4, 0.001) != 1 || gaugeHalves(4, 0) != 0 || gaugeHalves(4, 1) != 8 {
		t.Fatal("gauge bounds wrong")
	}
}
