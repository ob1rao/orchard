//go:build darwin

package scan

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const dirBufSize = 64 << 10

type dirReader struct {
	buf     []byte
	basep   uintptr
	bulk    bool // read metadata with the directory, rather than per entry
	started bool // an entry from this directory has already been emitted
}

func newDirReader() *dirReader { return &dirReader{buf: make([]byte, dirBufSize)} }

func (r *dirReader) open(int) {
	r.basep, r.started, r.bulk = 0, false, bulkUsable()
}

func (r *dirReader) read(ctx context.Context, fd int) chunk {
	if r.bulk {
		c, ordinary := r.readBulk(fd)
		if !ordinary {
			r.started = r.started || len(c.items) > 0
			return c
		}
		// Rewind and list this directory the ordinary way instead.
		r.bulk = false
		if _, err := unix.Seek(fd, 0, 0); err != nil {
			return chunk{err: err, done: true}
		}
		r.basep = 0
	}
	c := r.readdirStat(ctx, fd)
	r.started = r.started || len(c.items) > 0
	return c
}

// readBulk reads one batch with getattrlistbulk. It reports ordinary=true when
// the caller should list the directory the per-entry way instead.
func (r *dirReader) readBulk(fd int) (c chunk, ordinary bool) {
	n, supported, err := bulkRead(fd, r.buf)
	switch {
	case err != nil:
		return chunk{err: err, done: true}, false
	case !supported:
		return r.giveUp("getattrlistbulk is unsupported on this filesystem")
	case n == 0:
		return chunk{done: true}, false
	}
	items, ok := bulkParse(r.buf, n)
	if !ok {
		retireBulk()
		return r.giveUp("getattrlistbulk returned an unreadable record")
	}
	// Prove the record layout once per process before relying on it.
	if bulkState.Load() == 0 {
		if !verifyBulk(fd, items) {
			retireBulk()
			return r.giveUp("getattrlistbulk disagreed with fstatat")
		}
		bulkState.CompareAndSwap(0, 1)
	}
	return chunk{items: items}, false
}

// giveUp falls back to the per-entry path, which is only possible while no
// entry of this directory has been reported yet.
func (r *dirReader) giveUp(why string) (chunk, bool) {
	if r.started {
		return chunk{err: errors.New(why), done: true}, false
	}
	return chunk{}, true
}

// verifyBulk checks a decoded batch against the metadata calls it replaces.
func verifyBulk(fd int, items []item) bool {
	for _, got := range items {
		want, err := readMetadata(fd, got.name)
		if err != nil {
			return false
		}
		if got.dir != want.dir || got.id != want.id || got.links != want.links ||
			got.allocated != want.allocated || got.apparent != want.apparent ||
			got.modified != want.modified || got.created != want.created {
			return false
		}
	}
	return true
}

// readdirStat lists a directory and asks for each entry's metadata separately.
func (r *dirReader) readdirStat(ctx context.Context, fd int) chunk {
	n, err := unix.Getdirentries(fd, r.buf, &r.basep)
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
		if d.Ino == 0 {
			continue
		}
		switch d.Type {
		case unix.DT_DIR, unix.DT_REG, unix.DT_LNK, unix.DT_UNKNOWN:
		default:
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

// direntName reads the name packed into a dirent record. Darwin records their
// length, so the NUL terminator does not have to be hunted for.
func direntName(d *unix.Dirent, reclen int) string {
	n := int(d.Namlen)
	if max := reclen - direntNameOffset; n > max {
		n = max
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(&d.Name[0])), n))
}
