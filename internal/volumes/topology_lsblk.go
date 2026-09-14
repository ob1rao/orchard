package volumes

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type topologyBlock struct {
	Name, Type string
	Size       json.Number
	Children   []topologyBlock
}

func parseLinuxTopology(data []byte) (map[string]Topology, error) {
	var report struct{ Blockdevices []topologyBlock }
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode lsblk topology: %w", err)
	}
	out := map[string]Topology{}
	var visit func(topologyBlock, Topology)
	visit = func(n topologyBlock, t Topology) {
		if t.Disk == "" {
			t.Disk = n.Name
			t.DiskSize, _ = strconv.ParseUint(string(n.Size), 10, 64)
		}
		if t.Chain != "" {
			t.Chain += " > "
		}
		t.Chain += n.Name
		if old, ok := out[n.Name]; ok && old.Disk != t.Disk {
			// A mapper/RAID node may appear under several physical disks. Do not
			// arbitrarily assign its capacity to one of them.
			t.Disk = old.Disk + " + " + t.Disk
			t.DiskSize = 0
			t.Chain = old.Chain + " | " + t.Chain
		}
		out[n.Name] = t
		// mountinfo commonly uses /dev/mapper aliases for the same device.
		if canonical, err := filepath.EvalSymlinks(n.Name); err == nil && strings.HasPrefix(canonical, "/dev/") {
			out[canonical] = t
		}
		for _, child := range n.Children {
			visit(child, t)
		}
	}
	for _, n := range report.Blockdevices {
		visit(n, Topology{})
	}
	return out, nil
}
