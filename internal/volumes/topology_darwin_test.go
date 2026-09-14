//go:build darwin

package volumes

import "testing"

func TestAPFSTopology(t *testing.T) {
	report := map[string]any{"AllDisksAndPartitions": []any{
		map[string]any{"DeviceIdentifier": "disk0", "Size": uint64(1000), "Partitions": []any{map[string]any{"DeviceIdentifier": "disk0s2"}}},
		map[string]any{"DeviceIdentifier": "disk3", "APFSPhysicalStores": []any{map[string]any{"DeviceIdentifier": "disk0s2"}}, "APFSVolumes": []any{map[string]any{"DeviceIdentifier": "disk3s1", "MountedSnapshots": []any{map[string]any{"SnapshotBSD": "disk3s1s1"}}}, map[string]any{"DeviceIdentifier": "disk3s5"}}},
	}}
	got := parseDarwinTopology(report)
	for _, id := range []string{"/dev/disk3s1", "/dev/disk3s1s1", "/dev/disk3s5"} {
		v := got[id]
		if v.Disk != "/dev/disk0" || v.DiskSize != 1000 || v.Pool != "/dev/disk3" {
			t.Fatalf("bad APFS ancestry: %+v", v)
		}
	}
}
