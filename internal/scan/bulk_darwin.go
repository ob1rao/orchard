//go:build darwin

package scan

import (
	"encoding/binary"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

// getattrlistbulk returns a directory's entries together with their metadata,
// which removes the per-entry metadata call that otherwise dominates a scan.
//
// The reply is a packed record per entry rather than a struct, and the packing
// is not part of any contract, so the first directory a process reads this way
// is checked field by field against fstatat. One disagreement retires the fast
// path for the rest of the process and costs nothing but that check.
const (
	bulkCommon = unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_NAME | unix.ATTR_CMN_DEVID |
		unix.ATTR_CMN_OBJTYPE | unix.ATTR_CMN_CRTIME | unix.ATTR_CMN_MODTIME | unix.ATTR_CMN_FILEID
	bulkFile = unix.ATTR_FILE_LINKCOUNT | unix.ATTR_FILE_ALLOCSIZE | unix.ATTR_FILE_DATALENGTH
	// Every requested field is present in every record, valid or not, so one
	// record layout serves the whole reply.
	bulkOptions = unix.FSOPT_PACK_INVAL_ATTRS

	// fsobj_type_t values worth keeping.
	vreg = 1
	vdir = 2
	vlnk = 5
)

// bulkState is 0 while unproven, 1 once checked against fstatat, -1 once retired.
var bulkState atomic.Int32

func bulkUsable() bool { return bulkState.Load() >= 0 }
func retireBulk()      { bulkState.Store(-1) }

func attrList() unix.Attrlist {
	return unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  bulkCommon,
		Fileattr:    bulkFile,
	}
}

// bulkRead fills buf with one batch of entries. It reports how many entries
// the kernel wrote, or supported=false when this directory cannot serve them.
func bulkRead(fd int, buf []byte) (n int, supported bool, err error) {
	list := attrList()
	r, _, errno := unix.Syscall6(unix.SYS_GETATTRLISTBULK, uintptr(fd),
		uintptr(unsafe.Pointer(&list)), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)), uintptr(bulkOptions), 0)
	if errno != 0 {
		switch errno {
		case unix.ENOTSUP, unix.EINVAL, unix.EPERM:
			return 0, false, nil
		}
		return 0, true, errno
	}
	return int(r), true, nil
}

// bulkParse decodes count packed records into items. It returns ok=false if a
// record is malformed or omits an attribute, leaving the caller to fall back.
func bulkParse(buf []byte, count int) (items []item, ok bool) {
	items = make([]item, 0, count)
	off := 0
	for i := 0; i < count; i++ {
		if off+4 > len(buf) {
			return nil, false
		}
		length := int(binary.LittleEndian.Uint32(buf[off:]))
		if length < 4 || off+length > len(buf) {
			return nil, false
		}
		rec := buf[off : off+length]
		off += length
		it, keep, ok := bulkRecord(rec)
		if !ok {
			return nil, false
		}
		if keep {
			items = append(items, it)
		}
	}
	return items, true
}

// bulkRecord decodes one record. Fields follow the attribute bits in order,
// packed without padding, after the length and the returned-attribute set.
func bulkRecord(rec []byte) (it item, keep, ok bool) {
	c := cursor{buf: rec, off: 4}
	returned := c.attrSet()
	// FSOPT_PACK_INVAL_ATTRS notwithstanding, only trust a record that says it
	// carries everything the layout below assumes.
	if returned.common&bulkCommon != bulkCommon || returned.file&bulkFile != bulkFile {
		return item{}, false, false
	}
	name := c.name()
	dev := uint64(c.u32())
	objType := c.u32()
	created := c.timespec()
	modified := c.timespec()
	ino := c.u64()
	links := uint64(c.u32())
	allocated := c.u64()
	apparent := c.u64()
	if c.bad || name == "" || name == "." || name == ".." {
		return item{}, false, !c.bad
	}
	var mode uint32
	switch objType {
	case vreg:
		mode = unix.S_IFREG
	case vdir:
		mode = unix.S_IFDIR
	case vlnk:
		mode = unix.S_IFLNK
	default:
		// Sockets, FIFOs and device nodes hold no space worth reporting.
		return item{}, false, true
	}
	return item{
		name: name, dir: mode == unix.S_IFDIR, mode: mode,
		allocated: allocated, apparent: apparent,
		id: identity{dev, ino}, links: links,
		modified: modified, created: created, hasCreated: created != 0,
	}, true, true
}

type attrSet struct{ common, vol, dir, file, fork uint32 }

// cursor walks a packed record, refusing to read past its end.
type cursor struct {
	buf []byte
	off int
	bad bool
}

func (c *cursor) take(n int) []byte {
	if c.bad || c.off+n > len(c.buf) {
		c.bad = true
		return make([]byte, n)
	}
	b := c.buf[c.off : c.off+n]
	c.off += n
	return b
}
func (c *cursor) u32() uint32 { return binary.LittleEndian.Uint32(c.take(4)) }
func (c *cursor) u64() uint64 { return binary.LittleEndian.Uint64(c.take(8)) }

// timespec keeps the seconds and discards the nanoseconds, as the tree does.
func (c *cursor) timespec() int64 { s := int64(c.u64()); c.u64(); return s }

func (c *cursor) attrSet() attrSet {
	return attrSet{c.u32(), c.u32(), c.u32(), c.u32(), c.u32()}
}

// name resolves an attrreference_t: an offset from the reference itself and a
// length that counts the terminating NUL.
func (c *cursor) name() string {
	at := c.off
	dataOff := int(int32(c.u32()))
	length := int(c.u32())
	start, end := at+dataOff, at+dataOff+length
	if c.bad || length <= 1 || start < 0 || end > len(c.buf) {
		c.bad = true
		return ""
	}
	return string(c.buf[start : end-1])
}
