package volumes

import "testing"

func TestAPFSUsagePerVolume(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><plist version="1.0"><dict>
	<key>Containers</key><array><dict>
		<key>ContainerReference</key><string>disk3</string>
		<key>CapacityFree</key><integer>397000000000</integer>
		<key>Volumes</key><array>
			<dict><key>DeviceIdentifier</key><string>disk3s1</string><key>Name</key><string>Macintosh HD</string><key>CapacityInUse</key><integer>11000000000</integer></dict>
			<dict><key>DeviceIdentifier</key><string>disk3s5</string><key>Name</key><string>Data</string><key>CapacityInUse</key><integer>520000000000</integer></dict>
		</array>
	</dict></array></dict></plist>`)
	report, err := parsePlist(data)
	if err != nil {
		t.Fatal(err)
	}
	usage := parseAPFSUsage(report)
	if usage["/dev/disk3s1"] != 11000000000 || usage["/dev/disk3s5"] != 520000000000 {
		t.Fatalf("lost per-volume usage: %+v", usage)
	}
	if len(usage) != 2 {
		t.Fatalf("invented volumes: %+v", usage)
	}
	// A Mac with no APFS container, or a report shaped differently by a future
	// diskutil, must cost only the extra detail, never the volume list.
	for _, empty := range []map[string]any{{}, {"Containers": []any{}}, {"Containers": "unexpected"}, {"Containers": []any{map[string]any{}}}} {
		if got := parseAPFSUsage(empty); len(got) != 0 {
			t.Fatalf("usage invented from %v: %+v", empty, got)
		}
	}
}
