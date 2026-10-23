package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/library"
	"github.com/ob1rao/orchard/internal/snapshot"
)

// libraryScreen lists saved snapshots, newest first, for opening offline,
// comparing, or deleting.
type libraryScreen struct {
	list
	machine string // only this machine's snapshots, when set
	entries []library.Entry
	marked  []string // IDs picked for comparison, oldest pick first
	confirm string   // ID awaiting a second x to delete
	err     string
	top     int
	height  int
}

func (a *App) openLibraryScreen(machine string) {
	s := &libraryScreen{machine: machine}
	s.reload(a)
	a.push(s)
}

func (s *libraryScreen) reload(a *App) {
	lib, err := a.openLibrary()
	if err != nil {
		s.err = err.Error()
		return
	}
	all, err := lib.List()
	if err != nil {
		s.err = err.Error()
		return
	}
	s.err = ""
	s.entries = s.entries[:0]
	for _, e := range all {
		if s.machine == "" || strings.EqualFold(e.Machine, s.machine) {
			s.entries = append(s.entries, e)
		}
	}
	keep := s.marked[:0]
	for _, id := range s.marked {
		if s.find(id) >= 0 {
			keep = append(keep, id)
		}
	}
	s.marked = keep
}

func (s *libraryScreen) find(id string) int {
	for i, e := range s.entries {
		if e.ID == id {
			return i
		}
	}
	return -1
}

func (s *libraryScreen) typing() bool { return false }

func (s *libraryScreen) toggleMark(id string) {
	for i, m := range s.marked {
		if m == id {
			s.marked = append(s.marked[:i], s.marked[i+1:]...)
			return
		}
	}
	s.marked = append(s.marked, id)
	if len(s.marked) > 2 {
		s.marked = s.marked[1:]
	}
}

func (s *libraryScreen) isMarked(id string) bool {
	for _, m := range s.marked {
		if m == id {
			return true
		}
	}
	return false
}

// pair chooses what c compares: the two marked snapshots; the marked one
// and the selected one; or the selected one and the snapshot before it of
// the same machine and directory. The older is always "before".
func (s *libraryScreen) pair() (before, after library.Entry, ok bool) {
	var picks []library.Entry
	for _, id := range s.marked {
		picks = append(picks, s.entries[s.find(id)])
	}
	if len(picks) < 2 && len(s.entries) > 0 {
		sel := s.entries[s.selected]
		if len(picks) == 0 || picks[0].ID != sel.ID {
			picks = append(picks, sel)
		}
	}
	if len(picks) == 1 {
		for _, e := range s.entries[s.selected+1:] {
			if e.Machine == picks[0].Machine && e.Root == picks[0].Root {
				picks = append(picks, e)
				break
			}
		}
	}
	if len(picks) < 2 {
		return before, after, false
	}
	before, after = picks[0], picks[1]
	if before.ScannedAt.After(after.ScannedAt) {
		before, after = after, before
	}
	return before, after, true
}

func (s *libraryScreen) compare(a *App) {
	before, after, ok := s.pair()
	if !ok {
		a.notice = "Nothing to compare: mark two snapshots with Space, or select one that has an earlier scan."
		return
	}
	a.loadSnapshot(before, func(a *App, b *snapshot.Snapshot) {
		a.loadSnapshot(after, func(a *App, c *snapshot.Snapshot) {
			a.push(newCompare(b.Meta, c.Meta, b.Tree, c.Tree))
		})
	})
}

func (s *libraryScreen) key(a *App, ctx context.Context, e *tcell.EventKey) {
	confirm := s.confirm
	s.confirm = ""
	if s.list.navigate(e, len(s.entries), s.height) {
		return
	}
	switch e.Key() {
	case tcell.KeyEscape, tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
		a.leaveScreen(ctx)
		return
	case tcell.KeyEnter, tcell.KeyRight:
		if len(s.entries) > 0 {
			a.loadSnapshot(s.entries[s.selected], func(a *App, snap *snapshot.Snapshot) { a.openSnapshot(snap) })
		}
		return
	}
	if e.Key() != tcell.KeyRune {
		return
	}
	switch e.Str() {
	case " ":
		if len(s.entries) > 0 {
			s.toggleMark(s.entries[s.selected].ID)
			s.move(1, len(s.entries))
		}
	case "c":
		s.compare(a)
	case "x":
		if len(s.entries) == 0 {
			return
		}
		sel := s.entries[s.selected]
		if confirm != sel.ID {
			s.confirm = sel.ID
			a.notice = "Press x again to delete snapshot " + sel.ID + ". Archive it first with: orchard archive export FILE " + sel.ID
			return
		}
		if err := a.lib.Delete(sel); err != nil {
			a.notice = err.Error()
		} else {
			a.notice = "Deleted snapshot " + sel.ID
			delete(a.loaded, sel.ID)
		}
		s.reload(a)
	case "a":
		s.machine = ""
		s.reload(a)
	case "r":
		s.reload(a)
		a.notice = ""
	case "f", "F":
		a.openQueryScreen("")
	case "m":
		a.openMachinesScreen()
	}
}

func (s *libraryScreen) mouse(a *App, ctx context.Context, e *tcell.EventMouse) {
	if rightClick(e) {
		a.leaveScreen(ctx)
		return
	}
	if i, double := a.click(&s.list, e, s.top, s.height, len(s.entries)); i >= 0 && double {
		a.loadSnapshot(s.entries[i], func(a *App, snap *snapshot.Snapshot) { a.openSnapshot(snap) })
	}
}

func (s *libraryScreen) draw(a *App, w, h int) {
	title := "SNAPSHOT LIBRARY"
	if s.machine != "" {
		title += " · " + s.machine + " · a shows every machine"
	}
	machines := map[string]bool{}
	for _, e := range s.entries {
		machines[e.Machine] = true
	}
	a.text(2, 2, w-4, fmt.Sprintf("%s · %s on %s", title, plural(len(s.entries), "snapshot"), plural(len(machines), "machine")), base.Foreground(accent).Bold(true))
	if a.lib != nil {
		a.text(2, 3, w-4, a.lib.Dir, base.Foreground(muted))
	}
	s.top, s.height = 6, max(1, h-12)
	s.clamp(len(s.entries), s.height)
	cols := []int{4, 14, 30, 50}
	rootWidth := max(10, w-cols[3]-32)
	a.text(cols[0], 5, 10, "ID", base.Foreground(muted).Bold(true))
	a.text(cols[1], 5, 15, "MACHINE", base.Foreground(muted).Bold(true))
	a.text(cols[2], 5, 18, "SCANNED", base.Foreground(muted).Bold(true))
	a.text(cols[3], 5, rootWidth, "ROOT", base.Foreground(muted).Bold(true))
	a.text(w-30, 5, 10, "      USED", base.Foreground(muted).Bold(true))
	a.text(w-18, 5, 16, "FILES", base.Foreground(muted).Bold(true))
	now := time.Now()
	for row := 0; row < s.height && row+s.offset < len(s.entries); row++ {
		i := row + s.offset
		e := s.entries[i]
		y := s.top + row
		style := a.row(y, w, i == s.selected)
		if s.isMarked(e.ID) {
			a.text(2, y, 2, "●", style.Foreground(tcell.NewHexColor(0xf2c46d)))
		}
		a.text(cols[0], y, 9, e.ID, style)
		a.text(cols[1], y, 15, e.Machine, style)
		a.text(cols[2], y, 19, e.ScannedAt.Local().Format("2006-01-02 15:04"), style)
		root := e.Root
		if e.Stats.Partial {
			root += "  (partial)"
		}
		a.text(cols[3], y, rootWidth, root, style)
		a.text(w-30, y, 10, fmt.Sprintf("%10s", Bytes(e.Stats.Allocated)), style)
		a.text(w-18, y, 16, fmt.Sprintf("%d · %s", e.Stats.Files, library.Age(e.ScannedAt, now)), style.Foreground(muted))
	}
	switch {
	case s.err != "":
		a.text(2, s.top, w-4, s.err, base.Foreground(tcell.ColorYellow))
	case len(s.entries) == 0:
		a.text(4, s.top, w-6, "No snapshots yet. Press w after a scan, run `orchard save DIR`, or scan a machine from m.", base.Foreground(muted))
	default:
		e := s.entries[s.selected]
		detail := fmt.Sprintf("%s · %s (%s/%s, orchard %s) · via %s", e.Root, e.Hostname, e.OS, e.Arch, e.Version, e.Source)
		if e.Label != "" {
			detail = e.Label + " · " + detail
		}
		a.text(2, h-5, w-4, detail, base.Bold(true))
	}
	compare := "c compare with previous"
	if len(s.marked) == 2 {
		compare = "c compare marked"
	} else if len(s.marked) == 1 {
		compare = "c compare marked with selected"
	}
	a.text(2, h-4, w-4, a.notice, base.Foreground(tcell.ColorYellow))
	a.text(2, h-3, w-4, fmt.Sprintf("%d marked · Space mark · %s · x delete", len(s.marked), compare), base.Foreground(muted))
	a.text(2, h-2, w-4, "Enter open offline · f query all machines · m machines · Esc back · q quit", base.Foreground(accent))
}

// leaveScreen pops the top screen, landing on the picker when nothing is
// underneath and no tree is open.
func (a *App) leaveScreen(ctx context.Context) {
	a.pop()
	if len(a.screens) == 0 && a.tree == nil && !a.picker {
		a.showPicker(ctx)
	}
}
