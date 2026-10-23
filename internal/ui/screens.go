package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/diff"
	"github.com/ob1rao/orchard/internal/library"
	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/snapshot"
	"github.com/ob1rao/orchard/internal/volumes"
)

// Config says what the interface opens on.
type Config struct {
	Path                string
	Apparent, Unmounted bool
	Scan                scan.Options
	Version             string
	// Open starts on a saved snapshot instead of a live scan.
	Open *snapshot.Snapshot
	// Compare starts on the difference between two snapshots.
	Compare [2]*snapshot.Snapshot
	// Screen starts on "library" or "machines".
	Screen string
}

// screen is a full-window view stacked above the tree or the disk picker.
// Esc pops the top screen; opening a tree clears the stack.
type screen interface {
	draw(a *App, w, h int)
	key(a *App, ctx context.Context, e *tcell.EventKey)
	mouse(a *App, ctx context.Context, e *tcell.EventMouse)
	// typing is true while the screen takes text, so q and ? are input.
	typing() bool
}

func (a *App) top() screen {
	if len(a.screens) == 0 {
		return nil
	}
	return a.screens[len(a.screens)-1]
}
func (a *App) push(s screen) {
	a.screens = append(a.screens, s)
	a.mouseDown = false
}
func (a *App) pop() {
	if len(a.screens) > 0 {
		a.screens = a.screens[:len(a.screens)-1]
	}
	if r, ok := a.top().(interface{ reload(*App) }); ok {
		r.reload(a)
	}
}

// later hands work to a goroutine and applies its result on the event loop,
// which owns every field of App.
func (a *App) later(work func() func(*App)) {
	ch := a.async
	go func() {
		apply := work()
		ch <- apply
	}()
}

func (a *App) openLibrary() (*library.Library, error) {
	if a.lib != nil {
		return a.lib, nil
	}
	lib, err := library.Open()
	if err != nil {
		return nil, err
	}
	a.lib = lib
	return lib, nil
}

// loadSnapshot decodes a library entry off the event loop, reusing ones
// already in memory, then hands it to done.
func (a *App) loadSnapshot(e library.Entry, done func(*App, *snapshot.Snapshot)) {
	if s, ok := a.loaded[e.ID]; ok {
		done(a, s)
		return
	}
	a.busy = "Loading snapshot " + e.ID + "…"
	a.later(func() func(*App) {
		s, err := library.Load(e)
		return func(a *App) {
			a.busy = ""
			if err != nil {
				a.notice = err.Error()
				return
			}
			a.remember(s)
			done(a, s)
		}
	})
}

// remember keeps a few decoded snapshots so moving between the library,
// comparisons and queries does not decode the same file again.
func (a *App) remember(s *snapshot.Snapshot) {
	if a.loaded == nil {
		a.loaded = map[string]*snapshot.Snapshot{}
	}
	if len(a.loaded) >= 8 {
		for id := range a.loaded {
			delete(a.loaded, id)
			break
		}
	}
	a.loaded[s.Meta.ID] = s
}

// openSnapshot shows a saved tree offline. Nothing on disk is read: free
// space comes from the snapshot, and the file viewer is unavailable.
func (a *App) openSnapshot(s *snapshot.Snapshot) {
	a.stop()
	a.remember(s)
	meta := s.Meta
	a.snap = &meta
	a.tree = s.Tree
	a.current = s.Tree.Root
	a.screens = nil
	a.picker = false
	a.mapPages = nil
	a.filter, a.notice = "", ""
	a.selected, a.offset = 0, 0
	a.entries = nil
	a.spaceSerial++
	a.spaceLoading = false
	a.spaceReady = true
	a.spaceErr = nil
	if meta.Space != nil {
		a.diskSpace = *meta.Space
	} else {
		a.diskSpace, a.spaceErr = volumes.Space{}, errors.New("not recorded")
	}
	a.savedID = meta.ID
	a.refresh()
}

// snapshotTitle names a snapshot in one line.
func snapshotTitle(m snapshot.Meta) string {
	return fmt.Sprintf("%s · %s · %s", m.Machine, m.ScannedAt.Local().Format("2006-01-02 15:04"), m.ID)
}

// saveCurrent writes the live tree to the library.
func (a *App) saveCurrent() {
	switch {
	case a.tree == nil:
		return
	case a.snap != nil || a.savedID != "":
		a.notice = "Already saved as snapshot " + a.savedID + " · o opens the library"
		return
	case !a.view.Stats.Done:
		a.notice = "Still scanning: wait for it to finish, or press s to stop and save the partial result."
		return
	}
	lib, err := a.openLibrary()
	if err != nil {
		a.notice = err.Error()
		return
	}
	tree, started, version := a.tree, a.scanStarted, a.version
	var space *volumes.Space
	if a.spaceReady && a.spaceErr == nil {
		s := a.diskSpace
		space = &s
	}
	a.notice = "Saving snapshot…"
	a.later(func() func(*App) {
		meta := snapshot.Describe(tree, started, version)
		meta.Space = space
		e, err := lib.Save(meta, tree)
		return func(a *App) {
			if err != nil {
				a.notice = "Save failed: " + err.Error()
				return
			}
			if a.tree == tree {
				a.savedID = e.ID
			}
			a.notice = fmt.Sprintf("Saved snapshot %s · %s · c compares with the previous one · o opens the library", e.ID, Bytes(e.Stats.Allocated))
		}
	})
}

// compareWithPrevious diffs the current tree against the newest other
// snapshot of the same machine and root.
func (a *App) compareWithPrevious() {
	if a.tree == nil {
		return
	}
	if a.snap == nil && !a.view.Stats.Done {
		a.notice = "Still scanning: compare once the scan finishes."
		return
	}
	lib, err := a.openLibrary()
	if err != nil {
		a.notice = err.Error()
		return
	}
	machine, root, id := snapshot.Hostname(), a.tree.Root.Path(), a.savedID
	var after snapshot.Meta
	if a.snap != nil {
		machine, after = a.snap.Machine, *a.snap
	} else {
		after = snapshot.Describe(a.tree, a.scanStarted, a.version)
		after.ID = "live"
		if id != "" {
			after.ID = id
		}
	}
	all, err := lib.List()
	if err != nil {
		a.notice = err.Error()
		return
	}
	var previous *library.Entry
	for i, e := range all {
		if strings.EqualFold(e.Machine, machine) && e.Root == root && e.ID != id && !e.ScannedAt.After(after.ScannedAt) {
			previous = &all[i]
			break
		}
	}
	if previous == nil {
		a.notice = "No earlier snapshot of " + root + " on " + machine + " to compare with."
		return
	}
	tree := a.tree
	a.loadSnapshot(*previous, func(a *App, before *snapshot.Snapshot) {
		a.push(newCompare(before.Meta, after, before.Tree, tree))
	})
}

func newCompare(before, after snapshot.Meta, a, b *scan.Tree) *compareScreen {
	root := diff.Compare(a.Root, b.Root)
	return &compareScreen{before: before, after: after, root: root, current: root, hideSame: true}
}

// list is the scroll state every list screen shares.
type list struct{ selected, offset int }

func (l *list) move(delta, n int) { l.selected = max(0, min(n-1, l.selected+delta)) }
func (l *list) clamp(n, height int) {
	l.selected = max(0, min(l.selected, n-1))
	if l.selected < l.offset {
		l.offset = l.selected
	}
	if l.selected >= l.offset+height {
		l.offset = l.selected - height + 1
	}
	l.offset = max(0, min(l.offset, max(0, n-height)))
}

// navigate applies the movement keys every list shares and reports whether
// the key was one of them.
func (l *list) navigate(e *tcell.EventKey, n, page int) bool {
	switch e.Key() {
	case tcell.KeyUp:
		l.move(-1, n)
	case tcell.KeyDown:
		l.move(1, n)
	case tcell.KeyPgUp:
		l.move(-max(1, page), n)
	case tcell.KeyPgDn:
		l.move(max(1, page), n)
	case tcell.KeyHome:
		l.selected = 0
	case tcell.KeyEnd:
		l.selected = max(0, n-1)
	case tcell.KeyRune:
		switch e.Str() {
		case "j":
			l.move(1, n)
		case "k":
			l.move(-1, n)
		default:
			return false
		}
	default:
		return false
	}
	return true
}

// click turns a mouse press on a list into a selection, reporting a double
// click on the same row.
func (a *App) click(l *list, e *tcell.EventMouse, top, height, n int) (index int, double bool) {
	b := e.Buttons()
	_, y := e.Position()
	if b&tcell.WheelUp != 0 {
		l.move(-3, n)
		return -1, false
	}
	if b&tcell.WheelDown != 0 {
		l.move(3, n)
		return -1, false
	}
	if b == tcell.ButtonNone {
		a.mouseDown = false
		return -1, false
	}
	if a.mouseDown || b&tcell.Button1 == 0 {
		return -1, false
	}
	a.mouseDown = true
	i := l.offset + y - top
	if y < top || y >= top+height || i < 0 || i >= n {
		return -1, false
	}
	l.selected = i
	key := fmt.Sprintf("%p:%d", l, i)
	double = a.lastRow == key && time.Since(a.lastClick) < 450*time.Millisecond
	a.lastRow, a.lastClick = key, time.Now()
	if double {
		a.lastRow = ""
	}
	return i, double
}

func rightClick(e *tcell.EventMouse) bool { return e.Buttons()&tcell.Button2 != 0 }

var selectedStyle = base.Background(tcell.NewHexColor(0x26445a)).Foreground(accent)

func (a *App) row(y, w int, selected bool) tcell.Style {
	if selected {
		a.screen.FillArea(1, y, w-2, 1, ' ', selectedStyle)
		return selectedStyle
	}
	return base
}

func signedBytes(d int64) string {
	switch {
	case d > 0:
		return "+" + Bytes(uint64(d))
	case d < 0:
		return "−" + Bytes(uint64(-d))
	}
	return "±0"
}

// rescanSnapshot takes a fresh scan of the directory a snapshot came from:
// locally when it was this machine's, over SSH when it was another's.
func (a *App) rescanSnapshot(ctx context.Context) {
	m := *a.snap
	if m.Source == "local" && strings.EqualFold(m.Machine, snapshot.Hostname()) {
		_ = a.start(ctx, m.Root)
		return
	}
	lib, err := a.openLibrary()
	if err != nil {
		a.notice = err.Error()
		return
	}
	machine, err := lib.Machine(m.Machine)
	if err != nil {
		host, ok := strings.CutPrefix(m.Source, "ssh:")
		if !ok {
			a.notice = "Rescan " + m.Machine + " there with: orchard save " + m.Root
			return
		}
		machine = library.Machine{Name: m.Machine, Host: host}
	}
	a.startRemote(ctx, machine, m.Root)
}
