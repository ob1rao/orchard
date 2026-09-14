package volumes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type deviceInfo struct{ os.FileInfo }

func (deviceInfo) Mode() os.FileMode { return os.ModeDevice }

func TestMountLifecycle(t *testing.T) {
	for _, scenario := range []string{"success", "denied", "changed", "wrong-device", "helper-error-after-mount", "verification-error"} {
		t.Run(scenario, func(t *testing.T) {
			v := Volume{Device: "/dev/testdisk", Type: "ext4", Total: 1000, UUID: "identity"}
			target := filepath.Join(t.TempDir(), "new mount ; $(never-a-shell)")
			targetParent, _ := filepath.EvalSymlinks(filepath.Dir(target))
			canonical := filepath.Join(targetParent, filepath.Base(target))
			ran := false
			ops := mountOps{
				euid: 1000,
				discover: func(context.Context) ([]Volume, error) {
					if scenario == "changed" {
						return nil, nil
					}
					return []Volume{v}, nil
				},
				stat: func(string) (os.FileInfo, error) { return deviceInfo{}, nil },
				tool: func(name string) (string, error) { return "/usr/bin/" + name, nil },
				list: func() ([]Volume, error) {
					switch scenario {
					case "denied":
						return nil, nil
					case "verification-error":
						return nil, errors.New("cannot list")
					case "wrong-device":
						return []Volume{{Path: canonical, Device: "/dev/other"}}, nil
					default:
						return []Volume{{Path: canonical, Device: v.Device}}, nil
					}
				},
				run: func(_ context.Context, tool string, args []string) error {
					ran = true
					info, err := os.Stat(target)
					if err != nil || !info.IsDir() {
						t.Fatal("new mountpoint missing before command")
					}
					name, want := mountArgs(v, canonical)
					if runtime.GOOS == "linux" {
						want = append([]string{"--", "/usr/bin/" + name}, want...)
						name = "sudo"
					}
					if tool != "/usr/bin/"+name || !reflect.DeepEqual(args, want) {
						t.Fatalf("incorrect command %s %q", tool, args)
					}
					if scenario == "denied" || scenario == "helper-error-after-mount" {
						return errors.New("permission denied")
					}
					return nil
				},
			}
			path, err := mountWith(context.Background(), v, target, ops)
			success := scenario == "success" || scenario == "helper-error-after-mount"
			if (err == nil) != success {
				t.Fatalf("unexpected mount result %q %v", path, err)
			}
			if success && path != canonical {
				t.Fatal("did not return verified mountpoint")
			}
			if scenario == "changed" && ran {
				t.Fatal("changed volume was mounted")
			}
			_, statErr := os.Stat(target)
			if scenario == "denied" || scenario == "changed" {
				if !os.IsNotExist(statErr) {
					t.Fatal("failed mount left directory")
				}
			} else if statErr != nil {
				t.Fatal("removed potentially mounted directory")
			}
		})
	}
}

func TestNewMountpointRejectsExistingAndRelative(t *testing.T) {
	parent := t.TempDir()
	existing := filepath.Join(parent, "existing")
	if err := os.Mkdir(existing, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "symlink")
	if err := os.Symlink(existing, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative", existing, link, filepath.Join(parent, "missing", "child"), filepath.Join(parent, "bad\npath")} {
		if _, err := NewMountpoint(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	path, err := NewMountpoint(filepath.Join(parent, "new"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0700 {
		t.Fatal("mountpoint not private")
	}
}

func TestMountCommandRequestsReadOnly(t *testing.T) {
	_, args := mountArgs(Volume{Device: "/dev/test"}, "/tmp/test")
	s := strings.Join(args, " ")
	if runtime.GOOS == "linux" && !strings.Contains(s, "ro,nosuid,nodev,noexec") {
		t.Fatal(s)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(s, "mount readOnly -mountPoint") {
		t.Fatal(s)
	}
}

func TestLimitedOutputConcurrent(t *testing.T) {
	var out limitedOutput
	done := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				out.Write([]byte(strings.Repeat("x", 100)))
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("capture stuck")
		}
	}
	if len(out.data) != 8192 {
		t.Fatal("output not bounded")
	}
}
