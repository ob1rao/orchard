package volumes

import (
	"math"
	"path/filepath"
	"testing"
)

func TestSpaceCountsAndBounds(t *testing.T) {
	s, err := spaceFromBlocks(100, 40, 30, 4096)
	if err != nil || s.Total != 409600 || s.Free != 163840 || s.Available != 122880 {
		t.Fatalf("bad capacity: %+v %v", s, err)
	}
	s, err = spaceFromBlocks(100, 150, 200, 1)
	if err != nil || s.Free != 100 || s.Available != 100 {
		t.Fatal("invalid counts not clamped")
	}
	s, err = spaceFromBlocks(100, 40, math.MaxUint64, 1)
	if err != nil || s.Available != 0 {
		t.Fatal("negative available space was not clamped to zero")
	}
	for _, args := range [][4]uint64{{1, 0, 0, 0}, {0, 0, 0, 1}, {math.MaxUint64, 0, 0, 4096}} {
		if _, err := spaceFromBlocks(args[0], args[1], args[2], args[3]); err == nil {
			t.Fatal("invalid capacity accepted")
		}
	}
}

func TestReadSpaceForScanDirectory(t *testing.T) {
	path := t.TempDir()
	s, err := ReadSpace(path)
	if err != nil || s.Total == 0 || s.Free > s.Total || s.Available > s.Free {
		t.Fatalf("invalid live space: %+v %v", s, err)
	}
	if _, err := ReadSpace(filepath.Join(path, "missing")); err == nil {
		t.Fatal("missing path reported as zero free space")
	}
}
