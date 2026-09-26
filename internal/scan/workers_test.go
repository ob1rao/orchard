package scan

import (
	"path/filepath"
	"testing"
)

func TestWorkersForStaysInRange(t *testing.T) {
	for _, path := range []string{t.TempDir(), "/", filepath.Join(t.TempDir(), "absent")} {
		if n := WorkersFor(path); n < 1 || n > 64 {
			t.Errorf("WorkersFor(%q) = %d, want 1-64", path, n)
		}
	}
}

// An unidentifiable path must still scan, at the ordinary count.
func TestWorkersForUnknownPathUsesDefault(t *testing.T) {
	if n := WorkersFor(filepath.Join(t.TempDir(), "absent")); n != DefaultWorkers() {
		t.Errorf("WorkersFor(absent) = %d, want %d", n, DefaultWorkers())
	}
}

func TestDefaultWorkersInRange(t *testing.T) {
	if n := DefaultWorkers(); n < 2 || n > 8 {
		t.Errorf("DefaultWorkers = %d, want 2-8", n)
	}
}
