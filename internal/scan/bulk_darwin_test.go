//go:build darwin

package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// The packed reply layout is an assumption, not a contract. This is the test
// that checks it: every field getattrlistbulk reports must equal what fstatat
// reports for the same entry. A failure here means the fast path is wrong, not
// merely unavailable, and bulkRecord needs revisiting.
func TestBulkMatchesFstatat(t *testing.T) {
	root := fixture(t)
	if err := os.Mkdir(filepath.Join(root, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)

	buf := make([]byte, dirBufSize)
	seen := 0
	for {
		n, supported, err := bulkRead(fd, buf)
		if !supported {
			t.Skip("getattrlistbulk is unsupported on this filesystem")
		}
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
		items, ok := bulkParse(buf, n)
		if !ok {
			t.Fatal("bulkParse rejected a reply the kernel accepted")
		}
		for _, got := range items {
			want, err := readMetadata(fd, got.name)
			if err != nil {
				t.Fatalf("%s: %v", got.name, err)
			}
			if got.dir != want.dir || got.id != want.id || got.links != want.links {
				t.Errorf("%s: identity %+v, want %+v", got.name, got, want)
			}
			if got.allocated != want.allocated || got.apparent != want.apparent {
				t.Errorf("%s: sizes allocated=%d apparent=%d, want %d and %d",
					got.name, got.allocated, got.apparent, want.allocated, want.apparent)
			}
			if got.modified != want.modified || got.created != want.created {
				t.Errorf("%s: times modified=%d created=%d, want %d and %d",
					got.name, got.modified, got.created, want.modified, want.created)
			}
			seen++
		}
	}
	if seen == 0 {
		t.Fatal("the fixture reported no entries")
	}
}

// A scan on macOS should keep the fast path, not quietly fall back to it.
func TestBulkSurvivesAScan(t *testing.T) {
	bulkState.Store(0)
	tree, done, err := Start(context.Background(), fixture(t), Options{4})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, done)
	if tree.Snapshot(tree.Root, false).Stats.Errors != 0 {
		t.Error("the scan reported errors")
	}
	if bulkState.Load() < 0 {
		t.Error("getattrlistbulk was retired during an ordinary scan")
	}
}

// A truncated or nonsensical reply must be refused rather than read past.
func TestBulkParseRejectsMalformedRecords(t *testing.T) {
	for name, buf := range map[string][]byte{
		"empty":            {},
		"length only":      {8, 0, 0, 0},
		"length too small": {1, 0, 0, 0, 0, 0, 0, 0},
		"length past end":  {200, 0, 0, 0, 0, 0, 0, 0},
	} {
		if _, ok := bulkParse(buf, 1); ok {
			t.Errorf("%s: accepted", name)
		}
	}
}
