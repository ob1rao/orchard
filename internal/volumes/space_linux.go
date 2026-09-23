//go:build linux

package volumes

import "golang.org/x/sys/unix"

// statfs counts f_blocks in f_frsize units. f_bsize is only a preferred I/O
// size, and filesystems such as virtiofs report a much larger one.
func spaceFromStatfs(s *unix.Statfs_t) (Space, error) {
	unit := s.Frsize
	if unit <= 0 {
		unit = s.Bsize
	}
	if unit <= 0 {
		return spaceFromBlocks(0, 0, 0, 0)
	}
	return spaceFromBlocks(s.Blocks, s.Bfree, s.Bavail, uint64(unit))
}

func ReadSpace(path string) (Space, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return Space{}, err
	}
	return spaceFromStatfs(&s)
}
