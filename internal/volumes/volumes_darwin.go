//go:build darwin

package volumes

import "golang.org/x/sys/unix"

func list() ([]Volume, error) {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	buf := make([]unix.Statfs_t, n+16)
	n, err = unix.Getfsstat(buf, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}
	var out []Volume
	for _, s := range buf[:n] {
		if s.Blocks == 0 {
			continue
		}
		out = append(out, Volume{unix.ByteSliceToString(s.Mntfromname[:]), unix.ByteSliceToString(s.Mntonname[:]), unix.ByteSliceToString(s.Fstypename[:]), s.Blocks * uint64(s.Bsize), s.Bfree * uint64(s.Bsize), s.Bavail * uint64(s.Bsize)})
	}
	return out, nil
}
