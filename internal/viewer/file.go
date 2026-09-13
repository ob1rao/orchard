// Package viewer reads bounded pages of regular files without indexing the whole file.
package viewer

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const (
	ChunkSize = 16 * 1024
	PageBytes = 256 * 1024
	MaxRows   = 512
)

type Line struct {
	Offset, Next int64
	Text         string
	Continued    bool
}
type Page struct {
	Lines              []Line
	Offset, Next, Size int64
	EOF                bool
}
type File struct {
	file *os.File
	Path string
	Size int64
}

func Open(path string) (*File, error) {
	// Refuse symlinks, and don't block if a file was replaced with a FIFO.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("symlinks are not followed")
		}
		return nil, err
	}
	f := &File{file: os.NewFile(uintptr(fd), path), Path: path}
	if err = f.Refresh(); err != nil {
		f.Close()
		return nil, err
	}
	var probe [4096]byte
	n, err := f.file.ReadAt(probe[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		f.Close()
		return nil, err
	}
	if bytes.IndexByte(probe[:n], 0) >= 0 {
		f.Close()
		return nil, fmt.Errorf("binary file: text viewing is not supported")
	}
	return f, nil
}
func (f *File) Close() error { return f.file.Close() }
func (f *File) Refresh() error {
	st, err := f.file.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("only regular text files can be viewed")
	}
	f.Size = st.Size()
	return nil
}
func (f *File) ReadPage(offset int64, rows int) (Page, error) {
	offset = max(0, min(offset, f.Size))
	rows = max(1, min(rows, MaxRows))
	page := Page{Offset: offset, Next: offset, Size: f.Size}
	reader := bufio.NewReaderSize(io.NewSectionReader(f.file, offset, min(PageBytes, f.Size-offset)), ChunkSize)
	for len(page.Lines) < rows && page.Next-offset < PageBytes {
		text, err := reader.ReadSlice('\n')
		if bytes.IndexByte(text, 0) >= 0 {
			return page, fmt.Errorf("binary data encountered at byte %d", page.Next)
		}
		if len(text) > 0 {
			next := page.Next + int64(len(text))
			body := bytes.TrimSuffix(text, []byte{'\n'})
			if len(body) < len(text) {
				body = bytes.TrimSuffix(body, []byte{'\r'})
			}
			page.Lines = append(page.Lines, Line{page.Next, next, string(body), errors.Is(err, bufio.ErrBufferFull)})
			page.Next = next
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			if !errors.Is(err, io.EOF) {
				return page, err
			}
			break
		}
	}
	page.EOF = page.Next >= f.Size
	return page, nil
}

// Previous finds the prior line/segment using at most one bounded read. Very
// long lines are navigable in chunks instead of consuming unbounded memory.
func (f *File) Previous(offset int64) (int64, error) {
	offset = max(0, min(offset, f.Size))
	if offset == 0 {
		return 0, nil
	}
	start := max(int64(0), offset-ChunkSize)
	data := make([]byte, offset-start)
	n, err := f.file.ReadAt(data, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	data = data[:n]
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		return start + int64(i) + 1, nil
	}
	return start, nil
}
func (f *File) Back(offset int64, rows int) (int64, error) {
	var err error
	for i := 0; i < min(max(1, rows), MaxRows) && offset > 0; i++ {
		offset, err = f.Previous(offset)
		if err != nil {
			return 0, err
		}
	}
	return offset, nil
}
func (f *File) Tail(rows int) (int64, error) {
	// Limit backwards work to the same byte budget as a forward page.
	offset := f.Size
	for i := 0; i < min(max(1, rows), MaxRows) && offset > 0; i++ {
		previous, err := f.Previous(offset)
		if err != nil {
			return 0, err
		}
		if f.Size-previous > PageBytes {
			break
		}
		offset = previous
	}
	return offset, nil
}
