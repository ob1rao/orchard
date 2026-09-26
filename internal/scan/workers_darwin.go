//go:build darwin

package scan

import (
	"bytes"
	"runtime"

	"golang.org/x/sys/unix"
)

// workersForPath distinguishes network volumes, which the kernel flags
// directly, from local ones. macOS reports no rotation flag through statfs.
func workersForPath(path string) int {
	var st unix.Statfs_t
	if unix.Statfs(path, &st) != nil {
		return 0
	}
	if st.Flags&unix.MNT_LOCAL == 0 {
		return remoteWorkers
	}
	switch fstype(st.Fstypename[:]) {
	case "nfs", "smbfs", "afpfs", "webdav", "ftp":
		return remoteWorkers
	case "devfs":
		return runtime.NumCPU()
	}
	return 0
}

func fstype(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
