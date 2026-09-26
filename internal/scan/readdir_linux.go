//go:build linux

package scan

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The kernel packs roughly two thousand entries into a 64 KiB getdents64 call,
// against 256 per call for os.File.ReadDir, and hands back plain bytes rather
// than a DirEntry and a string for every name.
const dirBufSize = 64 << 10

type dirReader struct{ buf []byte }

func newDirReader() *dirReader { return &dirReader{buf: make([]byte, dirBufSize)} }

func (r *dirReader) open(int) {}

func (r *dirReader) read(ctx context.Context, fd int) chunk {
	n, err := unix.Getdents(fd, r.buf)
	if err != nil {
		return chunk{err: err, done: true}
	}
	if n <= 0 {
		return chunk{done: true}
	}
	c := chunk{items: make([]item, 0, countDirents(r.buf[:n]))}
	for off, seen := 0, 0; off < n; seen++ {
		if seen%cancelEvery == 0 && ctx.Err() != nil {
			return chunk{done: true}
		}
		d := (*unix.Dirent)(unsafe.Pointer(&r.buf[off]))
		reclen := int(d.Reclen)
		if reclen < direntNameOffset || off+reclen > n {
			break
		}
		off += reclen
		// A zero inode marks an entry deleted since the buffer was filled.
		if d.Ino == 0 {
			continue
		}
		switch d.Type {
		case unix.DT_DIR, unix.DT_REG, unix.DT_LNK, unix.DT_UNKNOWN:
		default:
			// Sockets, FIFOs and device nodes hold no space worth reporting,
			// and d_type spares them a metadata call.
			continue
		}
		name := direntName(d, reclen)
		if name == "" || name == "." || name == ".." {
			continue
		}
		// Metadata is anchored to the open directory; symlinks are not followed.
		it, err := readMetadata(fd, name)
		if err != nil {
			c.errs = append(c.errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		mode := it.mode & unix.S_IFMT
		if mode != unix.S_IFREG && mode != unix.S_IFDIR && mode != unix.S_IFLNK {
			continue
		}
		c.items = append(c.items, it)
	}
	return c
}

const direntNameOffset = int(unsafe.Offsetof(unix.Dirent{}.Name))

func direntReclen(buf []byte, off int) int {
	if off+direntNameOffset > len(buf) {
		return 0
	}
	return int((*unix.Dirent)(unsafe.Pointer(&buf[off])).Reclen)
}

// direntName reads the NUL-terminated name packed into a dirent record.
func direntName(d *unix.Dirent, reclen int) string {
	b := unsafe.Slice((*byte)(unsafe.Pointer(&d.Name[0])), reclen-direntNameOffset)
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
