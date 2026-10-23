package ui

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/library"
	"github.com/ob1rao/orchard/internal/query"
	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/snapshot"
)

const queryLimit = 2000

// queryScreen searches saved snapshots across every machine at once.
type queryScreen struct {
	list
	input   string
	all     bool // every snapshot rather than the newest per machine and root
	snaps   []*snapshot.Snapshot
	loading bool
	loadErr string
	result  query.Result
	err     string
	pending bool
	busy    bool
	next    time.Time
	serial  uint64
	top     int
	height  int
}

func (a *App) openQueryScreen(initial string) {
	s := &queryScreen{input: initial, pending: initial != ""}
	a.push(s)
	s.load(a)
}

func (s *queryScreen) typing() bool { return true }

// load decodes the snapshots in scope. Queries then run in memory, so each
// keystroke costs a pass over the indexes rather than a read of every file.
func (s *queryScreen) load(a *App) {
	lib, err := a.openLibrary()
	if err != nil {
		s.loadErr = err.Error()
		return
	}
	s.loading, s.snaps = true, nil
	all := s.all
	a.later(func() func(*App) {
		entries, err := lib.List()
		if !all {
			entries = library.Latest(entries)
		}
		var snaps []*snapshot.Snapshot
		var skipped int
		for _, e := range entries {
			snap, err := library.Load(e)
			if err != nil {
				skipped++
				continue
			}
			snaps = append(snaps, snap)
		}
		return func(*App) {
			if s.all != all {
				return // the scope changed while loading
			}
			s.loading = false
			s.loadErr = ""
			if err != nil {
				s.loadErr = err.Error()
			} else if skipped > 0 {
				s.loadErr = fmt.Sprintf("%d unreadable snapshots skipped", skipped)
			}
			s.snaps = snaps
			s.pending = s.input != ""
		}
	})
}

func (s *queryScreen) changed() {
	s.pending = true
	s.next = time.Now().Add(250 * time.Millisecond)
	s.serial++
	s.selected, s.offset = 0, 0
}

// tick runs a pending query once typing pauses.
func (s *queryScreen) tick(a *App) bool {
	if !s.pending || s.busy || s.loading || time.Now().Before(s.next) {
		return false
	}
	s.pending = false
	if s.input == "" {
		s.result, s.err = query.Result{}, ""
		return true
	}
	q, err := query.Parse(s.input)
	if err != nil {
		s.err, s.result = err.Error(), query.Result{}
		return true
	}
	q.Apparent = a.apparent
	s.err = ""
	s.busy = true
	serial := s.serial
	var trees []*scan.Tree
	var scope []*snapshot.Snapshot
	for _, snap := range s.snaps {
		if q.WantsMachine(snap.Meta.Machine) {
			trees = append(trees, snap.Tree)
			scope = append(scope, snap)
		}
	}
	a.later(func() func(*App) {
		res := q.Run(trees, queryLimit, time.Now())
		return func(*App) {
			s.busy = false
			if serial != s.serial {
				return
			}
			// Hits index the trees searched; map them back to s.snaps.
			for i := range res.Hits {
				for j, snap := range s.snaps {
					if snap == scope[res.Hits[i].Snapshot] {
						res.Hits[i].Snapshot = j
					}
				}
			}
			s.result = res
		}
	})
	return true
}

func (s *queryScreen) reveal(a *App, i int) {
	if i >= len(s.result.Hits) {
		return
	}
	h := s.result.Hits[i]
	snap := s.snaps[h.Snapshot]
	a.openSnapshot(snap)
	if h.Node.Parent != nil {
		a.enter(h.Node.Parent)
	}
	for j, e := range a.entries {
		if e.Node == h.Node {
			a.selected = j
		}
	}
}

func (s *queryScreen) key(a *App, ctx context.Context, e *tcell.EventKey) {
	n := len(s.result.Hits)
	switch e.Key() {
	case tcell.KeyEscape:
		a.leaveScreen(ctx)
	case tcell.KeyEnter:
		s.reveal(a, s.selected)
	case tcell.KeyUp:
		s.move(-1, n)
	case tcell.KeyDown:
		s.move(1, n)
	case tcell.KeyPgUp:
		s.move(-s.height, n)
	case tcell.KeyPgDn:
		s.move(s.height, n)
	case tcell.KeyTab:
		s.all = !s.all
		s.load(a)
		s.changed()
	case tcell.KeyCtrlU:
		s.input = ""
		s.changed()
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if r := []rune(s.input); len(r) > 0 {
			s.input = string(r[:len(r)-1])
			s.changed()
		}
	case tcell.KeyRune:
		if len(s.input) < 2048 {
			s.input += e.Str()
			s.changed()
		}
	}
}

func (s *queryScreen) mouse(a *App, ctx context.Context, e *tcell.EventMouse) {
	if rightClick(e) {
		a.leaveScreen(ctx)
		return
	}
	if i, double := a.click(&s.list, e, s.top, s.height, len(s.result.Hits)); i >= 0 && double {
		s.reveal(a, i)
	}
}

func (s *queryScreen) draw(a *App, w, h int) {
	scope := "newest snapshot per machine and directory"
	if s.all {
		scope = "every saved snapshot"
	}
	machines := map[string]bool{}
	for _, snap := range s.snaps {
		machines[snap.Meta.Machine] = true
	}
	a.text(2, 2, w-4, fmt.Sprintf("FLEET QUERY · %s · %s on %s", scope, plural(len(s.snaps), "snapshot"), plural(len(machines), "machine")), base.Foreground(accent).Bold(true))
	a.text(2, 3, w-4, "Query: "+s.input+"▏", base.Bold(true))
	a.text(2, 4, w-4, "words match names · ext:log  type:file|dir  size>1G  age>90d  modified<2024-01-01  path:/var/*  re:REGEX  machine:NAME  -term negates", base.Foreground(muted))
	s.top, s.height = 7, max(1, h-13)
	hits := s.result.Hits
	s.clamp(len(hits), s.height)
	a.text(2, 6, 16, "MACHINE", base.Foreground(muted).Bold(true))
	a.text(19, 6, 10, "      SIZE", base.Foreground(muted).Bold(true))
	a.text(31, 6, 10, "MODIFIED", base.Foreground(muted).Bold(true))
	a.text(43, 6, w-45, "PATH", base.Foreground(muted).Bold(true))
	for row := 0; row < s.height && row+s.offset < len(hits); row++ {
		i := row + s.offset
		hit := hits[i]
		meta := s.snaps[hit.Snapshot].Meta
		y := s.top + row
		style := a.row(y, w, i == s.selected)
		name := path.Join(meta.Root, hit.Path)
		if hit.Node.Dir {
			name += "/"
		}
		a.text(2, y, 16, meta.Machine, style.Bold(true))
		a.text(19, y, 10, fmt.Sprintf("%10s", Bytes(hit.Size)), style)
		a.text(31, y, 10, date(hit.Node.Modified), style.Foreground(muted))
		a.text(43, y, w-45, name, style)
	}
	status := fmt.Sprintf("%d matches · %s total", s.result.Matches, Bytes(s.result.Bytes))
	if s.result.Matches > len(hits) {
		status += fmt.Sprintf(" · showing the largest %d", len(hits))
	}
	if s.result.Matches > 0 && !strings.Contains(s.input, "type:f") && !strings.Contains(s.input, "ext:") {
		status += " · totals count nested matches again; add type:file for a true sum"
	}
	switch {
	case s.loading:
		status = "Loading snapshots…"
	case s.err != "":
		status = s.err
	case s.input == "":
		status = "Type a query to search every machine's saved snapshots."
	case s.busy || s.pending:
		status = "Searching…"
	}
	if len(s.snaps) == 0 && !s.loading {
		status = "No snapshots saved yet."
	}
	if len(hits) > 0 {
		meta := s.snaps[hits[s.selected].Snapshot].Meta
		a.text(2, h-5, w-4, "from snapshot "+snapshotTitle(meta), base.Foreground(muted))
	}
	a.text(2, h-4, w-4, status, base.Foreground(accent))
	if s.loadErr != "" {
		a.text(2, h-3, w-4, s.loadErr, base.Foreground(tcell.ColorYellow))
	} else {
		a.text(2, h-3, w-4, "Tab newest only / every snapshot · Ctrl-U clear", base.Foreground(muted))
	}
	a.text(2, h-2, w-4, "↑↓ select · Enter open in its snapshot · Esc close", base.Foreground(accent))
}
