//go:build linux

package scan

import (
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

// Memory-backed filesystems do no I/O, so only cores limit them.
func TestWorkersForTmpfs(t *testing.T) {
	const path = "/dev/shm"
	var st unix.Statfs_t
	if unix.Statfs(path, &st) != nil || uint32(st.Type) != unix.TMPFS_MAGIC {
		t.Skip("no tmpfs at " + path)
	}
	if n := WorkersFor(path); n != min(64, runtime.NumCPU()) {
		t.Errorf("WorkersFor(%s) = %d, want %d", path, n, min(64, runtime.NumCPU()))
	}
}

// virtio and loop devices claim to rotate whatever backs them; sysfs is only
// believed on a bus where a spinning disk can actually be attached.
func TestRotationalIgnoresVirtualDevices(t *testing.T) {
	for _, base := range []string{"/sys/block/loop0", "/sys/block/vda", "/sys/block/dm-0"} {
		if spinningBus(base) {
			t.Errorf("%s reported as a spinning bus", base)
		}
	}
}
