//go:build linux

package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Opt-in CI test: only a freshly created loopback image is mounted. Never
// select a host disk. Ordinary go test does not require sudo or mount anything.
//
// Btrfs numbers every subvolume as its own device while keeping it in one
// mount. Scanning by device alone reported a fraction of the tree and said so
// only through the skipped counter, so this checks the whole figure.
func TestBtrfsSubvolumeIntegration(t *testing.T) {
	if os.Getenv("ORCHARD_TEST_MOUNT") != "1" {
		t.Skip("set ORCHARD_TEST_MOUNT=1 on an isolated Linux test runner with passwordless sudo")
	}
	if _, err := exec.LookPath("mkfs.btrfs"); err != nil {
		t.Skip("btrfs-progs is not installed")
	}
	run := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	dir := t.TempDir()
	image := filepath.Join(dir, "btrfs.img")
	f, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(400 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	run("mkfs.btrfs", "-q", image)
	mount := filepath.Join(dir, "mnt")
	if err := os.Mkdir(mount, 0755); err != nil {
		t.Fatal(err)
	}
	run("sudo", "mount", "-o", "loop", image, mount)
	defer run("sudo", "umount", mount)
	run("sudo", "btrfs", "subvolume", "create", filepath.Join(mount, "sub"))
	run("sudo", "chmod", "-R", "a+rwX", mount)

	const mib = 1 << 20
	write := func(path string, size int) {
		t.Helper()
		if err := os.WriteFile(path, make([]byte, size), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(mount, "top.bin"), 20*mib)
	write(filepath.Join(mount, "sub", "inside.bin"), 50*mib)

	tree, done, err := Start(context.Background(), mount, Options{4})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, done)
	v := tree.Snapshot(tree.Root, true)
	if v.Stats.Files != 2 {
		t.Errorf("files = %d, want 2 (the subvolume was skipped)", v.Stats.Files)
	}
	if want := uint64(70 * mib); v.Stats.Apparent < want {
		t.Errorf("apparent = %d, want at least %d", v.Stats.Apparent, want)
	}
}
