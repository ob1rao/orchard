package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/library"
	"github.com/ob1rao/orchard/internal/remote"
	"github.com/ob1rao/orchard/internal/snapshot"
)

// machinesScreen lists this machine and every saved profile with its most
// recent snapshot, and starts scans of them.
type machinesScreen struct {
	list
	machines []library.Machine // index 0 is this machine, with no Host
	history  map[string][]library.Entry
	confirm  string
	err      string
	top      int
	height   int
}

func (a *App) openMachinesScreen() {
	s := &machinesScreen{}
	s.reload(a)
	a.push(s)
}

func (s *machinesScreen) typing() bool { return false }

func (s *machinesScreen) reload(a *App) {
	lib, err := a.openLibrary()
	if err != nil {
		s.err = err.Error()
		return
	}
	ms, err := lib.Machines()
	if err != nil {
		s.err = err.Error()
	}
	s.machines = append([]library.Machine{{Name: snapshot.Hostname(), Notes: "this machine"}}, ms...)
	s.history = map[string][]library.Entry{}
	all, _ := lib.List()
	for _, e := range all {
		key := strings.ToLower(e.Machine)
		s.history[key] = append(s.history[key], e)
	}
}

func (s *machinesScreen) selectedMachine() library.Machine { return s.machines[s.selected] }

func (s *machinesScreen) key(a *App, ctx context.Context, e *tcell.EventKey) {
	confirm := s.confirm
	s.confirm = ""
	if s.list.navigate(e, len(s.machines), s.height) {
		return
	}
	m := s.selectedMachine()
	switch e.Key() {
	case tcell.KeyEscape, tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
		a.leaveScreen(ctx)
		return
	case tcell.KeyEnter, tcell.KeyRight:
		s.scan(a, ctx, m)
		return
	}
	if e.Key() != tcell.KeyRune {
		return
	}
	switch e.Str() {
	case "o":
		a.openLibraryScreen(m.Name)
	case "l":
		h := s.history[strings.ToLower(m.Name)]
		if len(h) == 0 {
			a.notice = m.Name + " has no snapshots yet. Enter scans it."
			return
		}
		a.loadSnapshot(h[0], func(a *App, snap *snapshot.Snapshot) { a.openSnapshot(snap) })
	case "n":
		a.push(machineForm(library.Machine{}))
	case "e":
		if m.Host == "" {
			a.notice = "This machine needs no profile."
			return
		}
		a.push(machineForm(m))
	case "x":
		if m.Host == "" {
			return
		}
		if confirm != m.Name {
			s.confirm = m.Name
			a.notice = "Press x again to remove the profile " + m.Name + "; its snapshots stay in the library."
			return
		}
		if err := a.lib.RemoveMachine(m.Name); err != nil {
			a.notice = err.Error()
		} else {
			a.notice = "Removed " + m.Name
		}
		s.reload(a)
	case "f", "F":
		a.openQueryScreen("machine:" + m.Name + " ")
	}
}

// scan starts a scan of the selected machine: the disk picker for this one,
// a path prompt and then an SSH scan for any other.
func (s *machinesScreen) scan(a *App, ctx context.Context, m library.Machine) {
	if m.Host == "" {
		a.screens = nil
		a.showPicker(ctx)
		return
	}
	form := &formScreen{
		title:  "SCAN " + m.Name + " OVER SSH · " + m.Host,
		action: "scan",
		fields: []formField{{label: "Directory", value: m.DefaultPath(), hint: "on " + m.Name}},
		note:   "Orchard runs there headless and streams the result back; the snapshot is saved to the library.",
	}
	form.submit = func(a *App, values []string) string {
		path := strings.TrimSpace(values[0])
		if path == "" {
			return "Enter a directory to scan."
		}
		a.pop()
		a.startRemote(ctx, m, path)
		return ""
	}
	a.push(form)
}

func (s *machinesScreen) mouse(a *App, ctx context.Context, e *tcell.EventMouse) {
	if rightClick(e) {
		a.leaveScreen(ctx)
		return
	}
	if i, double := a.click(&s.list, e, s.top, s.height, len(s.machines)); i >= 0 && double {
		s.scan(a, ctx, s.machines[i])
	}
}

func (s *machinesScreen) draw(a *App, w, h int) {
	a.text(2, 2, w-4, fmt.Sprintf("MACHINES · %s · Enter scans", plural(len(s.machines), "machine")), base.Foreground(accent).Bold(true))
	a.text(2, 3, w-4, "Remote scans use your ssh client and config; keys or an agent must let ssh connect without a prompt.", base.Foreground(muted))
	s.top, s.height = 6, max(1, h-12)
	s.clamp(len(s.machines), s.height)
	a.text(4, 5, 16, "NAME", base.Foreground(muted).Bold(true))
	a.text(22, 5, 26, "HOST", base.Foreground(muted).Bold(true))
	a.text(50, 5, 16, "DEFAULT PATH", base.Foreground(muted).Bold(true))
	a.text(68, 5, w-70, "SNAPSHOTS · LAST SCAN", base.Foreground(muted).Bold(true))
	now := time.Now()
	for row := 0; row < s.height && row+s.offset < len(s.machines); row++ {
		i := row + s.offset
		m := s.machines[i]
		y := s.top + row
		style := a.row(y, w, i == s.selected)
		host, path := m.Host, m.DefaultPath()
		if m.Host == "" {
			host, path = "(this machine)", "disk picker"
		} else if m.Port != 0 {
			host += ":" + strconv.Itoa(m.Port)
		}
		a.text(4, y, 17, m.Name, style.Bold(true))
		a.text(22, y, 27, host, style)
		a.text(50, y, 17, path, style)
		h := s.history[strings.ToLower(m.Name)]
		last := "never scanned"
		if len(h) > 0 {
			last = fmt.Sprintf("%d · %s · %s · %s", len(h), library.Age(h[0].ScannedAt, now), h[0].Root, Bytes(h[0].Stats.Allocated))
		}
		a.text(68, y, w-70, last, style.Foreground(muted))
	}
	if s.err != "" {
		a.text(2, h-5, w-4, s.err, base.Foreground(tcell.ColorYellow))
	} else if len(s.machines) == 1 {
		a.text(4, s.top+2, w-6, "No remote machines yet. Press n to add one, or run: orchard machine add NAME user@host", base.Foreground(muted))
	} else if m := s.selectedMachine(); m.Notes != "" {
		a.text(2, h-5, w-4, m.Notes, base.Foreground(muted))
	}
	a.text(2, h-4, w-4, a.notice, base.Foreground(tcell.ColorYellow))
	a.text(2, h-3, w-4, "n add · e edit · x remove · l open latest · o snapshots · f query this machine", base.Foreground(muted))
	a.text(2, h-2, w-4, "Enter scan · Esc back · q quit", base.Foreground(accent))
}

func machineForm(m library.Machine) *formScreen {
	port := ""
	if m.Port != 0 {
		port = strconv.Itoa(m.Port)
	}
	title := "ADD MACHINE"
	if m.Name != "" {
		title = "EDIT MACHINE · " + m.Name
	}
	f := &formScreen{
		title: title,
		fields: []formField{
			{label: "Name", value: m.Name, hint: "letters, digits, . - _"},
			{label: "SSH host", value: m.Host, hint: "user@host, or an alias from ~/.ssh/config"},
			{label: "Port", value: port, hint: "blank for ssh's default"},
			{label: "Scan path", value: strings.Join(m.Paths, ", "), hint: "default directory; comma-separate several"},
			{label: "Identity", value: m.Identity, hint: "key file; blank uses your agent and config"},
			{label: "Notes", value: m.Notes},
		},
		note: "Saved to machines.json in the library. Test it first with: ssh HOST true",
	}
	f.submit = func(a *App, v []string) string {
		next := m
		next.Name, next.Host, next.Identity, next.Notes = strings.TrimSpace(v[0]), strings.TrimSpace(v[1]), strings.TrimSpace(v[4]), strings.TrimSpace(v[5])
		next.Port = 0
		if p := strings.TrimSpace(v[2]); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n <= 0 || n > 65535 {
				return "Port must be a number from 1 to 65535."
			}
			next.Port = n
		}
		next.Paths = nil
		for _, p := range strings.Split(v[3], ",") {
			if p = strings.TrimSpace(p); p != "" {
				next.Paths = append(next.Paths, p)
			}
		}
		lib, err := a.openLibrary()
		if err != nil {
			return err.Error()
		}
		if m.Name != "" && !strings.EqualFold(m.Name, next.Name) {
			_ = lib.RemoveMachine(m.Name)
		}
		if err := lib.SaveMachine(next); err != nil {
			return err.Error()
		}
		a.notice = "Saved " + next.Name + " · Enter scans it"
		a.pop()
		return ""
	}
	return f
}

// remoteScreen follows a scan running on another machine.
type remoteScreen struct {
	machine  library.Machine
	path     string
	stage    string
	progress remote.Progress
	err      string
	started  time.Time
	cancel   context.CancelFunc
}

func (s *remoteScreen) typing() bool { return false }

// startRemote runs a scan over SSH in the background, saves the snapshot
// and opens it.
func (a *App) startRemote(ctx context.Context, m library.Machine, path string) {
	lib, err := a.openLibrary()
	if err != nil {
		a.notice = err.Error()
		return
	}
	scanCtx, cancel := context.WithCancel(ctx)
	s := &remoteScreen{machine: m, path: path, stage: "starting ssh", started: time.Now(), cancel: cancel}
	a.push(s)
	ch := a.async
	post := func(fn func(*App)) {
		select {
		case ch <- fn:
		default: // a progress update can be dropped; the next one replaces it
		}
	}
	target := remote.Target{Host: m.Host, Port: m.Port, Identity: m.Identity, Options: m.Options, Binary: m.Binary, Sudo: m.Sudo, Batch: true,
		Stage: func(stage string) { post(func(*App) { s.stage = stage }) }}
	go func() {
		snap, err := target.Scan(scanCtx, m.Name, path, func(p remote.Progress) { post(func(*App) { s.progress = p }) })
		var saved library.Entry
		if err == nil {
			saved, err = lib.Save(snap.Meta, snap.Tree)
			snap.Meta = saved.Meta
		}
		cancelled := scanCtx.Err() != nil
		cancel()
		ch <- func(a *App) {
			if a.top() != s {
				return // closed while running
			}
			if err != nil {
				if cancelled {
					a.pop()
					a.notice = "Remote scan cancelled."
					return
				}
				s.err = err.Error()
				return
			}
			a.openSnapshot(snap)
			a.notice = fmt.Sprintf("Saved snapshot %s from %s · c compares with its previous scan", saved.ID, m.Name)
		}
	}()
}

func (s *remoteScreen) key(a *App, ctx context.Context, e *tcell.EventKey) {
	if e.Key() == tcell.KeyEscape || e.Key() == tcell.KeyRune && e.Str() == "s" {
		s.cancel()
		a.pop()
		if s.err == "" {
			a.notice = "Remote scan cancelled."
		}
		if len(a.screens) == 0 && a.tree == nil {
			a.showPicker(ctx)
		}
	}
}

func (s *remoteScreen) mouse(*App, context.Context, *tcell.EventMouse) {}

func (s *remoteScreen) draw(a *App, w, h int) {
	a.text(2, 2, w-4, "REMOTE SCAN · "+s.machine.Name+" · "+s.machine.Host, base.Foreground(accent).Bold(true))
	a.text(2, 3, w-4, "‹  "+s.path, base.Bold(true))
	elapsed := time.Since(s.started).Round(time.Second)
	if s.err != "" {
		a.text(2, 6, w-4, "Scan failed", base.Foreground(tcell.ColorYellow).Bold(true))
		for i, line := range wrap(s.err, w-6) {
			if 7+i >= h-4 {
				break
			}
			a.text(4, 7+i, w-6, line, base)
		}
		a.text(2, h-2, w-4, "Esc back", base.Foreground(accent))
		return
	}
	spinner := []string{".", "o", "O", "o"}[int(elapsed.Seconds()*4)%4]
	a.text(2, 6, w-4, spinner+" "+s.stage+"…", base.Foreground(accent))
	p := s.progress
	if p.Files+p.Directories > 0 {
		a.text(4, 8, w-6, fmt.Sprintf("%d files · %d dirs · %s allocated · %d unreadable", p.Files, p.Directories, Bytes(p.Bytes), p.Errors), base)
	}
	a.text(4, 10, w-6, "Elapsed "+elapsed.String()+". The tree arrives when the remote scan finishes.", base.Foreground(muted))
	a.text(2, h-2, w-4, "Esc cancel", base.Foreground(accent))
}

func wrap(s string, width int) []string {
	var lines []string
	for _, para := range strings.Split(s, "; ") {
		for len([]rune(para)) > width && width > 10 {
			r := []rune(para)
			cut := width
			for i := width; i > width/2; i-- {
				if r[i] == ' ' {
					cut = i
					break
				}
			}
			lines = append(lines, string(r[:cut]))
			para = strings.TrimLeft(string(r[cut:]), " ")
		}
		lines = append(lines, para)
	}
	return lines
}
