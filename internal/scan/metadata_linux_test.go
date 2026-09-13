//go:build linux

package scan

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	basic, err := readBasicMetadata(unix.AT_FDCWD, path)
	if err != nil {
		t.Fatal(err)
	}
	full, err := readMetadata(unix.AT_FDCWD, path)
	if err != nil {
		t.Fatal(err)
	}
	if basic.hasCreated {
		t.Fatal("fstatat must not manufacture creation time")
	}
	if full.id != basic.id || full.allocated != basic.allocated || full.apparent != basic.apparent || full.modified != basic.modified {
		t.Fatalf("statx/fstatat mismatch: %+v / %+v", full, basic)
	}
	var s unix.Statx_t
	if unix.Statx(unix.AT_FDCWD, path, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BTIME, &s) == nil && s.Mask&unix.STATX_BTIME != 0 {
		if !full.hasCreated || full.created != s.Btime.Sec {
			t.Fatal("available birth time lost")
		}
	}
	noStatx.Store(true)
	defer noStatx.Store(false)
	fallback, err := readMetadata(unix.AT_FDCWD, path)
	if err != nil || fallback != basic {
		t.Fatalf("fallback mismatch: %+v %v", fallback, err)
	}
}
