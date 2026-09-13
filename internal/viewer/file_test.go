package viewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func openText(t *testing.T, text string) *File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.log")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
func TestPagesAndBackwardsNavigation(t *testing.T) {
	for _, ending := range []string{"", "\n", "\r\n"} {
		t.Run(fmt.Sprintf("ending-%q", ending), func(t *testing.T) {
			f := openText(t, "one\ntwo\nthree\nfour"+ending)
			p, err := f.ReadPage(0, 2)
			if err != nil || len(p.Lines) != 2 || p.Lines[0].Text != "one" || p.Lines[1].Text != "two" || p.EOF {
				t.Fatalf("first page: %+v %v", p, err)
			}
			second, err := f.ReadPage(p.Next, 2)
			if err != nil || second.Lines[0].Text != "three" || second.Lines[1].Text != "four" || !second.EOF {
				t.Fatalf("last page: %+v %v", second, err)
			}
			start, err := f.Back(second.Offset, 2)
			if err != nil || start != 0 {
				t.Fatalf("backwards: %d %v", start, err)
			}
			start, err = f.Tail(2)
			if err != nil || start != second.Offset {
				t.Fatalf("tail: %d %v", start, err)
			}
		})
	}
}
func TestEmptyAndBlankLines(t *testing.T) {
	f := openText(t, "")
	p, err := f.ReadPage(0, 10)
	if err != nil || !p.EOF || len(p.Lines) != 0 {
		t.Fatalf("empty: %+v %v", p, err)
	}
	f = openText(t, "\n\nlast\n")
	start, err := f.Tail(3)
	if err != nil || start != 0 {
		t.Fatal(start, err)
	}
	p, err = f.ReadPage(start, 3)
	if err != nil || len(p.Lines) != 3 || p.Lines[0].Text != "" || p.Lines[1].Text != "" {
		t.Fatal(p, err)
	}
}
func TestBoundedLongLines(t *testing.T) {
	text := strings.Repeat("x", PageBytes*2) + "\nlast\n"
	f := openText(t, text)
	p, err := f.ReadPage(0, MaxRows)
	if err != nil {
		t.Fatal(err)
	}
	if p.Next > PageBytes || p.EOF || !p.Lines[0].Continued {
		t.Fatalf("page not bounded: %+v", p)
	}
	var offset int64
	for offset < f.Size {
		p, err = f.ReadPage(offset, MaxRows)
		if err != nil {
			t.Fatal(err)
		}
		if p.Next <= offset || p.Next-offset > PageBytes {
			t.Fatal("unbounded/non-progressing read")
		}
		offset = p.Next
	}
}
func TestTailLargeSparseFile(t *testing.T) {
	f := openText(t, strings.Repeat("a", 4096))
	writer, err := os.OpenFile(f.Path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	const size = int64(1 << 30)
	if err = writer.Truncate(size); err != nil {
		t.Fatal(err)
	}
	suffix := "\nlast-one\nlast-two\n"
	if _, err = writer.WriteAt([]byte(suffix), size-int64(len(suffix))); err != nil {
		t.Fatal(err)
	}
	if err = f.Refresh(); err != nil {
		t.Fatal(err)
	}
	offset, err := f.Tail(2)
	if err != nil || offset < size-100 {
		t.Fatalf("tail traversed sparse body: %d %v", offset, err)
	}
	p, err := f.ReadPage(offset, 2)
	if err != nil || len(p.Lines) != 2 || p.Lines[0].Text != "last-one" || p.Lines[1].Text != "last-two" {
		t.Fatal(p, err)
	}
}
func TestRefreshAppendAndTruncate(t *testing.T) {
	f := openText(t, "old\n")
	if err := os.WriteFile(f.Path, []byte("old\nnew\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	offset, err := f.Tail(1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.ReadPage(offset, 1)
	if err != nil || p.Lines[0].Text != "new" {
		t.Fatal(p, err)
	}
	if err := os.Truncate(f.Path, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	p, err = f.ReadPage(offset, 10)
	if err != nil || p.Offset != 0 || !p.EOF {
		t.Fatal(p, err)
	}
}
func TestRejectBinarySpecialAndSymlink(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "binary")
	if err := os.WriteFile(binary, []byte{'a', 0, 'b'}, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{binary, link, fifo, root, filepath.Join(root, "missing")} {
		f, err := Open(path)
		if err == nil {
			f.Close()
			t.Fatalf("accepted unsupported path %s", path)
		}
	}
	f := openText(t, strings.Repeat("x", 8192)+"\x00")
	if _, err := f.ReadPage(0, 10); err == nil {
		t.Fatal("binary data beyond probe not rejected")
	}
}
