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
	hideHidden                     bool
	picker, help, searching        bool
	filter, notice                 string
	listWidth, listTop, listHeight int
	lastNode                       *scan.Node
	lastClick                      time.Time
	mouseDown                      bool
	find                           *findState
	findResults                    chan findUpdate
	findSerial                     uint64
	viewer                         *fileView
	mapPages                       []mapPage
	mapReturnSelected              *scan.Node
	showUnmounted, diskLoading     bool
	diskResults                    chan diskUpdate
	diskSerial                     uint64
	diskCancel                     context.CancelFunc
	mount                          *mountForm
	mountVolume                    func(context.Context, volumes.Volume, string) (string, error)
	fatal                          error
	hideDiskSpace                  bool
	diskSpace                      volumes.Space
	spaceReady, spaceLoading       bool
	spaceErr                       error
	spaceSerial                    uint64
	spaceNext                      time.Time
	spaceResults                   chan spaceUpdate
	pickerRows                     map[int]int
	pickerTiles                    []pickerTile
	lastPick                       string
	brandFrame                     int
}

func Run(ctx context.Context, path string, apparent, showUnmounted bool, opt scan.Options) error {
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
	a := &App{screen: s, picker: true, apparent: apparent, opt: opt, showUnmounted: showUnmounted}
	defer a.stop()
	a.reloadDisks(ctx)
	defer func() {
		if a.diskCancel != nil {
			a.diskCancel()
		}
	}()
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
		case update := <-a.spaceResults:
			a.acceptSpace(update)
			a.draw()
		case update := <-a.diskResults:
			a.acceptDisks(update)
			a.draw()
		case update := <-a.findResults:
			a.acceptFind(update)
			a.draw()
		case <-tick.C:
			a.pollSpace(ctx, time.Now())
			redraw := false
			if a.picker && a.diskLoading {
				a.brandFrame++
				redraw = true
			}
			if a.tree != nil && !a.view.Stats.Done {
				a.refresh()
				redraw = true
			}
			if a.pollFind() {
				redraw = true
			}
			if redraw {
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
			if a.fatal != nil {
				return a.fatal
			}
			a.draw()
		}
	}
}
func (a *App) stop() {
	a.closeViewer()
	a.closeFind()
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
	a.mapPages = nil
	a.spaceSerial++
	a.spaceReady, a.spaceLoading = false, false
	a.spaceNext = time.Time{}
	a.tree = t
	a.pollSpace(ctx, time.Now())
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
		if a.hideHidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
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
	a.mapPages = nil
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
	if a.find != nil {
		a.find.selected = max(0, min(len(a.find.entries)-1, a.find.selected+delta))
		return
	}
	if a.picker {
		a.disk = a.pickerStep(delta)
	} else {
		lo, hi := a.mapBounds()
		a.selected = max(lo, min(hi-1, a.selected+delta))
	}
}

// pickerStep moves the selection through the grouped order the picker draws,
// which is not the order volumes were discovered in. Regrouping costs nothing
// at this size and cannot fall out of step with the current volume list.
func (a *App) pickerStep(delta int) int {
	order := storageOrder(splitStorage(groupStorage(a.volumes)))
	if len(order) == 0 {
		return 0
	}
	at := 0
	for i, index := range order {
		if index == a.disk {
			at = i
		}
	}
	return order[max(0, min(len(order)-1, at+delta))]
}

func (a *App) key(ctx context.Context, e *tcell.EventKey) bool {
	if e.Key() == tcell.KeyCtrlC {
		return true
	}
	if a.mount != nil {
		a.mountKey(ctx, e)
		return false
	}
	if a.viewer != nil {
		a.viewerKey(e)
		return false
	}
	if a.help {
		a.help = false
		return false
	}
	if a.find != nil {
		a.findKey(e)
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
	case tcell.KeyCtrlF:
		if !a.picker {
			a.openFind()
		}
	case tcell.KeyEscape:
		if a.filter != "" {
			a.filter = ""
			a.mapPages = nil
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
			a.disk = a.pickerStep(-len(a.volumes))
		} else {
			a.selected, _ = a.mapBounds()
		}
	case tcell.KeyEnd:
		if a.picker {
			a.disk = a.pickerStep(len(a.volumes))
		} else {
			_, hi := a.mapBounds()
			a.selected = max(0, hi-1)
		}
	case tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
		if !a.picker {
			a.back()
		}
	case tcell.KeyEnter, tcell.KeyRight:
		if a.picker {
			a.selectDisk(ctx)
		} else if len(a.entries) > 0 {
			a.enter(a.entries[a.selected].Node)
		}
	case tcell.KeyCtrlL:
		a.screen.Sync()
	case tcell.KeyRune:
		switch e.Str() {
		case "i":
			if !a.picker {
				a.hideDiskSpace = !a.hideDiskSpace
				a.pollSpace(ctx, time.Now())
			}
		case "u":
			if a.picker {
				a.showUnmounted = !a.showUnmounted
				a.reloadDisks(ctx)
			}
		case " ":
			if !a.picker {
				a.nextMapPage()
			}
		case "b":
			if !a.picker {
				a.previousMapPage()
			}
		case "f":
			if !a.picker {
				a.openFind()
			}
		case "v", "t":
			a.openViewer(e.Str() == "t")
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
			a.mapPages = nil
			a.picker = true
			a.offset = 0
			a.reloadDisks(ctx)
		case "a":
			a.mapPages = nil
			a.apparent = !a.apparent
			if a.tree != nil {
				a.refresh()
			}
		case ".", "H":
			if !a.picker {
				a.mapPages = nil
				a.hideHidden = !a.hideHidden
				a.refresh()
			}
		case "/":
			if !a.picker {
				a.mapPages = nil
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
				a.reloadDisks(ctx)
			} else {
				_ = a.start(ctx, a.tree.Root.Path())
			}
		}
	}
	return false
}
func (a *App) mouse(ctx context.Context, e *tcell.EventMouse) {
	if a.mount != nil {
		if e.Buttons() == tcell.ButtonNone {
			a.mouseDown = false
		}
		if e.Buttons()&tcell.Button2 != 0 {
			a.mount = nil
			a.mouseDown = false
		}
		return
	}
	if a.viewer != nil {
		a.viewerMouse(e)
		return
	}
	if a.find != nil {
		a.findMouse(e)
		return
	}
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
		// The map shares its rows with the list, so the column decides which
		// of them a click belongs to.
		if index, hit := a.pickerRows[y]; hit && x < a.listWidth && index < len(a.volumes) {
			a.disk = index
			a.selectDisk(ctx)
			return
		}
		for _, t := range a.pickerTiles {
			if !t.Contains(x, y) || t.volume >= len(a.volumes) {
				continue
			}
			a.disk = t.volume
			// One click on the map selects, so a reader can compare tiles
			// without starting a scan of whichever one they landed on.
			pick := a.volumes[t.volume].Device + "\x00" + a.volumes[t.volume].Path
			if a.lastPick == pick && time.Since(a.lastClick) < 450*time.Millisecond {
				a.lastPick = ""
				a.selectDisk(ctx)
			} else {
				a.lastPick, a.lastClick = pick, time.Now()
			}
			return
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
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	v := float64(n)
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
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
	// Drop every hit target before laying out again: a click arriving against
	// a layout that is no longer on screen would select something arbitrary.
	a.tiles, a.pickerRows, a.pickerTiles = nil, map[int]int{}, nil
	if w < 50 || h < 16 {
		a.text(1, 1, w-2, "orchard · resize terminal to at least 50 × 16", base)
		s.Show()
		return
	}
	if !a.picker || a.mount != nil {
		a.text(2, 0, w-4, "ORCHARD  /  see where your space goes", base.Foreground(accent).Bold(true))
	}
	if a.mount != nil {
		a.drawMount(w, h)
	} else if a.viewer != nil {
		a.drawViewer(w, h)
	} else if a.find != nil {
		a.drawFind(w, h)
	} else if a.picker {
		a.drawPicker(w, h)
	} else {
		a.drawTree(w, h)
	}
	if a.help {
		a.drawHelp(w, h)
	}
	s.Show()
}
func (a *App) drawTree(w, h int) {
	a.drawDiskSpace(w)
	st := a.view.Stats
	state := "SCANNING"
	if st.Done {
		state = "COMPLETE"
		if st.Errors > 0 {
			state = fmt.Sprintf("COMPLETE · %d unreadable entries", st.Errors)
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
	if len(a.mapPages) > 0 {
		a.drawFullMap(w, h)
		return
	}
	a.listWidth = min(44, max(26, w/3))
	dateColumns := w >= 150
	if dateColumns {
		a.listWidth = min(80, w/2)
	}
	nameWidth, sizeX := a.listWidth-15, a.listWidth-12
	if dateColumns {
		nameWidth -= 24
		sizeX -= 24
	}
	a.listTop = 6
	a.listHeight = h - 14
	a.text(2, 5, nameWidth, fmt.Sprintf("CONTENTS %d", len(a.entries)), base.Foreground(muted).Bold(true))
	a.text(sizeX, 5, 10, "      SIZE", base.Foreground(muted).Bold(true))
	if dateColumns {
		a.text(a.listWidth-24, 5, 10, "MODIFIED", base.Foreground(muted))
		a.text(a.listWidth-12, 5, 10, "CREATED", base.Foreground(muted))
	}
	title := "TREEMAP"
	if a.filter != "" || a.hideHidden {
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
		a.text(2, a.listTop+row, nameWidth, name, style)
		a.text(sizeX, a.listTop+row, 10, fmt.Sprintf("%10s", size), style)
		if dateColumns {
			a.text(a.listWidth-24, a.listTop+row, 10, date(e.Node.Modified), style)
			a.text(a.listWidth-12, a.listTop+row, 10, createdDate(e.Node), style)
		}
	}
	weights := make([]uint64, len(a.entries))
	for i, e := range a.entries {
		weights[i] = e.Size
	}
	a.tiles = a.layoutWithFreeSpace(weights, treemap.Rect{X: a.listWidth + 1, Y: 6, W: w - a.listWidth - 3, H: a.listHeight})
	for _, t := range a.tiles {
		a.drawMapTile(t, false)
	}
	fileTiles := 0
	for _, tile := range a.tiles {
		if tile.Index >= 0 {
			fileTiles++
		}
	}
	if fileTiles == 0 {
		msg := "Waiting for file sizes…"
		if st.Done {
			msg = "No measurable entries here"
		}
		if a.filter != "" {
			msg = "No measurable matches"
		}
		if len(a.tiles) > 0 {
			a.text(2, 8, a.listWidth-4, msg, base.Foreground(muted))
		} else {
			a.text(a.listWidth+3, 8, w-a.listWidth-6, msg, base.Foreground(muted))
		}
	}
	if len(a.entries) > 0 {
		e := a.entries[a.selected]
		a.text(2, h-7, w-4, fmt.Sprintf("%s  ·  %s  ·  %.2f%% of this directory", e.Node.Path(), Bytes(e.Size), percent(e.Size, a.view.Size)), base.Bold(true))
		a.drawDates(e.Node, w, h)
	} else {
		a.drawDates(a.current, w, h)
	}
	detail := fmt.Sprintf("%d unreadable  ·  %d mount/duplicate dirs skipped  ·  %d hard links deduplicated", st.Errors, st.Skipped, st.Hardlinks)
	if a.notice != "" {
		detail = a.notice
	} else if st.LastError != "" {
		detail = st.LastError
	}
	a.text(2, h-4, w-4, detail, base.Foreground(muted))
	hidden := "on"
	if a.hideHidden {
		hidden = "off"
	}
	first := 0
	if len(a.entries) > 0 {
		first = a.offset + 1
	}
	status := fmt.Sprintf("Hidden: %s (.) · entries %d–%d of %d · %d tiles · a: size metric", hidden, first, min(a.offset+a.listHeight, len(a.entries)), len(a.entries), len(a.tiles))
	if a.searching || a.filter != "" {
		status = fmt.Sprintf("Hidden: %s (.) · Filter: /%s (Esc clears)", hidden, a.filter)
	}
	a.text(2, h-3, w-4, status, base.Foreground(muted))
	a.text(2, h-2, w-4, "Space treemap · Enter open · ← back · i disk space · f find · v view · t tail · ? help · q quit", base.Foreground(accent))
}
func (a *App) drawTile(t treemap.Tile, e scan.Entry, selected, wrapName bool) {
	key := strings.ToLower(filepath.Ext(e.Name))
	if e.Dir {
		key = e.Name
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	// Keep the hash unsigned: converting to int can go negative on 32-bit Pis.
	color := tcell.NewHexColor(palette[hash.Sum32()%uint32(len(palette))])
	if t.Index == freeSpaceTile {
		color = tcell.NewHexColor(0x000000)
	}
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
		if wrapName {
			a.drawSoloName(t, e, style)
			return
		}
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
	lines := []string{"KEYBOARD & MOUSE", "", "↑/↓ or j/k   Select an entry; wheel scrolls", "Enter / l   Open selected directory", "← / h / Backspace   Parent directory", "g   Return to the scan root", "Space   Expand treemap / next smaller entries     b   Previous treemap view", "Click   Select tile or list entry", "Double-click   Open directory; right-click goes back", "/   Filter current directory     f / Ctrl-F   Search disk", ". / H   Show or hide dotfiles and dot directories (default: shown)", "v   View selected file     t   View from the end (no follow)", "a   Toggle allocated bytes / apparent file sizes", "i   Show/hide disk free space and unaccounted usage estimate", "s   Stop scan and browse partial results", "r   Rescan disk     d   Choose another disk", "u   In disk picker: show/hide unmounted volumes", "In the disk picker, a map tile is one volume inside its physical disk;", "click a tile to select it and double-click to explore it.", "Home / End / PgUp / PgDn   Navigate the list", "q / Ctrl-C   Quit     Ctrl-L   Redraw", "", "Other usage includes inaccessible data, overhead and data outside the scan.", "Its byte count is an estimate, not a measure of permission errors.", "Tiles represent immediate children, ordered by size.", "Directories open into another treemap. Tiny entries stay in the list.", "Symlinks are not followed; other filesystems are skipped.", "Hard links count once; black tiles show disk free space (i toggles).", "", "Press any key to close"}
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
