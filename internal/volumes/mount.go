package volumes

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

func SuggestedMountpoint(v Volume) string {
	// A new directory in a user-writable parent avoids privileged mkdir and
	// cannot hide existing files. It remains mounted until explicitly unmounted.
	parent, err := os.UserHomeDir()
	if err != nil {
		parent = os.TempDir()
	}
	return filepath.Join(parent, fmt.Sprintf("orchard-%s-%d", filepath.Base(v.Device), time.Now().Unix()))
}

func NewMountpoint(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("use an absolute mountpoint path without control characters")
	}
	path = filepath.Clean(path)
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("mountpoint parent must already exist: %w", err)
	}
	path = filepath.Join(parent, filepath.Base(path))
	if err := os.Mkdir(path, 0700); err != nil {
		return "", fmt.Errorf("choose a new directory in a writable parent: %w", err)
	}
	return path, nil
}

type mountOps struct {
	discover func(context.Context) ([]Volume, error)
	stat     func(string) (os.FileInfo, error)
	list     func() ([]Volume, error)
	tool     func(string) (string, error)
	run      func(context.Context, string, []string) error
	euid     int
}

// Mount only runs after the user submits the mountpoint. It never formats,
// unlocks, repairs, changes fstab, or silently falls back to a writable mount.
func Mount(ctx context.Context, v Volume, target string) (string, error) {
	return mountWith(ctx, v, target, mountOps{unmounted, os.Stat, List, systemTool, runMountCommand, os.Geteuid()})
}

func mountWith(ctx context.Context, v Volume, target string, ops mountOps) (string, error) {
	if v.Path != "" || v.MountIssue != "" || v.Type == "" {
		return "", fmt.Errorf("select an unmounted filesystem")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	current, err := ops.discover(checkCtx)
	cancel()
	if err != nil {
		return "", err
	}
	found := false
	for _, now := range current {
		if now.Device == v.Device && now.UUID == v.UUID && now.Type == v.Type && now.Total == v.Total && now.MountIssue == "" {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("volume changed or is already mounted; refresh the disk list")
	}
	st, err := ops.stat(v.Device)
	if err != nil {
		return "", err
	}
	if st.Mode()&os.ModeDevice == 0 || st.Mode()&os.ModeCharDevice != 0 {
		return "", fmt.Errorf("not a block device: %s", v.Device)
	}
	name, args := mountArgs(v, target)
	tool, err := ops.tool(name)
	if err != nil {
		return "", err
	}
	useSudo := needsSudo() && ops.euid != 0
	sudo := ""
	if useSudo {
		sudo, err = ops.tool("sudo")
		if err != nil {
			return "", fmt.Errorf("mounting requires root or sudo: %w", err)
		}
	}
	target, err = NewMountpoint(target)
	if err != nil {
		return "", err
	}
	_, args = mountArgs(v, target)
	if useSudo {
		args = append([]string{"--", tool}, args...)
		tool = sudo
	}
	err = ops.run(ctx, tool, args)
	// Check actual mounts even after a command error: a helper may have mounted
	// successfully before exiting unsuccessfully. Never delete a mounted target.
	mounted, listErr := ops.list()
	for _, m := range mounted {
		path := m.Path
		if resolved, e := filepath.EvalSymlinks(path); e == nil {
			path = resolved
		}
		if path == target {
			source := m.Device
			if resolved, e := filepath.EvalSymlinks(source); e == nil {
				source = resolved
			}
			device := v.Device
			if resolved, e := filepath.EvalSymlinks(device); e == nil {
				device = resolved
			}
			if source != device {
				return "", fmt.Errorf("another device is mounted at %s; check it with system tools", target)
			}
			return target, nil
		}
	}
	if listErr == nil {
		// Remove only the newly created empty directory.
		_ = os.Remove(target)
	}
	if err != nil {
		return "", fmt.Errorf("mount failed: %w", err)
	}
	if listErr != nil {
		return "", fmt.Errorf("mount command completed, but verification failed; check %s: %w", target, listErr)
	}
	return "", fmt.Errorf("mount command completed but %s is not mounted", target)
}

func runMountCommand(ctx context.Context, tool string, args []string) error {
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Stdin = os.Stdin
	var diagnostic limitedOutput
	cmd.Stdout = io.MultiWriter(os.Stdout, &diagnostic)
	cmd.Stderr = io.MultiWriter(os.Stderr, &diagnostic)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(diagnostic.data)))
	}
	return nil
}

type limitedOutput struct {
	mu   sync.Mutex
	data []byte
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := 8192 - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, p[:min(len(p), remaining)]...)
	}
	return n, nil
}
