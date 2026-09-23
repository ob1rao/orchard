//go:build darwin

package volumes

import "golang.org/x/sys/unix"

// BSD statfs has no f_frsize: f_blocks is already counted in f_bsize units.
func spaceFromStatfs(s *unix.Statfs_t) (Space, error) {
	return spaceFromBlocks(s.Blocks, s.Bfree, s.Bavail, uint64(s.Bsize))
}

func ReadSpace(path string) (Space, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return Space{}, err
	}
	return spaceFromStatfs(&s)
}
