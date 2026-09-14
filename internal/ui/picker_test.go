package ui

import (
	"fmt"
	"github.com/ob1rao/orchard/internal/volumes"
	"strings"
	"testing"
)

func TestDiskUsageCountsSharedSpaceOnce(t *testing.T) {
	vs := []volumes.Volume{
		{Topology: volumes.Topology{Disk: "disk0", DiskSize: 1000, Pool: "pool"}, Device: "a", Path: "/", Total: 800, Free: 300},
		{Topology: volumes.Topology{Disk: "disk0", DiskSize: 1000, Pool: "pool"}, Device: "b", Path: "/data", Total: 800, Free: 300},
		{Topology: volumes.Topology{Disk: "disk0", DiskSize: 1000}, Device: "c", Total: 200},
	}
	total, used, free := diskUsage(vs, "disk0")
	if total != 1000 || used != 500 || free != 300 {
		t.Fatalf("shared space double counted: %d %d %d", total, used, free)
	}
	if barCells(500, 1000, 60) != 30 || barCells(1000, 1000, 60) != 60 {
		t.Fatal("disk sizes not proportional")
	}
}

func TestPickerScrollingAndAncestry(t *testing.T) {
	for _, size := range [][2]int{{50, 16}, {80, 24}, {120, 32}} {
		screen := newTestScreen(size[0], size[1])
		a := &App{screen: screen, picker: true}
		for i := 0; i < 8; i++ {
			a.volumes = append(a.volumes, volumes.Volume{Topology: volumes.Topology{Disk: fmt.Sprintf("disk%d", i/2), DiskSize: 1000, Chain: fmt.Sprintf("disk%d > part%d", i/2, i)}, Device: fmt.Sprintf("part%d", i), Path: fmt.Sprintf("/volume%d", i), Total: 500, Free: 200})
		}
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
			text := ""
			for _, row := range screen.rows {
				text += string(row) + "\n"
			}
			if !strings.Contains(text, a.volumes[selected].Chain) {
				t.Fatalf("missing ancestry at %v: %s", size, text)
			}
		}
	}
}
