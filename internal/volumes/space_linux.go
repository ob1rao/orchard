//go:build linux

package volumes

import "golang.org/x/sys/unix"

func ReadSpace(path string) (Space, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return Space{}, err
	}
	unit := s.Frsize
	if unit <= 0 {
		unit = s.Bsize
	}
	if unit <= 0 {
		return spaceFromBlocks(0, 0, 0, 0)
	}
	return spaceFromBlocks(s.Blocks, s.Bfree, s.Bavail, uint64(unit))
}
