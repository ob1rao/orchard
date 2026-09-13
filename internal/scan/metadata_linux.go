//go:build linux

package scan

import (
	"errors"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

var noStatx atomic.Bool

// statx returns allocation and birth time in the same metadata call. Older
// kernels and restricted environments fall back without inventing birth times.
func readMetadata(fd int, name string) (item, error) {
	if !noStatx.Load() {
		var s unix.Statx_t
		err := unix.Statx(fd, name, unix.AT_SYMLINK_NOFOLLOW|unix.AT_NO_AUTOMOUNT, unix.STATX_BASIC_STATS|unix.STATX_BTIME, &s)
		if err == nil {
			return item{name: name, dir: s.Mode&unix.S_IFMT == unix.S_IFDIR, mode: uint32(s.Mode), allocated: s.Blocks * 512, apparent: s.Size, id: identity{unix.Mkdev(s.Dev_major, s.Dev_minor), s.Ino}, links: uint64(s.Nlink), modified: s.Mtime.Sec, created: s.Btime.Sec, hasCreated: s.Mask&unix.STATX_BTIME != 0}, nil
		}
		if errors.Is(err, unix.ENOSYS) {
			noStatx.Store(true)
		} else if !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EOPNOTSUPP) && !errors.Is(err, unix.EPERM) {
			return item{}, err
		}
	}
	return readBasicMetadata(fd, name)
}
func readBasicMetadata(fd int, name string) (item, error) {
	var s unix.Stat_t
	if err := unix.Fstatat(fd, name, &s, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return item{}, err
	}
	return item{name: name, dir: s.Mode&unix.S_IFMT == unix.S_IFDIR, mode: uint32(s.Mode), allocated: uint64(max(0, s.Blocks)) * 512, apparent: uint64(max(0, s.Size)), id: identity{uint64(s.Dev), uint64(s.Ino)}, links: uint64(s.Nlink), modified: int64(s.Mtim.Sec)}, nil
}
