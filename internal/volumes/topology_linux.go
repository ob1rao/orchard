//go:build linux

package volumes

import "context"

func readTopology(ctx context.Context) (map[string]Topology, error) {
	data, err := commandOutput(ctx, "lsblk", "--json", "--bytes", "--paths", "--output", "NAME,TYPE,SIZE")
	if err != nil {
		return nil, err
	}
	return parseLinuxTopology(data)
}
