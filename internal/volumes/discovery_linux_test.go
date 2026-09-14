//go:build linux

package volumes

import "testing"

func TestLSBLKUnmountedClassification(t *testing.T) {
	data := []byte(`{"blockdevices":[
 {"name":"/dev/sda","size":9000,"children":[
 {"name":"/dev/sda1","type":"part","fstype":"ext4","size":3000,"mountpoint":"/","uuid":"mounted"},
 {"name":"/dev/sda2","type":"part","fstype":"ext4","size":"3000","mountpoint":null,"uuid":"ready","label":"Backup Disk"},
 {"name":"/dev/sda3","type":"part","fstype":"crypto_LUKS","size":3000}]},
 {"name":"/dev/sdb","fstype":null,"size":5000},
 {"name":"/dev/sdc","fstype":"swap","size":4000},
 {"name":"/dev/sdd","fstype":"LVM2_member","size":4000},
 {"name":"/dev/dm-1","fstype":"ext4","size":4000,"uuid":"mounted"},
 {"name":"/dev/sda2","fstype":"ext4","size":3000},
 {"name":"/dev/sr0","size":0}]}`)
	v, err := parseLSBLK(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 5 {
		t.Fatalf("unexpected devices: %+v", v)
	}
	if v[0].Device != "/dev/sda2" || v[0].MountIssue != "" || v[0].Total != 3000 || v[0].Label != "Backup Disk" {
		t.Fatalf("mountable filesystem lost: %+v", v[0])
	}
	for _, d := range v[1:] {
		if d.MountIssue == "" {
			t.Fatalf("unsafe candidate: %+v", d)
		}
	}
	if _, err := parseLSBLK([]byte("not JSON")); err == nil {
		t.Fatal("malformed output accepted")
	}
}
