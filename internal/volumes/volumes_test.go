package volumes

import "testing"

func TestMountedVolumes(t *testing.T) {
	v, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) == 0 {
		t.Fatal("no mounted disks found")
	}
	for _, d := range v {
		if d.Path == "" || d.Total == 0 || d.Free > d.Total {
			t.Fatalf("invalid volume: %+v", d)
		}
	}
}
