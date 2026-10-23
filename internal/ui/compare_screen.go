package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/diff"
	"github.com/ob1rao/orchard/internal/snapshot"
	"github.com/ob1rao/orchard/internal/treemap"
)

// compareScreen browses the difference between two trees. Tiles are sized by
// how much an entry changed, not by how big it is, so the map points at
// whatever moved.
type compareScreen struct {
	list
	before, after snapshot.Meta
	root, current *diff.Node
	entries       []*diff.Node
	hideSame      bool
	tiles         []treemap.Tile
	top, height   int
	listWidth     int
}

var (
	grewColor   = tcell.NewHexColor(0xf08a74)
	shrankColor = tcell.NewHexColor(0x7fd6a4)
)

func (s *compareScreen) typing() bool { return false }

func (s *compareScreen) refresh(a *App) {
	var keep *diff.Node
	if s.selected < len(s.entries) {
		keep = s.entries[s.selected]
	}
	s.entries = diff.Sorted(s.current, a.apparent, s.hideSame)
	s.selected = 0
	for i, n := range s.entries {
		if n == keep {
			s.selected = i
		}
	}
}

func (s *compareScreen) enter(a *App, n *diff.Node) {
	if !n.Dir || len(n.Children) == 0 {
		return
	}
	s.current, s.entries, s.offset = n, nil, 0
	s.refresh(a)
}

func (s *compareScreen) up(a *App, ctx context.Context) {
	if s.current.Parent == nil {
		a.leaveScreen(ctx)
		return
	}
	from := s.current
	s.current, s.entries, s.offset = s.current.Parent, nil, 0
	s.refresh(a)
	for i, n := range s.entries {
		if n == from {
			s.selected = i
		}
	}
}

func (s *compareScreen) key(a *App, ctx context.Context, e *tcell.EventKey) {
	if s.entries == nil {
		s.refresh(a)
	}
	if s.list.navigate(e, len(s.entries), s.height) {
		return
	}
	switch e.Key() {
	case tcell.KeyEscape:
		a.leaveScreen(ctx)
	case tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
		s.up(a, ctx)
	case tcell.KeyEnter, tcell.KeyRight:
		if len(s.entries) > 0 {
			s.enter(a, s.entries[s.selected])
		}
	case tcell.KeyRune:
		switch e.Str() {
		case "h":
			s.up(a, ctx)
		case "l":
			if len(s.entries) > 0 {
				s.enter(a, s.entries[s.selected])
			}
		case "g":
			s.current, s.entries = s.root, nil
			s.refresh(a)
		case "u":
			s.hideSame = !s.hideSame
			s.refresh(a)
		case "a":
			a.apparent = !a.apparent
			s.refresh(a)
		}
	}
}

func (s *compareScreen) mouse(a *App, ctx context.Context, e *tcell.EventMouse) {
	if rightClick(e) {
		if !a.mouseDown {
			a.mouseDown = true
			s.up(a, ctx)
		}
		return
	}
	x, y := e.Position()
	if x < s.listWidth {
		if i, double := a.click(&s.list, e, s.top, s.height, len(s.entries)); i >= 0 && double {
			s.enter(a, s.entries[i])
		}
		return
	}
	if e.Buttons()&tcell.Button1 == 0 || a.mouseDown {
		if e.Buttons() == tcell.ButtonNone {
			a.mouseDown = false
		}
		return
	}
	a.mouseDown = true
	for _, t := range s.tiles {
		if t.Contains(x, y) && t.Index < len(s.entries) {
			key := fmt.Sprintf("tile:%p", s.entries[t.Index])
			double := a.lastRow == key && time.Since(a.lastClick) < 450*time.Millisecond
			s.selected = t.Index
			a.lastRow, a.lastClick = key, time.Now()
			if double {
				a.lastRow = ""
				s.enter(a, s.entries[t.Index])
			}
			return
		}
	}
}

func statusMark(n *diff.Node) string {
	switch n.Status {
	case diff.Added:
		return "+"
	case diff.Removed:
		return "−"
	case diff.Changed:
		return "~"
	}
	return " "
}

func deltaStyle(style tcell.Style, d int64) tcell.Style {
	switch {
	case d > 0:
		return style.Foreground(grewColor)
	case d < 0:
		return style.Foreground(shrankColor)
	}
	return style.Foreground(muted)
}

func (s *compareScreen) draw(a *App, w, h int) {
	if s.entries == nil {
		s.refresh(a)
	}
	apparent := a.apparent
	cur := s.current
	a.text(2, 1, w-4, "COMPARE  "+snapshotTitle(s.before)+"  →  "+snapshotTitle(s.after), base.Foreground(accent).Bold(true))
	if s.before.Root != s.after.Root || s.before.Machine != s.after.Machine {
		a.text(2, 2, w-4, fmt.Sprintf("‹  %s  (before %s:%s, after %s:%s)", cur.Path(), s.before.Machine, s.before.Root, s.after.Machine, s.after.Root), base.Bold(true))
	} else {
		a.text(2, 2, w-4, "‹  "+s.after.Root+trimRoot(cur.Path()), base.Bold(true))
	}
	metric := "allocated"
	if apparent {
		metric = "apparent"
	}
	summary := fmt.Sprintf("%s → %s  ", Bytes(cur.Before(apparent)), Bytes(cur.After(apparent)))
	a.text(2, 3, w-4, summary, base.Foreground(accent))
	a.text(2+len([]rune(summary)), 3, w-4-len([]rune(summary)), fmt.Sprintf("%s %s  ·  %d added · %d removed · %d changed files", signedBytes(cur.Delta(apparent)), metric, cur.FilesAdded, cur.FilesRemoved, cur.FilesChanged), deltaStyle(base, cur.Delta(apparent)))

	s.listWidth = min(76, max(48, w/2))
	s.top, s.height = 6, max(1, h-13)
	s.clamp(len(s.entries), s.height)
	nameWidth := s.listWidth - 38
	a.text(2, 5, nameWidth, fmt.Sprintf("CHANGES %d", len(s.entries)), base.Foreground(muted).Bold(true))
	a.text(s.listWidth-36, 5, 34, "    BEFORE       AFTER       DELTA", base.Foreground(muted).Bold(true))
	for row := 0; row < s.height && row+s.offset < len(s.entries); row++ {
		i := row + s.offset
		n := s.entries[i]
		y := s.top + row
		style := base
		if i == s.selected {
			style = selectedStyle
			a.screen.FillArea(1, y, s.listWidth-2, 1, ' ', style)
		}
		name := statusMark(n) + " " + n.Name
		if n.Dir {
			name += "/"
		}
		a.text(2, y, nameWidth, name, style)
		before, after := "—", "—"
		if n.Status != diff.Added {
			before = Bytes(n.Before(apparent))
		}
		if n.Status != diff.Removed {
			after = Bytes(n.After(apparent))
		}
		a.text(s.listWidth-36, y, 10, fmt.Sprintf("%10s", before), style.Foreground(muted))
		a.text(s.listWidth-24, y, 10, fmt.Sprintf("%10s", after), style)
		a.text(s.listWidth-12, y, 11, fmt.Sprintf("%11s", signedBytes(n.Delta(apparent))), deltaStyle(style, n.Delta(apparent)))
	}
	if len(s.entries) == 0 {
		msg := "No differences here."
		if s.hideSame && len(cur.Children) > 0 {
			msg = "No differences here · u shows unchanged entries"
		}
		a.text(2, s.top, s.listWidth-4, msg, base.Foreground(muted))
	}

	a.text(s.listWidth+2, 5, w-s.listWidth-4, "CHANGE MAP · area is size of change · red grew · green shrank", base.Foreground(muted).Bold(true))
	weights := make([]uint64, len(s.entries))
	for i, n := range s.entries {
		weights[i] = n.Magnitude(apparent)
	}
	s.tiles = treemap.Layout(weights, treemap.Rect{X: s.listWidth + 1, Y: s.top, W: w - s.listWidth - 3, H: s.height})
	for _, t := range s.tiles {
		s.drawTile(a, t, s.entries[t.Index], t.Index == s.selected)
	}

	if len(s.entries) > 0 {
		n := s.entries[s.selected]
		a.text(2, h-6, w-4, fmt.Sprintf("%s%s  ·  %s  ·  %s", n.Path(), map[bool]string{true: "/"}[n.Dir], n.Status, signedBytes(n.Delta(apparent))), base.Bold(true))
		var dates string
		if n.Status != diff.Added {
			dates = "modified before " + time.Unix(n.BeforeMod, 0).Local().Format("2006-01-02 15:04")
		}
		if n.Status != diff.Removed {
			if dates != "" {
				dates += " · "
			}
			dates += "modified after " + time.Unix(n.AfterMod, 0).Local().Format("2006-01-02 15:04")
		}
		if n.Dir {
			dates += fmt.Sprintf(" · %d added · %d removed · %d changed files below", n.FilesAdded, n.FilesRemoved, n.FilesChanged)
		}
		a.text(2, h-5, w-4, dates, base.Foreground(muted))
	}
	a.text(2, h-4, w-4, a.notice, base.Foreground(tcell.ColorYellow))
	hidden := "hidden"
	if !s.hideSame {
		hidden = "shown"
	}
	a.text(2, h-3, w-4, fmt.Sprintf("+ added · − removed · ~ changed · unchanged entries %s (u) · a size metric", hidden), base.Foreground(muted))
	a.text(2, h-2, w-4, "Enter open · ← up · g top · Esc close · q quit", base.Foreground(accent))
}

func trimRoot(p string) string {
	if p == "/" {
		return ""
	}
	return p
}

func (s *compareScreen) drawTile(a *App, t treemap.Tile, n *diff.Node, selected bool) {
	d := n.Delta(a.apparent)
	color := tcell.NewHexColor(0x6b3a36)
	switch {
	case n.Status == diff.Added:
		color = tcell.NewHexColor(0x8a4630)
	case n.Status == diff.Removed:
		color = tcell.NewHexColor(0x2b5e57)
	case d < 0:
		color = tcell.NewHexColor(0x2f5a3f)
	}
	style := base.Background(color)
	a.screen.FillArea(t.X, t.Y, t.W, t.H, ' ', style)
	border := style.Foreground(tcell.NewHexColor(0xa4b9cb))
	if selected {
		border = style.Foreground(accent).Bold(true)
	}
	if t.W >= 3 && t.H >= 3 {
		for x := t.X + 1; x < t.X+t.W-1; x++ {
			a.screen.PutStrStyled(x, t.Y, "─", border)
			a.screen.PutStrStyled(x, t.Y+t.H-1, "─", border)
		}
		for y := t.Y + 1; y < t.Y+t.H-1; y++ {
			a.screen.PutStrStyled(t.X, y, "│", border)
			a.screen.PutStrStyled(t.X+t.W-1, y, "│", border)
		}
		a.screen.PutStrStyled(t.X, t.Y, "╭", border)
		a.screen.PutStrStyled(t.X+t.W-1, t.Y, "╮", border)
		a.screen.PutStrStyled(t.X, t.Y+t.H-1, "╰", border)
		a.screen.PutStrStyled(t.X+t.W-1, t.Y+t.H-1, "╯", border)
		name := statusMark(n) + " " + n.Name
		if n.Dir {
			name += "/"
		}
		if selected {
			name = "› " + name
		}
		a.text(t.X+1, t.Y+1, t.W-2, name, style.Bold(true))
		if t.H >= 4 {
			a.text(t.X+1, t.Y+2, t.W-2, signedBytes(d), style)
		}
		return
	}
	a.text(t.X, t.Y, t.W, n.Name, border)
}
