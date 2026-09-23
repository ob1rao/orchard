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
		space, err := spaceFromStatfs(&s)
		if err != nil {
			continue
		}
		out = append(out, Volume{Device: unix.ByteSliceToString(s.Mntfromname[:]), Path: unix.ByteSliceToString(s.Mntonname[:]), Type: unix.ByteSliceToString(s.Fstypename[:]), Total: space.Total, Free: space.Free, Available: space.Available})
	}
	return out, nil
}
