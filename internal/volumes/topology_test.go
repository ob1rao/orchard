package volumes

import "testing"

func TestLinuxTopologyPiAndMapper(t *testing.T) {
	data := []byte(`{"blockdevices":[{"name":"/dev/mmcblk0","type":"disk","size":64000,"children":[{"name":"/dev/mmcblk0p1","size":1000},{"name":"/dev/mmcblk0p2","size":63000,"children":[{"name":"/dev/mapper/root","size":62000}]}]},{"name":"/dev/sda","size":128000,"children":[{"name":"/dev/sda1","size":128000}]}]}`)
	got, err := parseLinuxTopology(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"/dev/mmcblk0p1", "/dev/mmcblk0p2", "/dev/mapper/root"} {
		if got[id].Disk != "/dev/mmcblk0" || got[id].DiskSize != 64000 {
			t.Fatalf("lost Pi ancestry: %+v", got[id])
		}
	}
	if got["/dev/mapper/root"].Chain != "/dev/mmcblk0 > /dev/mmcblk0p2 > /dev/mapper/root" {
		t.Fatal("lost intermediate partition")
	}
	if got["/dev/sda1"].DiskSize != 128000 {
		t.Fatal("wrong external capacity")
	}
}

func TestLinuxTopologyMultipleParents(t *testing.T) {
	got, err := parseLinuxTopology([]byte(`{"blockdevices":[{"name":"/dev/a","size":1000,"children":[{"name":"/dev/md0","size":2000}]},{"name":"/dev/b","size":1000,"children":[{"name":"/dev/md0","size":2000}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got["/dev/md0"].Disk != "/dev/a + /dev/b" || got["/dev/md0"].DiskSize != 0 {
		t.Fatalf("multi-disk volume assigned to one disk: %+v", got["/dev/md0"])
	}
}
