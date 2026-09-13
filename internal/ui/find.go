package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/scan"
)

type findState struct {
	query                     string
	regex, fullPath           bool
	entries                   []scan.Entry
	selected, offset, matches int
	err                       string
	pending, busy             bool
	next                      time.Time
	version                   uint64
	cancel                    context.CancelFunc
}
type findUpdate struct {
	serial uint64
	result scan.SearchResult
	err    error
}

func (a *App) openFind() {
	a.closeFind()
	if a.findResults == nil {
		a.findResults = make(chan findUpdate, 4)
	}
	a.find = &findState{}
	a.lastNode = nil
	a.mouseDown = false
}
func (a *App) closeFind() {
	if a.find != nil && a.find.cancel != nil {
		a.find.cancel()
	}
	a.find = nil
	a.findSerial++
}
func (a *App) queueFind() {
	f := a.find
	if f.cancel != nil {
		f.cancel()
	}
	a.findSerial++
	f.pending = true
	f.busy = false
	f.entries = nil
	f.selected = 0
	f.offset = 0
	f.matches = 0
	f.err = ""
	f.next = time.Now().Add(150 * time.Millisecond)
}
func (a *App) pollFind() bool {
	f := a.find
	if f == nil || f.query == "" || f.busy || time.Now().Before(f.next) {
		return false
	}
	version := a.view.Stats.Files + a.view.Stats.Directories
	if !f.pending && version == f.version {
		return false
	}
	f.pending = false
	f.busy = true
	f.version = version
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	a.findSerial++
	serial := a.findSerial
	tree, updates := a.tree, a.findResults
	query := scan.Query{Text: f.query, Regex: f.regex, FullPath: f.fullPath, HideHidden: a.hideHidden, Apparent: a.apparent}
	go func() {
		result, err := tree.Search(ctx, query)
		select {
		case updates <- findUpdate{serial, result, err}:
		case <-ctx.Done():
		}
	}()
	return true
}
func (a *App) acceptFind(u findUpdate) {
	if a.find == nil || u.serial != a.findSerial {
		return
	}
	f := a.find
	var selected *scan.Node
	if f.selected < len(f.entries) {
		selected = f.entries[f.selected].Node
	}
	f.busy = false
	f.next = time.Now().Add(500 * time.Millisecond)
	f.entries = u.result.Entries
	f.matches = u.result.Matches
	f.err = ""
	if u.err != nil {
		f.err = u.err.Error()
	}
	f.selected = min(f.selected, max(0, len(f.entries)-1))
	for i, e := range f.entries {
		if e.Node == selected {
			f.selected = i
			break
		}
	}
}
func (a *App) revealFind() {
	if a.find == nil || len(a.find.entries) == 0 {
		return
	}
	n := a.find.entries[a.find.selected].Node
	a.closeFind()
	a.enter(n.Parent)
	for i, e := range a.entries {
		if e.Node == n {
			a.selected = i
			break
		}
	}
}
func (a *App) findKey(e *tcell.EventKey) {
	f := a.find
	switch e.Key() {
	case tcell.KeyEscape:
		a.closeFind()
	case tcell.KeyEnter:
		a.revealFind()
	case tcell.KeyUp:
		a.move(-1)
	case tcell.KeyDown:
		a.move(1)
	case tcell.KeyPgUp:
		a.move(-max(1, a.listHeight))
	case tcell.KeyPgDn:
		a.move(max(1, a.listHeight))
	case tcell.KeyHome:
		f.selected = 0
	case tcell.KeyEnd:
		f.selected = max(0, len(f.entries)-1)
	case tcell.KeyF2, tcell.KeyTab:
		f.regex = !f.regex
		a.queueFind()
	case tcell.KeyF3:
		f.fullPath = !f.fullPath
		a.queueFind()
	case tcell.KeyF4:
		a.mapPages = nil
		a.hideHidden = !a.hideHidden
		a.refresh()
		a.queueFind()
	case tcell.KeyCtrlU:
		f.query = ""
		a.queueFind()
	case tcell.KeyCtrlL:
		a.screen.Sync()
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		r := []rune(f.query)
		if len(r) > 0 {
			f.query = string(r[:len(r)-1])
			a.queueFind()
		}
	case tcell.KeyRune:
		if len(f.query)+len(e.Str()) <= 4096 {
			f.query += e.Str()
			a.queueFind()
		}
	}
}
func (a *App) findMouse(e *tcell.EventMouse) {
	b := e.Buttons()
	_, y := e.Position()
	if b&tcell.WheelUp != 0 {
		a.move(-3)
		return
	}
	if b&tcell.WheelDown != 0 {
		a.move(3)
		return
	}
	if b == tcell.ButtonNone {
		a.mouseDown = false
		return
	}
	if a.mouseDown {
		return
	}
	a.mouseDown = true
	if b&tcell.Button2 != 0 {
		a.closeFind()
		return
	}
	i := a.find.offset + y - a.listTop
	if b&tcell.Button1 == 0 || y < a.listTop || y >= a.listTop+a.listHeight || i < 0 || i >= len(a.find.entries) {
		return
	}
	a.find.selected = i
	n := a.find.entries[i].Node
	if a.lastNode == n && time.Since(a.lastClick) < 450*time.Millisecond {
		a.revealFind()
		a.lastNode = nil
	} else {
		a.lastNode = n
		a.lastClick = time.Now()
	}
}
func date(sec int64) string { return time.Unix(sec, 0).Local().Format("2006-01-02") }
func createdDate(n *scan.Node) string {
	if !n.HasCreated {
		return "—"
	}
	return date(n.Created)
}
func (a *App) drawDates(n *scan.Node, w, h int) {
	modified := time.Unix(n.Modified, 0).Local().Format("2006-01-02 15:04:05 MST")
	created := "unavailable"
	if n.HasCreated {
		created = time.Unix(n.Created, 0).Local().Format("2006-01-02 15:04:05 MST")
	}
	a.text(2, h-6, w-4, "Modified: "+modified, base.Foreground(muted))
	a.text(2, h-5, w-4, "Created:  "+created, base.Foreground(muted))
}
func (a *App) drawFind(w, h int) {
	f := a.find
	mode := "plain text (ignore case)"
	if f.regex {
		mode = "regex (case-sensitive; (?i) ignores case)"
	}
	target := "filename"
	if f.fullPath {
		target = "full path"
	}
	hidden := "shown"
	if a.hideHidden {
		hidden = "hidden"
	}
	a.text(2, 2, w-4, "RECURSIVE SEARCH · "+a.tree.Root.Path(), base.Foreground(accent).Bold(true))
	a.text(2, 3, w-4, "Query: "+f.query+"▏", base.Bold(true))
	a.text(2, 4, w-4, fmt.Sprintf("%s · %s · dotfiles %s", mode, target, hidden), base.Foreground(muted))
	a.listTop = 7
	a.listHeight = h - 15
	nameWidth := w - 17
	sizeX := w - 13
	if w >= 100 {
		nameWidth = w - 53
		sizeX = w - 49
		a.text(w-36, 6, 16, "MODIFIED (local)", base.Foreground(muted))
		a.text(w-18, 6, 16, "CREATED (local)", base.Foreground(muted))
	}
	a.text(2, 6, nameWidth, "PATH", base.Foreground(muted).Bold(true))
	a.text(sizeX, 6, 10, "      SIZE", base.Foreground(muted).Bold(true))
	if f.selected < f.offset {
		f.offset = f.selected
	}
	if f.selected >= f.offset+a.listHeight {
		f.offset = f.selected - a.listHeight + 1
	}
	for row := 0; row < a.listHeight && row+f.offset < len(f.entries); row++ {
		i := row + f.offset
		e := f.entries[i]
		style := base
		if i == f.selected {
			style = base.Background(tcell.NewHexColor(0x26445a)).Foreground(accent)
			a.screen.FillArea(1, a.listTop+row, w-2, 1, ' ', style)
		}
		name := e.Node.Path()
		if e.Dir {
			name += "/"
		}
		a.text(2, a.listTop+row, nameWidth, name, style)
		a.text(sizeX, a.listTop+row, 10, fmt.Sprintf("%10s", Bytes(e.Size)), style)
		if w >= 100 {
			a.text(w-36, a.listTop+row, 16, time.Unix(e.Node.Modified, 0).Local().Format("2006-01-02 15:04"), style)
			created := "unavailable"
			if e.Node.HasCreated {
				created = time.Unix(e.Node.Created, 0).Local().Format("2006-01-02 15:04")
			}
			a.text(w-18, a.listTop+row, 16, created, style)
		}
	}
	if len(f.entries) > 0 {
		n := f.entries[f.selected].Node
		a.text(2, h-7, w-4, n.Path(), base.Bold(true))
		a.drawDates(n, w, h)
	}
	status := fmt.Sprintf("%d matches · %d listed", f.matches, len(f.entries))
	if f.matches > scan.SearchLimit {
		status += " · limit reached: narrow the query"
	}
	if !a.view.Stats.Done {
		status += " · disk scan in progress"
	}
	if a.view.Stats.Cancelled {
		status += " · partial disk scan"
	}
	if a.view.Stats.Errors > 0 {
		status += " · unreadable entries"
	}
	if f.busy || f.pending && f.query != "" {
		status += " · searching…"
	}
	if f.query == "" {
		status = "Type a query to search the scanned disk; no files are reread."
	} else if f.err != "" {
		status = "Invalid regex: " + f.err
	}
	a.text(2, h-4, w-4, status, base.Foreground(accent))
	a.text(2, h-3, w-4, "Tab/F2 regex · F3 filename/path · F4 hidden · Ctrl-U clear", base.Foreground(muted))
	a.text(2, h-2, w-4, "↑↓ select · Enter / double-click reveal · Esc close", base.Foreground(accent))
}
