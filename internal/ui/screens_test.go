package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/library"
	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/snapshot"
)

// fleetApp is an app with a scratch library holding two scans of the same
// directory, one before and one after a file grows and another appears.
func fleetApp(t *testing.T) (*App, string) {
	t.Helper()
	t.Setenv("ORCHARD_HOME", t.TempDir())
	root := t.TempDir()
	write := func(name string, size int) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lib, err := library.Open()
	if err != nil {
		t.Fatal(err)
	}
	save := func(at time.Time) {
		tree, done, err := scan.Start(context.Background(), root, scan.Options{Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		<-done
		if _, err := lib.Save(snapshot.Describe(tree, at, "test"), tree); err != nil {
			t.Fatal(err)
		}
	}
	write("logs/app.log", 1000)
	save(time.Now().Add(-time.Hour))
	write("logs/app.log", 90000)
	write("logs/new.log", 5000)
	save(time.Now())
	a := &App{screen: newTestScreen(120, 34), picker: true, async: make(chan func(*App), 16), listHeight: 10}
	return a, root
}

// settle applies background results, as the event loop would.
func settle(t *testing.T, a *App, n int) {
	t.Helper()
	for range n {
		select {
		case apply := <-a.async:
			apply(a)
		case <-time.After(5 * time.Second):
			t.Fatal("background work never finished")
		}
	}
}

func press(a *App, keys ...string) {
	for _, k := range keys {
		switch k {
		case "Enter":
			a.key(context.Background(), tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
		case "Esc":
			a.key(context.Background(), tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
		case "Down":
			a.key(context.Background(), tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
		case "Left":
			a.key(context.Background(), tcell.NewEventKey(tcell.KeyLeft, "", tcell.ModNone))
		case "Tab":
			a.key(context.Background(), tcell.NewEventKey(tcell.KeyTab, "", tcell.ModNone))
		default:
			for _, r := range k {
				a.key(context.Background(), tcell.NewEventKey(tcell.KeyRune, string(r), tcell.ModNone))
			}
		}
	}
}

func TestLibraryOpensSnapshotOffline(t *testing.T) {
	a, root := fleetApp(t)
	press(a, "o")
	lib, ok := a.top().(*libraryScreen)
	if !ok || len(lib.entries) != 2 {
		t.Fatalf("library screen: %#v", a.top())
	}
	a.draw()
	if !strings.Contains(screenText(a), "SNAPSHOT LIBRARY · 2 snapshots on 1 machine") {
		t.Fatalf("library not drawn:\n%s", screenText(a))
	}
	press(a, "Enter")
	settle(t, a, 1)
	// The scan resolves symlinks, and macOS keeps temporary directories
	// behind one (/var is /private/var).
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if a.top() != nil || a.snap == nil || a.tree.Root.Path() != root {
		t.Fatalf("snapshot not opened: top %v snap %v", a.top(), a.snap)
	}
	a.draw()
	if !strings.Contains(screenText(a), "SNAPSHOT "+a.snap.Machine) {
		t.Errorf("tree view does not say it is a snapshot:\n%s", screenText(a))
	}
	press(a, "Enter", "v")
	if !strings.Contains(a.notice, "not part of a snapshot") {
		t.Errorf("viewer should be unavailable offline, notice %q", a.notice)
	}
	press(a, "Left", "Left")
	if _, ok := a.top().(*libraryScreen); !ok {
		t.Errorf("leaving a snapshot's root should return to the library, top %#v", a.top())
	}
}

func TestLibraryComparesWithPrevious(t *testing.T) {
	a, _ := fleetApp(t)
	press(a, "o", "c")
	settle(t, a, 2)
	cmp, ok := a.top().(*compareScreen)
	if !ok {
		t.Fatalf("compare screen not opened: %#v (notice %q)", a.top(), a.notice)
	}
	if !cmp.before.ScannedAt.Before(cmp.after.ScannedAt) {
		t.Error("before and after are swapped")
	}
	a.draw()
	text := screenText(a)
	if !strings.Contains(text, "~ logs/") || !strings.Contains(text, "COMPARE") {
		t.Fatalf("compare not drawn:\n%s", text)
	}
	press(a, "Enter")
	if cmp.current.Name != "logs" || len(cmp.entries) != 2 || cmp.entries[0].Name != "app.log" {
		t.Fatalf("did not open logs/ sorted by change: %v", cmp.entries)
	}
	a.draw()
	if !strings.Contains(screenText(a), "+ new.log") {
		t.Errorf("added file not marked:\n%s", screenText(a))
	}
	press(a, "Left", "Esc")
	if _, ok := a.top().(*libraryScreen); !ok {
		t.Errorf("Esc should return to the library, top %#v", a.top())
	}
}

func TestSaveThenCompareLiveScan(t *testing.T) {
	a, root := fleetApp(t)
	if err := a.start(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	<-a.done
	a.refresh()
	press(a, "w")
	settle(t, a, 1)
	if a.savedID == "" || !strings.Contains(a.notice, "Saved snapshot") {
		t.Fatalf("save: id %q notice %q", a.savedID, a.notice)
	}
	press(a, "w")
	if !strings.Contains(a.notice, "Already saved") {
		t.Errorf("second save: %q", a.notice)
	}
	press(a, "c")
	settle(t, a, 1)
	cmp, ok := a.top().(*compareScreen)
	if !ok {
		t.Fatalf("no comparison: %q", a.notice)
	}
	if cmp.root.Status.String() != "same" {
		t.Errorf("nothing changed since the last save, got %v", cmp.root.Status)
	}
}

func TestQueryScreenFindsAndReveals(t *testing.T) {
	a, _ := fleetApp(t)
	press(a, "F")
	q := a.top().(*queryScreen)
	settle(t, a, 1) // snapshots load
	press(a, "ext:log")
	q.next = time.Time{}
	if !q.tick(a) {
		t.Fatal("query did not start")
	}
	settle(t, a, 1)
	if q.result.Matches != 2 || q.result.Hits[0].Node.Name != "app.log" {
		t.Fatalf("query result: %+v", q.result)
	}
	a.draw()
	if !strings.Contains(screenText(a), "2 matches") {
		t.Errorf("results not drawn:\n%s", screenText(a))
	}
	press(a, "q") // text, not quit
	if q.input != "ext:logq" {
		t.Errorf("q should type into the query, got %q", q.input)
	}
	q.input = "ext:log"
	press(a, "Enter")
	if a.snap == nil || a.current.Name != "logs" || a.entries[a.selected].Name != "app.log" {
		t.Fatalf("reveal: snap %v current %v", a.snap, a.current.Name)
	}
}

func TestMachineFormSavesProfile(t *testing.T) {
	a, _ := fleetApp(t)
	press(a, "m", "n")
	if _, ok := a.top().(*formScreen); !ok {
		t.Fatalf("no form: %#v", a.top())
	}
	press(a, "web1", "Tab", "ops@web1", "Tab", "x", "Enter")
	if f := a.top().(*formScreen); !strings.Contains(f.err, "Port") {
		t.Fatalf("bad port accepted: %q", f.err)
	}
	f := a.top().(*formScreen)
	f.fields[2].value = "2222"
	f.fields[3].value = "/srv, /home"
	press(a, "Enter")
	ms, ok := a.top().(*machinesScreen)
	if !ok {
		t.Fatalf("form did not close: %#v", a.top())
	}
	m, err := a.lib.Machine("web1")
	if err != nil || m.Port != 2222 || m.Host != "ops@web1" || len(m.Paths) != 2 || m.DefaultPath() != "/srv" {
		t.Fatalf("profile: %+v %v", m, err)
	}
	if len(ms.machines) != 2 {
		t.Errorf("machines screen did not reload: %v", ms.machines)
	}
	a.draw()
	if !strings.Contains(screenText(a), "ops@web1:2222") {
		t.Errorf("profile not listed:\n%s", screenText(a))
	}
}
