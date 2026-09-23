package volumes

// Volumes sharing an APFS container all report the container's occupancy
// through statfs, so statfs alone cannot say which of them holds the bytes.
// diskutil's container report attributes usage to each volume separately.
func parseAPFSUsage(report map[string]any) map[string]uint64 {
	out := map[string]uint64{}
	containers, _ := report["Containers"].([]any)
	for _, raw := range containers {
		container, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		list, _ := container["Volumes"].([]any)
		for _, raw := range list {
			volume, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if id := stringValue(volume, "DeviceIdentifier"); id != "" {
				out["/dev/"+id] = uintValue(volume, "CapacityInUse")
			}
		}
	}
	return out
}
