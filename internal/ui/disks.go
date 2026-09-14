package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/volumes"
)

type diskUpdate struct {
	serial  uint64
	volumes []volumes.Volume
	err     error
}
type mountForm struct {
	volume    volumes.Volume
	path, err string
}

func (a *App) reloadDisks(ctx context.Context) {
	if a.diskCancel != nil {
		a.diskCancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	a.diskCancel = cancel
	if a.diskResults == nil {
		a.diskResults = make(chan diskUpdate, 1)
	}
	a.diskSerial++
	serial := a.diskSerial
	results := a.diskResults
	a.diskLoading = true
	all := a.showUnmounted
	go func() {
		var v []volumes.Volume
		var err error
		if all {
			v, err = volumes.ListAll(ctx)
		} else {
			v, err = volumes.List()
		}
		select {
		case results <- diskUpdate{serial, v, err}:
		case <-ctx.Done():
		}
	}()
}

func (a *App) acceptDisks(update diskUpdate) {
	if update.serial != a.diskSerial {
		return
	}
	var selected volumes.Volume
	if a.disk < len(a.volumes) {
		selected = a.volumes[a.disk]
	}
	a.volumes = update.volumes
	a.diskLoading = false
	a.notice = ""
	a.disk = min(a.disk, max(0, len(a.volumes)-1))
	for i, v := range a.volumes {
		if v.Device == selected.Device && v.Path == selected.Path {
			a.disk = i
			break
		}
	}
	if update.err != nil {
		a.notice = update.err.Error()
	}
}

func (a *App) selectDisk(ctx context.Context) {
	if a.disk >= len(a.volumes) {
		return
	}
	v := a.volumes[a.disk]
	if v.Path != "" {
		_ = a.start(ctx, v.Path)
		return
	}
	if v.MountIssue != "" {
		a.notice = v.MountIssue
		return
	}
	a.mount = &mountForm{volume: v, path: volumes.SuggestedMountpoint(v)}
	a.notice = ""
}

func (a *App) mountKey(ctx context.Context, e *tcell.EventKey) {
	f := a.mount
	switch e.Key() {
	case tcell.KeyEscape:
		a.mount = nil
		a.mouseDown = false
	case tcell.KeyCtrlL:
		a.screen.Sync()
	case tcell.KeyCtrlU:
		f.path = ""
		f.err = ""
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		r := []rune(f.path)
		if len(r) > 0 {
			f.path = string(r[:len(r)-1])
		}
		f.err = ""
	case tcell.KeyEnter:
		if f.path == "" {
			f.err = "Enter an absolute path for a new mountpoint"
			return
		}
		if err := a.screen.Suspend(); err != nil {
			f.err = err.Error()
			return
		}
		mount := a.mountVolume
		if mount == nil {
			mount = volumes.Mount
		}
		fmt.Printf("Orchard: mount %s read-only at %s\n", clean(f.volume.Device), clean(f.path))
		path, err := mount(ctx, f.volume, f.path)
		if resumeErr := a.screen.Resume(); resumeErr != nil {
			a.fatal = fmt.Errorf("restore terminal after mounting: %w", resumeErr)
			return
		}
		a.screen.EnableMouse(tcell.MouseButtonEvents)
		a.mouseDown = false
		if err != nil {
			f.err = err.Error()
			return
		}
		a.mount = nil
		if err = a.start(ctx, path); err != nil {
			a.notice = fmt.Sprintf("Mounted at %s, but scan failed: %v", path, err)
		}
	case tcell.KeyRune:
		if len(f.path)+len(e.Str()) <= 4096 && strings.IndexFunc(e.Str(), unicode.IsControl) < 0 {
			f.path += e.Str()
			f.err = ""
		}
	}
}

func (a *App) drawMount(w, h int) {
	f := a.mount
	a.text(2, 2, w-4, "MOUNT UNMOUNTED VOLUME", base.Foreground(accent).Bold(true))
	a.text(2, 4, w-4, fmt.Sprintf("%s · %s · %s · %s", f.volume.Device, f.volume.Label, f.volume.Type, Bytes(f.volume.Total)), base)
	a.text(2, 6, w-4, "New mountpoint (absolute path; parent must exist and be writable):", base)
	path := clean(f.path)
	for displaywidth.String(path) > w-7 {
		r := []rune(path)
		path = string(r[1:])
	}
	a.text(3, 8, w-6, path+"▏", base.Foreground(accent).Bold(true))
	a.text(2, 10, w-4, "Read-only mount · system authentication may be required", base.Foreground(muted))
	a.text(2, 11, w-4, "The volume stays mounted after Orchard exits.", base.Foreground(muted))
	a.text(2, h-3, w-4, f.err, base.Foreground(tcell.ColorYellow))
	a.text(2, h-2, w-4, "Enter mount & scan · Esc cancel · Ctrl-U clear · Backspace edit", base.Foreground(accent))
}
