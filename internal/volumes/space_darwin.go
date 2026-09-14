//go:build darwin

package volumes

import "golang.org/x/sys/unix"

func ReadSpace(path string) (Space, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return Space{}, err
	}
	return spaceFromBlocks(s.Blocks, s.Bfree, s.Bavail, uint64(s.Bsize))
}
