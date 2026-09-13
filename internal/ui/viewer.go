package ui

import (
	"fmt"
	"strings"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/viewer"
)

type fileView struct {
	file         *viewer.File
	page         viewer.Page
	rows, column int
	err          string
}

func (a *App) openViewer(tail bool) {
	if a.picker || len(a.entries) == 0 {
		return
	}
	n := a.entries[a.selected].Node
	if n.Dir {
		a.notice = "Select a file to view its contents."
		return
	}
	f, err := viewer.Open(n.Path())
	if err != nil {
		a.notice = "Cannot view: " + err.Error()
		return
	}
	a.closeViewer()
	a.notice = ""
	_, h := a.screen.Size()
	v := &fileView{file: f, rows: min(viewer.MaxRows, max(1, h-8))}
	a.viewer = v
	var offset int64
	if tail {
		offset, err = f.Tail(v.rows)
		if err != nil {
			v.err = err.Error()
			return
		}
	}
	a.loadViewer(offset)
}
func (a *App) closeViewer() {
	if a.viewer != nil {
		_ = a.viewer.file.Close()
		a.viewer = nil
	}
}
func (a *App) loadViewer(offset int64) {
	v := a.viewer
	page, err := v.file.ReadPage(offset, v.rows)
	v.page = page
	v.err = ""
	if err != nil {
		v.err = err.Error()
	}
}
func (a *App) scrollViewer(rows int) {
	v := a.viewer
	if rows < 0 {
		offset, err := v.file.Back(v.page.Offset, -rows)
		if err != nil {
			v.err = err.Error()
			return
		}
		a.loadViewer(offset)
		return
	}
	if len(v.page.Lines) == 0 || v.page.EOF && len(v.page.Lines) <= rows {
		return
	}
	i := min(rows, len(v.page.Lines)) - 1
	if i >= 0 {
		a.loadViewer(v.page.Lines[i].Next)
	}
}
func (a *App) viewerEnd() {
	v := a.viewer
	if err := v.file.Refresh(); err != nil {
		v.err = err.Error()
		return
	}
	offset, err := v.file.Tail(v.rows)
	if err != nil {
		v.err = err.Error()
		return
	}
	a.loadViewer(offset)
}
func (a *App) viewerRefresh() {
	v := a.viewer
	atEnd := v.page.EOF
	if err := v.file.Refresh(); err != nil {
		v.err = err.Error()
		return
	}
	if atEnd || v.page.Offset >= v.file.Size {
		a.viewerEnd()
	} else {
		a.loadViewer(v.page.Offset)
	}
}
func (a *App) viewerKey(e *tcell.EventKey) {
	v := a.viewer
	switch e.Key() {
	case tcell.KeyEscape:
		a.closeViewer()
	case tcell.KeyUp:
		a.scrollViewer(-1)
	case tcell.KeyDown:
		a.scrollViewer(1)
	case tcell.KeyPgUp:
		a.scrollViewer(-v.rows)
	case tcell.KeyPgDn:
		a.scrollViewer(v.rows)
	case tcell.KeyHome:
		v.column = 0
		a.loadViewer(0)
	case tcell.KeyEnd:
		a.viewerEnd()
	case tcell.KeyLeft:
		v.column = max(0, v.column-8)
	case tcell.KeyRight:
		v.column = min(viewer.ChunkSize*4, v.column+8)
	case tcell.KeyCtrlL:
		a.screen.Sync()
	case tcell.KeyCtrlF:
		a.closeViewer()
		a.openFind()
	case tcell.KeyRune:
		switch e.Str() {
		case "q":
			a.closeViewer()
		case "j":
			a.scrollViewer(1)
		case "k":
			a.scrollViewer(-1)
		case " ":
			a.scrollViewer(v.rows)
		case "b":
			a.scrollViewer(-v.rows)
		case "g", "v":
			v.column = 0
			a.loadViewer(0)
		case "G", "t":
			a.viewerEnd()
		case "h":
			v.column = max(0, v.column-8)
		case "l":
			v.column = min(viewer.ChunkSize*4, v.column+8)
		case "r":
			a.viewerRefresh()
		case "f":
			a.closeViewer()
			a.openFind()
		}
	}
}
func (a *App) viewerMouse(e *tcell.EventMouse) {
	switch {
	case e.Buttons()&tcell.WheelUp != 0:
		a.scrollViewer(-3)
	case e.Buttons()&tcell.WheelDown != 0:
		a.scrollViewer(3)
	case e.Buttons()&tcell.Button2 != 0:
		a.closeViewer()
	}
}
func (a *App) drawViewer(w, h int) {
	v := a.viewer
	if rows := min(viewer.MaxRows, max(1, h-8)); v.rows != rows {
		v.rows = rows
		a.loadViewer(v.page.Offset)
	}
	a.text(2, 2, w-4, "FILE VIEW · "+v.file.Path, base.Foreground(accent).Bold(true))
	a.text(2, 3, w-4, "Read-only · arrows / mouse scroll · ← → pan long lines", base.Foreground(muted))
	for row, line := range v.page.Lines {
		text := clean(strings.ReplaceAll(line.Text, "\t", "    "))
		// Trim by terminal-cell width, preserving complete graphemes.
		if v.column > 0 {
			text = text[len(displaywidth.TruncateString(text, v.column, "")):]
		}
		if line.Continued {
			text = "↪ " + text
		}
		a.text(2, 5+row, w-4, text, base)
	}
	if len(v.page.Lines) == 0 && v.err == "" {
		a.text(2, 5, w-4, "(empty file)", base.Foreground(muted))
	}
	position := ""
	if v.page.EOF {
		position = " · END"
	}
	status := fmt.Sprintf("Bytes %d–%d / %s · column %d%s", v.page.Offset, v.page.Next, Bytes(uint64(v.file.Size)), v.column+1, position)
	if v.err != "" {
		status = "Read error: " + v.err
	}
	a.text(2, h-3, w-4, status, base.Foreground(muted))
	a.text(2, h-2, w-4, "PgUp/PgDn page · Home/End jump · r refresh · Esc/q close", base.Foreground(accent))
}
