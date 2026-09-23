//go:build darwin

package volumes

import "testing"

func TestAPFSTopology(t *testing.T) {
	report := map[string]any{"AllDisksAndPartitions": []any{
		map[string]any{"DeviceIdentifier": "disk0", "Size": uint64(1000), "Partitions": []any{map[string]any{"DeviceIdentifier": "disk0s2"}}},
		map[string]any{"DeviceIdentifier": "disk3", "APFSPhysicalStores": []any{map[string]any{"DeviceIdentifier": "disk0s2"}}, "APFSVolumes": []any{map[string]any{"DeviceIdentifier": "disk3s1", "MountedSnapshots": []any{map[string]any{"SnapshotBSD": "disk3s1s1"}}}, map[string]any{"DeviceIdentifier": "disk3s5"}}},
	}}
	got := parseDarwinTopology(report, map[string]uint64{"/dev/disk3s1": 300, "/dev/disk3s5": 500})
	for _, id := range []string{"/dev/disk3s1", "/dev/disk3s1s1", "/dev/disk3s5"} {
		v := got[id]
		if v.Disk != "/dev/disk0" || v.DiskSize != 1000 || v.Pool != "/dev/disk3" {
			t.Fatalf("bad APFS ancestry: %+v", v)
		}
	}
	// A sealed system volume is mounted from its snapshot, so statfs reports
	// the snapshot device for "/". It must carry the volume's own usage.
	if got["/dev/disk3s1"].Owned != 300 || got["/dev/disk3s1s1"].Owned != 300 || got["/dev/disk3s5"].Owned != 500 {
		t.Fatalf("per-volume usage lost: %+v %+v %+v", got["/dev/disk3s1"], got["/dev/disk3s1s1"], got["/dev/disk3s5"])
	}
	if parseDarwinTopology(report, nil)["/dev/disk3s1"].Pool != "/dev/disk3" {
		t.Fatal("ancestry must survive a missing container report")
	}
}
