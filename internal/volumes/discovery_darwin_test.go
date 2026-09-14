//go:build darwin

package volumes

import (
	"context"
	"testing"
	"time"
)

func TestDiskutilDiscovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	v, err := unmounted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, disk := range v {
		if disk.Device == "" || disk.Path != "" || disk.Total == 0 {
			t.Fatalf("invalid unmounted volume: %+v", disk)
		}
	}
}
