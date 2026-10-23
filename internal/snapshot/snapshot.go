// Package snapshot saves a finished scan to a portable file and reads it back.
//
// A file is a fixed magic line, a length-prefixed JSON header, and then a gzip
// stream of nodes in pre-order. The header stays uncompressed so a library of
// large snapshots can be listed without inflating any of them.
//
// Each node record is: name length (uvarint), name bytes, a flag byte, the
// allocated and apparent byte totals (uvarint, already including descendants),
// the modification time (varint Unix seconds), the creation time when the
// flags say it exists, and the child count (uvarint). The root's name is the
// absolute path that was scanned, as it is in a live tree.
package snapshot

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/volumes"
)

const magic = "ORCHARD-SNAPSHOT\n"

// Format is the record layout written by this version. Readers reject a newer
// format rather than misreading it.
const Format = 1

const (
	flagDir     = 1
	flagCreated = 2
)

// Limits that keep a damaged or hostile file from exhausting memory before
// it fails to decode.
const (
	maxHeader   = 1 << 20
	maxName     = 1 << 16
	maxChildren = 1 << 26
)

// Meta describes where and when a snapshot was taken.
type Meta struct {
	Format    int           `json:"format"`
	ID        string        `json:"id"`
	Machine   string        `json:"machine"`
	Hostname  string        `json:"hostname"`
	Source    string        `json:"source"`
	Root      string        `json:"root"`
	Label     string        `json:"label,omitempty"`
	ScannedAt time.Time     `json:"scanned_at"`
	Elapsed   time.Duration `json:"elapsed_ns"`
	OS        string        `json:"os"`
	Arch      string        `json:"arch"`
	Version   string        `json:"orchard_version"`
	Stats     Stats         `json:"stats"`
	// Space is the filesystem's capacity when the scan finished, when known.
	Space *volumes.Space `json:"space,omitempty"`
}

// Stats is the persisted part of a scan's counters.
type Stats struct {
	Files       uint64 `json:"files"`
	Directories uint64 `json:"directories"`
	Errors      uint64 `json:"errors"`
	Skipped     uint64 `json:"skipped"`
	Hardlinks   uint64 `json:"hardlinks"`
	Allocated   uint64 `json:"allocated"`
	Apparent    uint64 `json:"apparent"`
	Partial     bool   `json:"partial"`
	LastError   string `json:"last_error,omitempty"`
}

// Snapshot is a decoded file: its header and a browsable tree.
type Snapshot struct {
	Meta Meta
	Tree *scan.Tree
}

// NewID returns a short random identifier. Eight hex digits keep IDs easy to
// type while making a collision within one library vanishingly unlikely.
func NewID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Hostname is the short name this machine reports, or "local".
func Hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "local"
	}
	return h
}

// Describe fills a header for a tree scanned on this machine.
func Describe(t *scan.Tree, scannedAt time.Time, version string) Meta {
	st := t.Stats()
	host := Hostname()
	return Meta{
		Format: Format, ID: NewID(), Machine: host, Hostname: host, Source: "local",
		Root: t.Root.Path(), ScannedAt: scannedAt.UTC(), Elapsed: st.Elapsed,
		OS: runtime.GOOS, Arch: runtime.GOARCH, Version: version,
		Stats: Stats{Files: st.Files, Directories: st.Directories, Errors: st.Errors, Skipped: st.Skipped,
			Hardlinks: st.Hardlinks, Allocated: st.Allocated, Apparent: st.Apparent,
			Partial: st.Cancelled || !st.Done, LastError: st.LastError},
	}
}

// Write encodes the tree. The scan should be finished or stopped: the tree
// is held for reading throughout, which pauses a running scan.
func Write(w io.Writer, meta Meta, t *scan.Tree) error {
	meta.Format = Format
	header, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(w, 64<<10)
	if _, err := bw.WriteString(magic); err != nil {
		return err
	}
	var num [binary.MaxVarintLen64]byte
	if _, err := bw.Write(num[:binary.PutUvarint(num[:], uint64(len(header)))]); err != nil {
		return err
	}
	if _, err := bw.Write(header); err != nil {
		return err
	}
	zw, err := gzip.NewWriterLevel(bw, gzip.BestSpeed)
	if err != nil {
		return err
	}
	enc := encoder{w: bufio.NewWriterSize(zw, 64<<10)}
	t.Read(func(root *scan.Node, _ scan.Stats) { enc.node(root) })
	if enc.err == nil {
		enc.err = enc.w.Flush()
	}
	if enc.err != nil {
		return enc.err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return bw.Flush()
}

type encoder struct {
	w   *bufio.Writer
	buf [binary.MaxVarintLen64]byte
	err error
}

func (e *encoder) uvarint(v uint64) {
	if e.err == nil {
		_, e.err = e.w.Write(e.buf[:binary.PutUvarint(e.buf[:], v)])
	}
}
func (e *encoder) varint(v int64) {
	if e.err == nil {
		_, e.err = e.w.Write(e.buf[:binary.PutVarint(e.buf[:], v)])
	}
}
func (e *encoder) node(n *scan.Node) {
	e.uvarint(uint64(len(n.Name)))
	if e.err == nil {
		_, e.err = e.w.WriteString(n.Name)
	}
	var flags byte
	if n.Dir {
		flags |= flagDir
	}
	if n.HasCreated {
		flags |= flagCreated
	}
	if e.err == nil {
		e.err = e.w.WriteByte(flags)
	}
	e.uvarint(n.Allocated)
	e.uvarint(n.Apparent)
	e.varint(n.Modified)
	if n.HasCreated {
		e.varint(n.Created)
	}
	e.uvarint(uint64(len(n.Children)))
	for _, c := range n.Children {
		if e.err != nil {
			return
		}
		e.node(c)
	}
}

// ReadMeta decodes only the header.
func ReadMeta(r io.Reader) (Meta, error) {
	meta, _, err := readHeader(bufio.NewReader(r))
	return meta, err
}

// ReadMetaFile decodes the header of a snapshot file.
func ReadMetaFile(path string) (Meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return Meta{}, err
	}
	defer f.Close()
	return ReadMeta(f)
}

func readHeader(br *bufio.Reader) (Meta, *bufio.Reader, error) {
	var meta Meta
	head := make([]byte, len(magic))
	if _, err := io.ReadFull(br, head); err != nil || string(head) != magic {
		return meta, nil, errors.New("not an Orchard snapshot")
	}
	size, err := binary.ReadUvarint(br)
	if err != nil || size > maxHeader {
		return meta, nil, errors.New("snapshot header is damaged")
	}
	header := make([]byte, size)
	if _, err := io.ReadFull(br, header); err != nil {
		return meta, nil, errors.New("snapshot header is truncated")
	}
	if err := json.Unmarshal(header, &meta); err != nil {
		return meta, nil, fmt.Errorf("snapshot header: %w", err)
	}
	if meta.Format > Format {
		return meta, nil, fmt.Errorf("snapshot format %d is newer than this Orchard reads (%d); upgrade Orchard", meta.Format, Format)
	}
	return meta, br, nil
}

// Read decodes a whole snapshot.
func Read(r io.Reader) (*Snapshot, error) {
	meta, br, err := readHeader(bufio.NewReaderSize(r, 64<<10))
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(br)
	if err != nil {
		return nil, fmt.Errorf("snapshot body: %w", err)
	}
	defer zr.Close()
	d := decoder{r: bufio.NewReaderSize(zr, 64<<10)}
	root := d.node(nil)
	if d.err != nil {
		return nil, fmt.Errorf("snapshot body: %w", d.err)
	}
	st := meta.Stats
	tree := scan.Restore(root, scan.Stats{Files: st.Files, Directories: st.Directories, Errors: st.Errors,
		Skipped: st.Skipped, Hardlinks: st.Hardlinks, Cancelled: st.Partial, LastError: st.LastError, Elapsed: meta.Elapsed})
	return &Snapshot{Meta: meta, Tree: tree}, nil
}

// ReadFile decodes a snapshot file.
func ReadFile(path string) (*Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(f)
}

// Decode reads a snapshot held in memory, such as one streamed from a remote
// machine.
func Decode(b []byte) (*Snapshot, error) { return Read(bytes.NewReader(b)) }

type decoder struct {
	r   *bufio.Reader
	err error
}

func (d *decoder) uvarint() uint64 {
	if d.err != nil {
		return 0
	}
	v, err := binary.ReadUvarint(d.r)
	d.err = err
	return v
}
func (d *decoder) varint() int64 {
	if d.err != nil {
		return 0
	}
	v, err := binary.ReadVarint(d.r)
	d.err = err
	return v
}
func (d *decoder) node(parent *scan.Node) *scan.Node {
	size := d.uvarint()
	if d.err != nil {
		return nil
	}
	if size > maxName {
		d.err = errors.New("entry name too long")
		return nil
	}
	name := make([]byte, size)
	if _, err := io.ReadFull(d.r, name); err != nil {
		d.err = err
		return nil
	}
	flags, err := d.r.ReadByte()
	if err != nil {
		d.err = err
		return nil
	}
	n := &scan.Node{Name: string(name), Parent: parent, Dir: flags&flagDir != 0, HasCreated: flags&flagCreated != 0}
	n.Allocated = d.uvarint()
	n.Apparent = d.uvarint()
	n.Modified = d.varint()
	if n.HasCreated {
		n.Created = d.varint()
	}
	count := d.uvarint()
	if d.err != nil {
		return nil
	}
	if count > maxChildren || count > 0 && !n.Dir {
		d.err = errors.New("entry has an impossible child count")
		return nil
	}
	if count > 0 {
		// Cap the preallocation: the count is untrusted until the children
		// actually arrive.
		n.Children = make([]*scan.Node, 0, min(count, 4096))
	}
	for i := uint64(0); i < count; i++ {
		c := d.node(n)
		if d.err != nil {
			return nil
		}
		n.Children = append(n.Children, c)
	}
	return n
}
