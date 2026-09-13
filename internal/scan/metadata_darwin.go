//go:build darwin

package scan

import "golang.org/x/sys/unix"

func readMetadata(fd int, name string) (item, error) {
	var s unix.Stat_t
	if err := unix.Fstatat(fd, name, &s, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return item{}, err
	}
	return item{name: name, dir: s.Mode&unix.S_IFMT == unix.S_IFDIR, mode: uint32(s.Mode), allocated: uint64(max(0, s.Blocks)) * 512, apparent: uint64(max(0, s.Size)), id: identity{uint64(s.Dev), uint64(s.Ino)}, links: uint64(s.Nlink), modified: s.Mtim.Sec, created: s.Btim.Sec, hasCreated: s.Btim.Sec != 0 || s.Btim.Nsec != 0}, nil
}
