package volumes

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// Topology describes a reported storage ancestry, not one inferred from names.
// Pool identifies shared capacity (e.g. APFS), which must only be counted once.
// Owned is the share of a pool this volume alone holds, when the system
// reports it; it stays zero for a volume that owns its capacity outright.
type Topology struct {
	Disk, Chain, Pool string
	DiskSize, Owned   uint64
}

func WithTopology(ctx context.Context, v []Volume) ([]Volume, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	topology, err := readTopology(ctx)
	if err != nil {
		return v, fmt.Errorf("Storage ancestry unavailable: %w", err)
	}
	for i := range v {
		device := v[i].Device
		if _, ok := topology[device]; !ok {
			if canonical, err := filepath.EvalSymlinks(device); err == nil {
				device = canonical
			}
		}
		if t, ok := topology[device]; ok {
			v[i].Topology = t
		}
	}
	// Keep groups contiguous while preserving the initial root-first order.
	order := map[string]int{}
	for _, volume := range v {
		key := volume.Disk
		if key == "" {
			key = volume.Device
		}
		if _, ok := order[key]; !ok {
			order[key] = len(order)
		}
	}
	sort.SliceStable(v, func(i, j int) bool {
		x, y := v[i].Disk, v[j].Disk
		if x == "" {
			x = v[i].Device
		}
		if y == "" {
			y = v[j].Device
		}
		return order[x] < order[y]
	})
	return v, nil
}
