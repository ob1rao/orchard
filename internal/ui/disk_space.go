package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/ob1rao/orchard/internal/volumes"
)

type spaceUpdate struct {
	serial uint64
	space  volumes.Space
	err    error
}

// Only one request per scan is in flight. Statfs can block on remote storage;
// keep it outside rendering and the event loop, including after scanning ends.
func (a *App) pollSpace(ctx context.Context, now time.Time) {
	if a.tree == nil || a.hideDiskSpace || a.spaceLoading || now.Before(a.spaceNext) {
		return
	}
	if a.spaceResults == nil {
		a.spaceResults = make(chan spaceUpdate, 1)
	}
	a.spaceLoading = true
	a.spaceNext = now.Add(5 * time.Second)
	serial, path, results := a.spaceSerial, a.tree.Root.Path(), a.spaceResults
	go func() {
		space, err := volumes.ReadSpace(path)
		select {
		case results <- spaceUpdate{serial, space, err}:
		case <-ctx.Done():
		}
	}()
}

func (a *App) acceptSpace(update spaceUpdate) {
	if update.serial != a.spaceSerial {
		return
	}
	a.spaceLoading = false
	a.diskSpace = update.space
	a.spaceErr = update.err
	a.spaceReady = true
}

func unaccounted(space volumes.Space, allocated uint64) (uint64, bool) {
	used := space.Total - min(space.Free, space.Total)
	if allocated > used {
		return 0, false
	}
	return used - allocated, true
}

func (a *App) drawDiskSpace(w int) {
	if a.hideDiskSpace {
		return
	}
	text := "Disk space: reading… · i hide"
	if a.spaceReady {
		if a.spaceErr != nil {
			text = "Disk space unavailable · i hide"
		} else {
			other, comparable := unaccounted(a.diskSpace, a.view.Stats.Allocated)
			value := "~" + Bytes(other)
			if !comparable {
				value = "unknown"
			}
			label := "Outside scan/other"
			if !a.view.Stats.Done || a.view.Stats.Cancelled {
				label = "Unscanned/other"
			}
			text = fmt.Sprintf("Free %s · %s %s · i hide", Bytes(a.diskSpace.Free), label, value)
			if w >= 110 {
				text = fmt.Sprintf("Disk free %s / %s · Available %s · %s %s · i hide", Bytes(a.diskSpace.Free), Bytes(a.diskSpace.Total), Bytes(a.diskSpace.Available), label, value)
			} else if w < 75 {
				text = fmt.Sprintf("Free %s · Other %s · i", Bytes(a.diskSpace.Free), value)
			}
		}
	}
	a.text(2, 1, w-4, text, base.Foreground(accent))
}
