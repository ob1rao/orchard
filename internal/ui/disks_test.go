package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/volumes"
)

type mountScreen struct {
	*testScreen
	suspended, resumed int
}

func (s *mountScreen) Suspend() error                  { s.suspended++; return nil }
func (s *mountScreen) Resume() error                   { s.resumed++; return nil }
func (s *mountScreen) EnableMouse(...tcell.MouseFlags) {}

func TestMountFormCancelAndSubmit(t *testing.T) {
	ctx := context.Background()
	screen := &mountScreen{testScreen: newTestScreen(120, 32)}
	v := volumes.Volume{Device: "/dev/test", Type: "ext4", Total: 1000}
	a := &App{screen: screen, picker: true, volumes: []volumes.Volume{v}}
	key := func(k tcell.Key, s string) { a.key(ctx, tcell.NewEventKey(k, s, tcell.ModNone)) }
	called := false
	target := t.TempDir()
	a.mountVolume = func(_ context.Context, got volumes.Volume, path string) (string, error) {
		called = true
		if got.Device != v.Device || path != target {
			t.Fatalf("wrong mount request: %+v %q", got, path)
		}
		return target, nil
	}
	key(tcell.KeyEnter, "")
	if a.mount == nil || called {
		t.Fatal("select must open form without mounting")
	}
	a.draw()
	if !strings.Contains(string(screen.rows[2]), "MOUNT UNMOUNTED VOLUME") {
		t.Fatal("form missing")
	}
	key(tcell.KeyEscape, "")
	if a.mount != nil || called {
		t.Fatal("cancel triggered mount")
	}
	key(tcell.KeyEnter, "")
	key(tcell.KeyCtrlU, "")
	key(tcell.KeyRune, target)
	key(tcell.KeyEnter, "")
	defer a.stop()
	canonical, _ := filepath.EvalSymlinks(target)
	if !called || a.mount != nil || a.picker || a.current.Path() != canonical {
		t.Fatal("mount success did not start scan")
	}
	if screen.suspended != 1 || screen.resumed != 1 {
		t.Fatal("terminal was not suspended and restored")
	}
}

func TestMountFailureAndUnavailableDevice(t *testing.T) {
	screen := &mountScreen{testScreen: newTestScreen(120, 32)}
	a := &App{screen: screen, picker: true, volumes: []volumes.Volume{{Device: "/dev/locked", MountIssue: "Unlock first"}}}
	a.selectDisk(context.Background())
	if a.mount != nil || a.notice != "Unlock first" {
		t.Fatal("unavailable device offered mounting")
	}
	a.volumes[0].MountIssue = ""
	a.volumes[0].Type = "ext4"
	a.selectDisk(context.Background())
	a.mountVolume = func(context.Context, volumes.Volume, string) (string, error) {
		return "", errors.New("permission denied")
	}
	a.mountKey(context.Background(), tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if a.mount == nil || !strings.Contains(a.mount.err, "permission denied") || !a.picker || screen.resumed != 1 {
		t.Fatal("mount error lost form or terminal")
	}
}

func TestDiskRefreshStaleAndPartialResults(t *testing.T) {
	a := &App{diskSerial: 2, diskLoading: true, volumes: []volumes.Volume{{Device: "/dev/a", Path: "/"}}}
	a.acceptDisks(diskUpdate{serial: 1, err: errors.New("stale")})
	if !a.diskLoading || a.notice != "" {
		t.Fatal("stale refresh applied")
	}
	a.acceptDisks(diskUpdate{serial: 2, volumes: a.volumes, err: errors.New("lsblk unavailable")})
	if a.diskLoading || len(a.volumes) != 1 || a.notice != "lsblk unavailable" {
		t.Fatal("mounted results lost on discovery failure")
	}
}

func TestUnmountedPickerDoesNotInventUsedSpace(t *testing.T) {
	screen := newTestScreen(120, 32)
	a := &App{screen: screen, picker: true, showUnmounted: true, volumes: []volumes.Volume{{Device: "/dev/test", Type: "ext4", Total: 1000000, Label: "Backup"}}}
	a.draw()
	var text strings.Builder
	for _, row := range screen.rows {
		text.WriteString(string(row))
		text.WriteByte('\n')
	}
	if !strings.Contains(text.String(), "unmounted") || !strings.Contains(text.String(), "1.0 MB capacity · usage unknown until mounted") {
		t.Fatal("unmounted capacity rendered as known usage")
	}
	// A gauge or a percentage here would be invented: nothing has measured it.
	if strings.Contains(text.String(), "█") || strings.Contains(text.String(), "% ") {
		t.Fatal("unmounted volume drew a usage level")
	}
}
