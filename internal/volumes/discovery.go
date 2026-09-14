package volumes

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// ListAll adds unmounted devices without replacing the fast mounted-only path.
// A discovery failure still returns the mounted volumes.
func ListAll(ctx context.Context) ([]Volume, error) {
	mounted, err := List()
	if err != nil {
		return mounted, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	extra, err := unmounted(ctx)
	if err != nil {
		return mounted, err
	}
	seen := map[string]bool{}
	for _, v := range mounted {
		seen[v.Device] = true
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Device < extra[j].Device })
	for _, v := range extra {
		if !seen[v.Device] {
			mounted = append(mounted, v)
			seen[v.Device] = true
		}
	}
	return mounted, nil
}

// Use system tools, not shell command strings. These paths work on macOS,
// Raspberry Pi OS, and both merged-/usr and traditional Linux installations.
func systemTool(name string) (string, error) {
	for _, dir := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"} {
		path := filepath.Join(dir, name)
		if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s is not installed in a system binary directory", name)
}

func commandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := systemTool(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return data, nil
}
