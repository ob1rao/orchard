//go:build darwin

package volumes

import (
	"context"
	"fmt"
)

func unmounted(ctx context.Context) ([]Volume, error) {
	data, err := commandOutput(ctx, "diskutil", "list", "-plist")
	if err != nil {
		return nil, err
	}
	report, err := parsePlist(data)
	if err != nil {
		return nil, fmt.Errorf("decode diskutil: %w", err)
	}
	disks, ok := report["AllDisks"].([]any)
	if !ok {
		return nil, fmt.Errorf("diskutil response has no AllDisks array")
	}
	var out []Volume
	for _, disk := range disks {
		id, ok := disk.(string)
		if !ok {
			continue
		}
		data, err = commandOutput(ctx, "diskutil", "info", "-plist", id)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", id, err)
		}
		info, err := parsePlist(data)
		if err != nil {
			return nil, err
		}
		if boolValue(info, "Mounted") || stringValue(info, "MountPoint") != "" {
			continue
		}
		fs := stringValue(info, "FilesystemType")
		// Partition maps and APFS/CoreStorage containers are not mountable volumes.
		content := stringValue(info, "Content")
		if fs == "" && (boolValue(info, "WholeDisk") || content == "Apple_APFS" || content == "Apple_CoreStorage" || content == "GUID_partition_scheme") {
			continue
		}
		size := uintValue(info, "TotalSize")
		if size == 0 {
			size = uintValue(info, "DiskSize")
		}
		if size == 0 {
			continue
		}
		issue := ""
		if fs == "" {
			issue = "No recognized filesystem"
		}
		if boolValue(info, "Locked") || boolValue(info, "APFSVolumeLocked") {
			issue = "Unlock this volume with Disk Utility first"
		}
		out = append(out, Volume{Device: stringValue(info, "DeviceNode"), Type: fs, Total: size, UUID: stringValue(info, "VolumeUUID"), Label: stringValue(info, "VolumeName"), MountIssue: issue})
	}
	return out, nil
}

func mountArgs(v Volume, target string) (string, []string) {
	return "diskutil", []string{"mount", "readOnly", "-mountPoint", target, v.Device}
}
func needsSudo() bool { return false }
