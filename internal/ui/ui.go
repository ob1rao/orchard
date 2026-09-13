// Package ui implements the keyboard and mouse driven terminal interface.
package ui

import (
	"context"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/treemap"
	"github.com/ob1rao/orchard/internal/volumes"
)

var (
	bg      = tcell.NewHexColor(0x0d1420)
	fg      = tcell.NewHexColor(0xdce7f4)
	muted   = tcell.NewHexColor(0x8d9fb6)
	accent  = tcell.NewHexColor(0x6ce5c0)
	base    = tcell.StyleDefault.Background(bg).Foreground(fg)
	palette = []int32{0x245d73, 0x365b89, 0x5b4c82, 0x7a4b68, 0x45684c, 0x796437, 0x316c6b, 0x72563e}
)

type App struct {
	screen                         tcell.Screen
	volumes                        []volumes.Volume
	disk, offset, selected         int
	tree                           *scan.Tree
	current                        *scan.Node
	view                           scan.View
	entries                        []scan.Entry
	tiles                          []treemap.Tile
	cancel                         context.CancelFunc
	done                           <-chan struct{}
	opt                            scan.Options
	apparent                       bool
	picker, help, searching        bool
	filter, notice                 string
	listWidth, listTop, listHeight int
	lastNode                       *scan.Node
	lastClick                      time.Time
	mouseDown                      bool
}

func Run(ctx context.Context, path string, apparent bool, opt scan.Options) error {
	s, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err = s.Init(); err != nil {
		return err
	}
	defer s.Fini()
	s.SetStyle(base)
	s.HideCursor()
	s.EnableMouse(tcell.MouseButtonEvents)
	a := &App{screen: s, picker: true, apparent: apparent, opt: opt}
	defer a.stop()
	a.volumes, err = volumes.List()
	if err != nil {
		a.notice = err.Error()
	}
	if path != "" {
		if err = a.start(ctx, path); err != nil {
			return err
		}
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	a.draw()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if a.tree != nil && !a.view.Stats.Done {
				a.refresh()
				a.draw()
			}
		case ev, ok := <-s.EventQ():
			if !ok {
				return nil
			}
			switch e := ev.(type) {
			case *tcell.EventKey:
				if a.key(ctx, e) {
					return nil
				}
			case *tcell.EventMouse:
				a.mouse(ctx, e)
			case *tcell.EventResize:
				s.Sync()
			}
			a.draw()
		}
	}
}
func (a *App) stop() {
	if a.cancel != nil {
		a.cancel()
		<-a.done
		a.cancel = nil
	}
}
func (a *App) start(ctx context.Context, path string) error {
	a.stop()
	scanCtx, cancel := context.WithCancel(ctx)
	t, done, err := scan.Start(scanCtx, path, a.opt)
	if err != nil {
		cancel()
		a.notice = err.Error()
		return err
	}
	a.tree = t
	a.current = t.Root
	a.cancel = cancel
	a.done = done
	a.picker = false
	a.filter = ""
	a.notice = ""
	a.selected = 0
	a.offset = 0
	a.refresh()
	return nil
}
func (a *App) refresh() {
	var selected *scan.Node
	if a.selected < len(a.entries) {
		selected = a.entries[a.selected].Node
	}
	a.view = a.tree.Snapshot(a.current, a.apparent)
	a.entries = a.entries[:0]
	for _, e := range a.view.Entries {
		if strings.Contains(strings.ToLower(e.Name), strings.ToLower(a.filter)) {
			a.entries = append(a.entries, e)
		}
	}
	a.selected = min(a.selected, max(0, len(a.entries)-1))
	for i, e := range a.entries {
		if e.Node == selected {
			a.selected = i
			break
		}
	}
}
func (a *App) enter(n *scan.Node) {
	if !n.Dir {
		return
	}
	a.current = n
	a.filter = ""
	a.entries = nil
	a.selected = 0
	a.offset = 0
	a.refresh()
}
func (a *App) back() {
	if a.current != nil && a.current.Parent != nil {
		a.enter(a.current.Parent)
	}
}
func (a *App) move(delta int) {
	if a.picker {
		a.disk = max(0, min(len(a.volumes)-1, a.disk+delta))
	} else {
		a.selected = max(0, min(len(a.entries)-1, a.selected+delta))
	}
}
func (a *App) key(ctx context.Context, e *tcell.EventKey) bool {
	if e.Key() == tcell.KeyCtrlC {
		return true
	}
	if a.help {
		a.help = false
		return false
	}
	if a.searching {
		switch e.Key() {
		case tcell.KeyEscape:
			a.searching = false
			a.filter = ""
		case tcell.KeyEnter:
			a.searching = false
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			r := []rune(a.filter)
			if len(r) > 0 {
				a.filter = string(r[:len(r)-1])
			}
		default:
			if e.Key() == tcell.KeyRune {
				a.filter += e.Str()
			}
		}
		a.refresh()
		return false
	}
	switch e.Key() {
	case tcell.KeyEscape:
		if a.filter != "" {
			a.filter = ""
			a.refresh()
		} else if !a.picker {
			a.back()
		}
	case tcell.KeyUp:
		a.move(-1)
	case tcell.KeyDown:
		a.move(1)
	case tcell.KeyPgUp:
		a.move(-max(1, a.listHeight))
	case tcell.KeyPgDn:
		a.move(max(1, a.listHeight))
	case tcell.KeyHome:
		if a.picker {
			a.disk = 0
		} else {
			a.selected = 0
		}
	case tcell.KeyEnd:
		if a.picker {
			a.disk = max(0, len(a.volumes)-1)
		} else {
			a.selected = max(0, len(a.entries)-1)
		}
	case tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
		if !a.picker {
			a.back()
		}
	case tcell.KeyEnter, tcell.KeyRight:
		if a.picker {
			if len(a.volumes) > 0 {
				_ = a.start(ctx, a.volumes[a.disk].Path)
			}
		} else if len(a.entries) > 0 {
			a.enter(a.entries[a.selected].Node)
		}
	case tcell.KeyCtrlL:
		a.screen.Sync()
	case tcell.KeyRune:
		switch e.Str() {
		case "q":
			return true
		case "?":
			a.help = true
		case "j":
			a.move(1)
		case "k":
			a.move(-1)
		case "h":
			if !a.picker {
				a.back()
			}
		case "l":
			if !a.picker && len(a.entries) > 0 {
				a.enter(a.entries[a.selected].Node)
			}
		case "g":
			if !a.picker && a.tree != nil {
				a.enter(a.tree.Root)
			}
		case "d":
			a.picker = true
			a.offset = 0
			a.volumes, _ = volumes.List()
		case "a":
			a.apparent = !a.apparent
			if a.tree != nil {
				a.refresh()
			}
		case "/":
			if !a.picker {
				a.searching = true
				a.filter = ""
				a.refresh()
			}
		case "s":
			if !a.picker && a.cancel != nil {
				a.cancel()
				a.notice = "Scan stopped. Partial results remain navigable."
			}
		case "r":
			if a.picker {
				var err error
				a.volumes, err = volumes.List()
				a.disk = min(a.disk, max(0, len(a.volumes)-1))
				if err != nil {
					a.notice = err.Error()
				}
			} else {
				_ = a.start(ctx, a.tree.Root.Path())
			}
		}
	}
	return false
}
func (a *App) mouse(ctx context.Context, e *tcell.EventMouse) {
	if a.help || a.searching {
		return
	}
	b := e.Buttons()
	x, y := e.Position()
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
		if !a.picker {
			a.back()
		}
		return
	}
	if b&tcell.Button1 == 0 {
		return
	}
	if a.picker {
		index := (y-6)/3 + a.offset
		if y >= 6 && y < 6+a.listHeight*3 && index >= 0 && index < len(a.volumes) {
			a.disk = index
			_ = a.start(ctx, a.volumes[index].Path)
		}
		return
	}
	if y == 2 {
		a.back()
		return
	}
	index := -1
	if x < a.listWidth && y >= a.listTop && y < a.listTop+a.listHeight {
		index = a.offset + y - a.listTop
	} else {
		for _, t := range a.tiles {
			if t.Contains(x, y) {
				index = t.Index
				break
			}
		}
	}
	if index < 0 || index >= len(a.entries) {
		return
	}
	a.selected = index
	n := a.entries[index].Node
	if a.lastNode == n && time.Since(a.lastClick) < 450*time.Millisecond {
		a.enter(n)
		a.lastNode = nil
	} else {
		a.lastNode = n
		a.lastClick = time.Now()
	}
}

// clean prevents filenames and mount paths from injecting terminal controls.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 {
			return '�'
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
}
func (a *App) text(x, y, width int, s string, style tcell.Style) {
	if width <= 0 {
		return
	}
	s = clean(s)
	if displaywidth.String(s) > width {
		s = displaywidth.TruncateString(s, width, "…")
	}
	a.screen.PutStrStyled(x, y, s, style)
}
func Bytes(n uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
func percent(n, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}
func (a *App) draw() {
	s := a.screen
	s.Clear()
	w, h := s.Size()
	a.tiles = nil
	if w < 50 || h < 16 {
		a.text(1, 1, w-2, "orchard · resize terminal to at least 50 × 16", base)
		s.Show()
		return
	}
	a.text(2, 0, w-4, "ORCHARD  /  see where your space goes", base.Foreground(accent).Bold(true))
	if a.picker {
		a.drawPicker(w, h)
	} else {
		a.drawTree(w, h)
	}
	if a.help {
		a.drawHelp(w, h)
	}
	s.Show()
}
func (a *App) drawPicker(w, h int) {
	a.text(2, 2, w-4, "Select a disk to explore", base.Bold(true))
	a.text(2, 3, w-4, "Mounted filesystems · read-only scan · no files are modified", base.Foreground(muted))
	a.listHeight = max(1, (h-10)/3)
	a.disk = max(0, min(a.disk, len(a.volumes)-1))
	if a.disk < a.offset {
		a.offset = a.disk
	}
	if a.disk >= a.offset+a.listHeight {
		a.offset = a.disk - a.listHeight + 1
	}
	for row := 0; row < a.listHeight && a.offset+row < len(a.volumes); row++ {
		i := a.offset + row
		v := a.volumes[i]
		y := 6 + row*3
		style := base
		if i == a.disk {
			style = style.Background(tcell.NewHexColor(0x203448))
			a.screen.FillArea(1, y, w-2, 2, ' ', style)
		}
		marker := "  "
		if i == a.disk {
			marker = "› "
		}
		a.text(2, y, w-4, fmt.Sprintf("%s%s  ·  %s  [%s]", marker, v.Path, v.Device, v.Type), style.Bold(true))
		used := v.Total - min(v.Free, v.Total)
		a.text(4, y+1, w-6, fmt.Sprintf("%s used / %s total   %.0f%%   ·   %s available", Bytes(used), Bytes(v.Total), percent(used, v.Total), Bytes(v.Available)), style.Foreground(muted))
	}
	if len(a.volumes) == 0 {
		a.text(2, 6, w-4, "No disks found. Try: orchard /path/to/folder", base)
	}
	a.text(2, h-3, w-4, a.notice, base.Foreground(tcell.ColorYellow))
	a.text(2, h-2, w-4, "↑↓ select  ·  Enter / click scan  ·  r refresh  ·  ? help  ·  q quit", base.Foreground(accent))
}
func (a *App) drawTree(w, h int) {
	st := a.view.Stats
	state := "SCANNING"
	if st.Done {
		state = "COMPLETE"
		if st.Errors > 0 {
			state = "COMPLETE · unreadable entries"
		}
		if st.Cancelled {
			state = "STOPPED · partial results"
		}
	}
	mode := "allocated"
	if a.apparent {
		mode = "apparent"
	}
	a.text(2, 2, w-4, "‹  "+a.current.Path(), base.Bold(true))
	rate := float64(st.Files+st.Directories) / max(0.001, st.Elapsed.Seconds())
	a.text(2, 3, w-4, fmt.Sprintf("%s  ·  %s %s  ·  %d files / %d dirs  ·  %.0f entries/s  ·  %s", state, Bytes(a.view.Size), mode, st.Files, st.Directories, rate, st.Elapsed.Round(time.Millisecond)), base.Foreground(accent))
	a.listWidth = min(39, w/3)
	a.listTop = 6
	a.listHeight = h - 12
	a.text(2, 5, a.listWidth-3, fmt.Sprintf("CONTENTS  %d", len(a.entries)), base.Foreground(muted).Bold(true))
	title := "SPACE MAP"
	if a.filter != "" {
		title += " · filtered"
	}
	a.text(a.listWidth+2, 5, w-a.listWidth-4, title, base.Foreground(muted).Bold(true))
	if a.selected < a.offset {
		a.offset = a.selected
	}
	if a.selected >= a.offset+a.listHeight {
		a.offset = a.selected - a.listHeight + 1
	}
	for row := 0; row < a.listHeight && row+a.offset < len(a.entries); row++ {
		i := row + a.offset
		e := a.entries[i]
		style := base
		if i == a.selected {
			style = style.Background(tcell.NewHexColor(0x26445a)).Foreground(accent)
			a.screen.FillArea(1, a.listTop+row, a.listWidth-2, 1, ' ', style)
		}
		name := e.Name
		if e.Dir {
			name += "/"
		}
		size := Bytes(e.Size)
		a.text(2, a.listTop+row, a.listWidth-15, name, style)
		a.text(a.listWidth-12, a.listTop+row, 10, size, style)
	}
	weights := make([]uint64, len(a.entries))
	for i, e := range a.entries {
		weights[i] = e.Size
	}
	a.tiles = treemap.Layout(weights, treemap.Rect{X: a.listWidth + 1, Y: 6, W: w - a.listWidth - 3, H: a.listHeight})
	for _, t := range a.tiles {
		a.drawTile(t, a.entries[t.Index], t.Index == a.selected)
	}
	if len(a.tiles) == 0 {
		msg := "Waiting for file sizes…"
		if st.Done {
			msg = "No measurable entries here"
		}
		if a.filter != "" {
			msg = "No measurable matches"
		}
		a.text(a.listWidth+3, 8, w-a.listWidth-6, msg, base.Foreground(muted))
	}
	if len(a.entries) > 0 {
		e := a.entries[a.selected]
		a.text(2, h-5, w-4, fmt.Sprintf("%s  ·  %s  ·  %.2f%% of this directory", e.Node.Path(), Bytes(e.Size), percent(e.Size, a.view.Size)), base.Bold(true))
	}
	detail := fmt.Sprintf("%d unreadable  ·  %d mount/duplicate dirs skipped  ·  %d hard links deduplicated", st.Errors, st.Skipped, st.Hardlinks)
	if a.notice != "" {
		detail = a.notice
	} else if st.LastError != "" {
		detail = st.LastError
	}
	a.text(2, h-4, w-4, detail, base.Foreground(muted))
	if a.searching || a.filter != "" {
		a.text(2, h-3, w-4, "Filter: /"+a.filter+"  (Esc clears)", base.Foreground(accent))
	} else {
		a.text(2, h-3, w-4, fmt.Sprintf("%d / %d entries drawn · tiny/zero entries remain in list · a: switch size metric", len(a.tiles), len(a.entries)), base.Foreground(muted))
	}
	a.text(2, h-2, w-4, "↑↓ select · Enter / double-click open · ← back · / filter · d disks · ? help · q quit", base.Foreground(accent))
}
func (a *App) drawTile(t treemap.Tile, e scan.Entry, selected bool) {
	key := strings.ToLower(filepath.Ext(e.Name))
	if e.Dir {
		key = e.Name
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	color := tcell.NewHexColor(palette[int(hash.Sum32())%len(palette)])
	style := base.Background(color)
	border := style.Foreground(tcell.NewHexColor(0xa4b9cb))
	if selected {
		border = style.Foreground(accent).Bold(true)
	}
	a.screen.FillArea(t.X, t.Y, t.W, t.H, ' ', style)
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
		name := e.Name
		if e.Dir {
			name += "/"
		}
		if selected {
			name = "› " + name
		}
		a.text(t.X+1, t.Y+1, t.W-2, name, style.Bold(true))
		if t.H >= 4 {
			a.text(t.X+1, t.Y+2, t.W-2, Bytes(e.Size), style)
		}
	} else {
		marker := ""
		if selected {
			marker = "›"
		}
		a.text(t.X, t.Y, t.W, marker+e.Name, border)
	}
}
func (a *App) drawHelp(w, h int) {
	a.screen.FillArea(1, 1, w-2, h-2, ' ', base)
	lines := []string{"KEYBOARD & MOUSE", "", "↑/↓ or j/k   Select an entry; wheel scrolls", "Enter / l   Open selected directory", "← / h / Backspace   Parent directory", "g   Return to the scan root", "Click   Select tile or list entry", "Double-click   Open directory; right-click goes back", "/   Filter names in the current directory; Esc clears", "a   Toggle allocated bytes / apparent file sizes", "s   Stop scan and browse partial results", "r   Rescan disk     d   Choose another disk", "Home / End / PgUp / PgDn   Navigate the list", "q / Ctrl-C   Quit     Ctrl-L   Redraw", "", "Tiles represent immediate children, ordered by size.", "Directories open into another treemap. Tiny entries stay in the list.", "Symlinks are not followed; other filesystems are skipped.", "Hard links count once; filesystem overhead/free space is not mapped.", "", "Press any key to close"}
	for i, line := range lines {
		if i+2 >= h-2 {
			break
		}
		style := base
		if i == 0 {
			style = base.Foreground(accent).Bold(true)
		}
		a.text(3, i+2, w-6, line, style)
	}
}
