//go:build linux

package volumes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Opt-in CI test: only a freshly created loopback image is mounted. Never
// select a host disk. Ordinary go test does not require sudo or mount anything.
func TestMountLoopbackIntegration(t *testing.T) {
	if os.Getenv("ORCHARD_TEST_MOUNT") != "1" {
		t.Skip("set ORCHARD_TEST_MOUNT=1 on an isolated Linux test runner with passwordless sudo")
	}
	run := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	dir := t.TempDir()
	seed := filepath.Join(dir, "seed")
	if err := os.Mkdir(seed, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "readme.txt"), []byte("orchard mount test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(dir, "disk.img")
	f, err := os.Create(image)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(32 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	f.Close()
	run("mkfs.ext4", "-q", "-F", "-d", seed, "-L", "orchard-test", image)
	device := run("sudo", "-n", "losetup", "--find", "--show", image)
	target := filepath.Join(dir, "mounted")
	t.Cleanup(func() {
		_ = exec.Command("sudo", "-n", "umount", target).Run()
		if out, err := exec.Command("sudo", "-n", "losetup", "-d", device).CombinedOutput(); err != nil {
			t.Errorf("detach test loop: %v %s", err, out)
		}
	})
	run("sudo", "-n", "udevadm", "settle")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	found, err := unmounted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var selected Volume
	for _, v := range found {
		if v.Device == device {
			selected = v
			break
		}
	}
	if selected.Device == "" || selected.Type != "ext4" || selected.MountIssue != "" {
		t.Fatalf("new loop filesystem not discovered: %+v", selected)
	}
	mounted, err := Mount(ctx, selected, target)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(mounted, "readme.txt"))
	if err != nil || string(data) != "orchard mount test\n" {
		t.Fatalf("mounted contents unavailable: %q %v", data, err)
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(mounted, &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Flags&unix.ST_RDONLY == 0 {
		t.Fatal("filesystem was not mounted read-only")
	}
	found, err = unmounted(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range found {
		if v.Device == device {
			t.Fatal("mounted device still offered as unmounted")
		}
	}
}
