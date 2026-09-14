//go:build linux

package volumes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type blockDevice struct {
	Name, Type, Fstype, Mountpoint, UUID, Label string
	Size                                        json.Number
	Children                                    []blockDevice
}

func unmounted(ctx context.Context) ([]Volume, error) {
	data, err := commandOutput(ctx, "lsblk", "--json", "--bytes", "--paths", "--output", "NAME,TYPE,FSTYPE,SIZE,MOUNTPOINT,UUID,LABEL")
	if err != nil {
		return nil, fmt.Errorf("list unmounted volumes (requires util-linux lsblk): %w", err)
	}
	return parseLSBLK(data)
}

func parseLSBLK(data []byte) ([]Volume, error) {
	var report struct{ Blockdevices []blockDevice }
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode lsblk: %w", err)
	}
	mountedUUID := map[string]bool{}
	var visit func([]blockDevice)
	visit = func(nodes []blockDevice) {
		for _, n := range nodes {
			if n.Mountpoint != "" && n.UUID != "" {
				mountedUUID[n.UUID] = true
			}
			visit(n.Children)
		}
	}
	visit(report.Blockdevices)
	var out []Volume
	seen := map[string]bool{}
	visit = func(nodes []blockDevice) {
		for _, n := range nodes {
			visit(n.Children)
			if len(n.Children) > 0 || n.Mountpoint != "" || mountedUUID[n.UUID] || seen[n.Name] || !strings.HasPrefix(n.Name, "/dev/") {
				continue
			}
			var size uint64
			if _, err := fmt.Sscan(string(n.Size), &size); err != nil {
				continue
			}
			if size == 0 {
				continue
			}
			issue := ""
			switch {
			case n.Fstype == "":
				issue = "No recognized filesystem"
			case n.Fstype == "swap":
				issue = "Swap space cannot be browsed"
			case n.Fstype == "crypto_LUKS" || n.Fstype == "BitLocker":
				issue = "Unlock this volume with system tools first"
			case strings.HasSuffix(n.Fstype, "_member") || n.Fstype == "LVM2_member" || n.Fstype == "bcache":
				issue = "Storage member; activate its volume with system tools first"
			}
			seen[n.Name] = true
			out = append(out, Volume{Device: n.Name, Type: n.Fstype, Total: size, UUID: n.UUID, Label: n.Label, MountIssue: issue})
		}
	}
	visit(report.Blockdevices)
	return out, nil
}

func mountArgs(v Volume, target string) (string, []string) {
	return "mount", []string{"-o", "ro,nosuid,nodev,noexec", "--", v.Device, target}
}
func needsSudo() bool { return true }
