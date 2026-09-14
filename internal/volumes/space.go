package volumes

import (
	"fmt"
	"math"
)

// Space describes filesystem capacity, not a directory's apparent file sizes.
type Space struct{ Total, Free, Available uint64 }

func spaceFromBlocks(blocks, free, available, unit uint64) (Space, error) {
	if unit == 0 || blocks == 0 || blocks > math.MaxUint64/unit {
		return Space{}, fmt.Errorf("filesystem capacity unavailable")
	}
	// Some kernels encode negative availability (reserved blocks exhausted)
	// in the unsigned statfs field. That means no user-available space.
	if available > math.MaxInt64 {
		available = 0
	}
	free = min(free, blocks)
	available = min(available, free)
	return Space{blocks * unit, free * unit, available * unit}, nil
}
