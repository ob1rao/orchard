package volumes

import "testing"

func TestDiskutilPlist(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>AllDisks</key><array><string>disk0</string><string>disk0s1</string></array><key>TotalSize</key><integer>10000000000</integer><key>Mounted</key><false/><key>Locked</key><true/><key>VolumeName</key><string>A &amp; B</string></dict></plist>`)
	info, err := parsePlist(data)
	if err != nil {
		t.Fatal(err)
	}
	if uintValue(info, "TotalSize") != 10000000000 || boolValue(info, "Mounted") || !boolValue(info, "Locked") || stringValue(info, "VolumeName") != "A & B" {
		t.Fatalf("bad plist: %+v", info)
	}
	disks := info["AllDisks"].([]any)
	if len(disks) != 2 {
		t.Fatal("lost disk array")
	}
	for _, bad := range []string{"", "<plist><dict>", "<dict><key>Size</key><integer>no</integer></dict>"} {
		if _, err := parsePlist([]byte(bad)); err == nil {
			t.Fatal("accepted malformed plist")
		}
	}
}
