package scan

import "testing"

// A btrfs subvolume gets its own device number inside one mount, so the device
// alone cannot say where a filesystem ends.
func TestBoundaryPrefersMountID(t *testing.T) {
	root := boundary{dev: 59, mount: 71, hasMount: true}
	for _, c := range []struct {
		name string
		it   item
		want bool
	}{
		{"same mount, same device", item{id: identity{dev: 59}, mount: 71, hasMount: true}, true},
		{"same mount, subvolume device", item{id: identity{dev: 60}, mount: 71, hasMount: true}, true},
		{"other mount, other device", item{id: identity{dev: 61}, mount: 52, hasMount: true}, false},
		{"other mount reusing the device", item{id: identity{dev: 59}, mount: 52, hasMount: true}, false},
		{"no mount reported falls back to the device", item{id: identity{dev: 59}}, true},
		{"no mount reported, other device", item{id: identity{dev: 60}}, false},
	} {
		if got := root.holds(c.it); got != c.want {
			t.Errorf("%s: holds = %v, want %v", c.name, got, c.want)
		}
	}
}

// Without a mount ID for the scan root, every comparison must use the device.
func TestBoundaryWithoutMountIDUsesDevice(t *testing.T) {
	root := boundary{dev: 59}
	if !root.holds(item{id: identity{dev: 59}, mount: 52, hasMount: true}) {
		t.Error("same device must be held when the root reported no mount")
	}
	if root.holds(item{id: identity{dev: 60}, mount: 71, hasMount: true}) {
		t.Error("other device must be crossed when the root reported no mount")
	}
}
