//go:build linux

package scan

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Not every filesystem magic number reaches x/sys.
const (
	cifsMagic = 0xFF534D42
	smb2Magic = 0xFE534D42
)

// workersForPath reads the filesystem type, then the device's own report of
// whether it rotates. It returns zero when neither says anything useful.
func workersForPath(path string) int {
	var st unix.Statfs_t
	if unix.Statfs(path, &st) != nil {
		return 0
	}
	switch uint32(st.Type) {
	case unix.NFS_SUPER_MAGIC, unix.SMB_SUPER_MAGIC, smb2Magic, cifsMagic,
		unix.V9FS_MAGIC, unix.CEPH_SUPER_MAGIC, unix.AFS_SUPER_MAGIC:
		return remoteWorkers
	case unix.TMPFS_MAGIC, unix.RAMFS_MAGIC:
		// Memory answers immediately, so only cores limit the scan.
		return runtime.NumCPU()
	case unix.FUSE_SUPER_MAGIC:
		return min(DefaultWorkers(), fuseWorkers)
	}
	if rotational(path) {
		return rotatingWorkers
	}
	return 0
}

// rotational asks sysfs whether the block device holding path really spins.
// Filesystems on an anonymous device, btrfs among them, have no sysfs entry
// and go unanswered.
func rotational(path string) bool {
	var st unix.Stat_t
	if unix.Stat(path, &st) != nil {
		return false
	}
	dev := strconv.FormatUint(uint64(unix.Major(uint64(st.Dev))), 10) + ":" +
		strconv.FormatUint(uint64(unix.Minor(uint64(st.Dev))), 10)
	base, err := filepath.EvalSymlinks("/sys/dev/block/" + dev)
	if err != nil {
		return false
	}
	// A partition keeps its queue settings and its bus on the parent disk.
	if _, err := os.Stat(filepath.Join(base, "partition")); err == nil {
		base = filepath.Dir(base)
	}
	if !spinningBus(base) {
		return false
	}
	b, err := os.ReadFile(filepath.Join(base, "queue", "rotational"))
	return err == nil && strings.TrimSpace(string(b)) == "1"
}

// spinningBus reports whether a device sits on a bus where the rotation flag
// carries information. virtio and loop devices declare themselves rotating
// whatever backs them, and device-mapper and md name no bus at all, so
// believing them would halve the worker count on storage that wants more.
func spinningBus(base string) bool {
	sub, err := filepath.EvalSymlinks(filepath.Join(base, "device", "subsystem"))
	if err != nil {
		return false
	}
	switch filepath.Base(sub) {
	case "scsi", "ide":
		return true
	}
	return false
}
