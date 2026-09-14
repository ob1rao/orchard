//go:build darwin

package volumes

import "context"

func readTopology(ctx context.Context) (map[string]Topology, error) {
	data, err := commandOutput(ctx, "diskutil", "list", "-plist")
	if err != nil {
		return nil, err
	}
	report, err := parsePlist(data)
	if err != nil {
		return nil, err
	}
	return parseDarwinTopology(report), nil
}

func parseDarwinTopology(report map[string]any) map[string]Topology {
	out := map[string]Topology{}
	disks, _ := report["AllDisksAndPartitions"].([]any)
	for _, raw := range disks {
		d, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if stores, _ := d["APFSPhysicalStores"].([]any); len(stores) > 0 {
			continue
		}
		id := "/dev/" + stringValue(d, "DeviceIdentifier")
		t := Topology{Disk: id, Chain: id, DiskSize: uintValue(d, "Size")}
		out[id] = t
		parts, _ := d["Partitions"].([]any)
		for _, raw := range parts {
			p, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			child := "/dev/" + stringValue(p, "DeviceIdentifier")
			t.Chain = id + " > " + child
			out[child] = t
		}
	}
	for _, raw := range disks {
		d, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		stores, _ := d["APFSPhysicalStores"].([]any)
		if len(stores) == 0 {
			continue
		}
		pool := "/dev/" + stringValue(d, "DeviceIdentifier")
		t := Topology{Disk: pool, Pool: pool, Chain: pool + " (shared APFS container)"}
		if len(stores) == 1 {
			store, _ := stores[0].(map[string]any)
			if parent, ok := out["/dev/"+stringValue(store, "DeviceIdentifier")]; ok {
				t.Disk = parent.Disk
				t.DiskSize = parent.DiskSize
				t.Chain = parent.Chain + " > " + t.Chain
			}
		} else {
			t.Chain += " (multiple physical stores)"
		}
		out[pool] = t
		children, _ := d["APFSVolumes"].([]any)
		for _, raw := range children {
			child, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id := "/dev/" + stringValue(child, "DeviceIdentifier")
			v := t
			v.Chain += " > " + id
			out[id] = v
			snapshots, _ := child["MountedSnapshots"].([]any)
			for _, raw := range snapshots {
				snapshot, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				id := "/dev/" + stringValue(snapshot, "SnapshotBSD")
				snap := v
				snap.Chain += " > " + id
				out[id] = snap
			}
		}
	}
	return out
}
