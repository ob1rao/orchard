package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/treemap"
	"github.com/ob1rao/orchard/internal/volumes"
)

func testApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "folder", "data"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	tree, done, err := scan.Start(context.Background(), root, scan.Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	a := &App{tree: tree, current: tree.Root, listHeight: 10}
	a.refresh()
	return a
}
func TestKeyboardNavigation(t *testing.T) {
	a := testApp(t)
	key := func(k tcell.Key, s string) { a.key(context.Background(), tcell.NewEventKey(k, s, tcell.ModNone)) }
	key(tcell.KeyEnter, "")
	if a.current == a.tree.Root || len(a.entries) != 1 {
		t.Fatal("did not enter folder")
	}
	key(tcell.KeyLeft, "")
	if a.current != a.tree.Root {
		t.Fatal("did not go back")
	}
	key(tcell.KeyRune, "/")
	key(tcell.KeyRune, "missing")
	if len(a.entries) != 0 {
		t.Fatal("filter failed")
	}
	key(tcell.KeyEscape, "")
	if len(a.entries) != 1 {
		t.Fatal("filter clear failed")
	}
	key(tcell.KeyRune, "a")
	if !a.apparent {
		t.Fatal("metric not switched")
	}
	if !a.key(context.Background(), tcell.NewEventKey(tcell.KeyRune, "q", tcell.ModNone)) {
		t.Fatal("quit not handled")
	}
}
func TestMouseNavigation(t *testing.T) {
	a := testApp(t)
	a.listWidth = 30
	a.listTop = 6
	a.tiles = []treemap.Tile{{Rect: treemap.Rect{X: 31, Y: 6, W: 20, H: 10}, Index: 0}}
	click := func() {
		a.mouse(context.Background(), tcell.NewEventMouse(35, 8, tcell.Button1, tcell.ModNone))
		a.mouse(context.Background(), tcell.NewEventMouse(35, 8, tcell.ButtonNone, tcell.ModNone))
	}
	click()
	if a.current != a.tree.Root {
		t.Fatal("single click should select")
	}
	click()
	if a.current == a.tree.Root {
		t.Fatal("double click should enter")
	}
	a.mouse(context.Background(), tcell.NewEventMouse(35, 8, tcell.Button2, tcell.ModNone))
	if a.current != a.tree.Root {
		t.Fatal("right click should go back")
	}
}
func TestControlCharacters(t *testing.T) {
	if got := clean("a\x1b[31m\n\tb"); strings.ContainsAny(got, "\x1b\n\t") {
		t.Fatal("control characters escaped sanitization")
	}
}
func TestBytes(t *testing.T) {
	if Bytes(1000) != "1.0 KB" || Bytes(0) != "0 B" || Bytes(1_000_000_000_000) != "1.0 TB" {
		t.Fatal("invalid byte formatting")
	}
}

// A cell buffer lets tests inspect the sidebar after scrolling and filtering.
type testScreen struct {
	tcell.Screen
	width, height int
	rows          [][]rune
}

func newTestScreen(w, h int) *testScreen { s := &testScreen{width: w, height: h}; s.Clear(); return s }
func (s *testScreen) Size() (int, int)   { return s.width, s.height }
func (s *testScreen) Clear() {
	s.rows = make([][]rune, s.height)
	for y := range s.rows {
		s.rows[y] = []rune(strings.Repeat(" ", s.width))
	}
}
func (s *testScreen) Show() {}
func (s *testScreen) PutStrStyled(x, y int, text string, _ tcell.Style) {
	if y < 0 || y >= s.height {
		return
	}
	for _, r := range text {
		if x >= 0 && x < s.width {
			s.rows[y][x] = r
		}
		x++
	}
}
func (s *testScreen) FillArea(x, y, w, h int, r rune, _ tcell.Style) {
	for j := max(0, y); j < min(y+h, s.height); j++ {
		for i := max(0, x); i < min(x+w, s.width); i++ {
			s.rows[j][i] = r
		}
	}
}
func appForPath(t *testing.T, path string) *App {
	t.Helper()
	tree, done, err := scan.Start(context.Background(), path, scan.Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	a := &App{tree: tree, current: tree.Root, apparent: true, screen: newTestScreen(80, 24)}
	a.refresh()
	return a
}
func TestSidebarAllFilesAndSizes(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("file_%02d", i)
		f, err := os.Create(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		sizes := []int64{0, 500, 1500, 2_500_000, 3_500_000_000}
		if err = f.Truncate(sizes[i%len(sizes)]); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	a := appForPath(t, root)
	if len(a.entries) != 30 {
		t.Fatal("sidebar omitted individual files")
	}
	want := map[uint64]string{0: "0 B", 500: "500 B", 1500: "1.5 KB", 2_500_000: "2.5 MB", 3_500_000_000: "3.5 GB"}
	for i, e := range a.entries {
		a.selected = i
		a.draw()
		row := string(a.screen.(*testScreen).rows[a.listTop+i-a.offset][:a.listWidth])
		if !strings.Contains(row, e.Name) || !strings.Contains(row, want[e.Size]) {
			t.Fatalf("missing file or size after scrolling: %q for %s (%d)", row, e.Name, e.Size)
		}
	}
}
func TestHiddenToggleAndNavigation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"visible", "zero", "report.txt", ".secret"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"folder", ".cache"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "folder", ".nested"), []byte("hidden"), 0600); err != nil {
		t.Fatal(err)
	}
	a := appForPath(t, root)
	if a.hideHidden || len(a.entries) != 6 {
		t.Fatal("hidden files must be visible by default")
	}
	total := a.view.Size
	a.key(context.Background(), tcell.NewEventKey(tcell.KeyRune, ".", tcell.ModNone))
	if !a.hideHidden || len(a.entries) != 4 || a.view.Size != total {
		t.Fatal("toggle should hide dot entries without changing scan totals")
	}
	var folder *scan.Node
	for _, e := range a.entries {
		if strings.HasPrefix(e.Name, ".") {
			t.Fatal("hidden entry is visible")
		}
		if e.Name == "folder" {
			folder = e.Node
		}
	}
	a.filter = "secret"
	a.refresh()
	if len(a.entries) != 0 {
		t.Fatal("search bypasses hidden toggle")
	}
	a.key(context.Background(), tcell.NewEventKey(tcell.KeyRune, "H", tcell.ModNone))
	if len(a.entries) != 1 || a.entries[0].Name != ".secret" {
		t.Fatal("toggle failed with active filter")
	}
	a.filter = ""
	a.refresh()
	a.key(context.Background(), tcell.NewEventKey(tcell.KeyRune, ".", tcell.ModNone))
	a.enter(folder)
	if len(a.entries) != 0 || !a.hideHidden {
		t.Fatal("hidden preference lost on navigation")
	}
	a.draw()
	a.key(context.Background(), tcell.NewEventKey(tcell.KeyRune, ".", tcell.ModNone))
	if len(a.entries) != 1 {
		t.Fatal("could not reveal hidden-only directory")
	}
	a.back(context.Background())
	if len(a.entries) != 6 {
		t.Fatal("toggle did not persist when going back")
	}
}

// The scan root used to be a dead end: every gesture meaning "up" did nothing
// there, so the opening screen was unreachable except through an undocumented
// key. Each of them must now land back on the volume picker.
func TestUpFromScanRootReturnsToThePicker(t *testing.T) {
	press := func(key tcell.Key, s string) func(*App) {
		return func(a *App) { a.key(context.Background(), tcell.NewEventKey(key, s, tcell.ModNone)) }
	}
	gestures := map[string]func(*App){
		"left":      press(tcell.KeyLeft, ""),
		"backspace": press(tcell.KeyBackspace2, ""),
		"escape":    press(tcell.KeyEscape, ""),
		"h":         press(tcell.KeyRune, "h"),
		"d":         press(tcell.KeyRune, "d"),
		"right click": func(a *App) {
			a.mouse(context.Background(), tcell.NewEventMouse(10, 10, tcell.Button2, tcell.ModNone))
		},
		"path bar": func(a *App) {
			a.mouse(context.Background(), tcell.NewEventMouse(10, 2, tcell.Button1, tcell.ModNone))
			a.mouse(context.Background(), tcell.NewEventMouse(10, 2, tcell.ButtonNone, tcell.ModNone))
		},
	}
	for name, gesture := range gestures {
		a := testApp(t)
		a.screen = newTestScreen(100, 30)
		if a.picker {
			t.Fatalf("%s: expected to start in the tree view", name)
		}
		gesture(a)
		if !a.picker {
			t.Fatalf("%s at the scan root did not return to the picker", name)
		}
	}
	// One level down, the same gesture still means "up one directory".
	a := testApp(t)
	a.screen = newTestScreen(100, 30)
	a.enter(a.entries[0].Node)
	if a.current == a.tree.Root {
		t.Fatal("could not descend to set up the test")
	}
	a.back(context.Background())
	if a.picker || a.current != a.tree.Root {
		t.Fatal("back from a subdirectory should reach the root, not the picker")
	}
}

// Every screen introduces the app the same way. The wordmark and tagline used
// to be spelled four different ways across the picker, the header and the
// resize notice, which is how they drifted apart in the first place.
func TestBrandingIsConsistentOnEveryScreen(t *testing.T) {
	tree := func() *App {
		a := testApp(t)
		a.screen = newTestScreen(100, 30)
		return a
	}
	screens := map[string]func() *App{
		"picker": func() *App { return &App{screen: newTestScreen(100, 30), picker: true} },
		"narrow picker": func() *App {
			return &App{screen: newTestScreen(60, 18), picker: true}
		},
		"tree": tree,
		"help over the tree": func() *App {
			a := tree()
			a.help = true
			return a
		},
		"help over the picker": func() *App {
			return &App{screen: newTestScreen(100, 30), picker: true, help: true}
		},
		"mount form": func() *App {
			a := &App{screen: newTestScreen(100, 30), picker: true}
			a.mount = &mountForm{volume: volumes.Volume{Device: "/dev/sdb1", Label: "BACKUP"}}
			return a
		},
		"find": func() *App {
			a := tree()
			a.openFind()
			return a
		},
		"viewer": func() *App {
			a := tree()
			a.enter(a.entries[0].Node)
			a.openViewer(false)
			return a
		},
	}
	for name, build := range screens {
		a := build()
		a.draw()
		text := screenText(a)
		if !strings.Contains(text, brandTagline) {
			t.Errorf("%s: no tagline %q on screen:\n%s", name, brandTagline, text)
		}
		// The splash letterspaces the wordmark; every other screen prints it
		// plainly. One or the other, never a third spelling.
		if !strings.Contains(text, brandLine()) && !strings.Contains(text, spacedBrand()) {
			t.Errorf("%s: no wordmark on screen:\n%s", name, text)
		}
	}
	// Even the resize notice is branded, and with the same name.
	a := &App{screen: newTestScreen(40, 12), picker: true}
	a.draw()
	if !strings.Contains(screenText(a), brandName) {
		t.Errorf("resize notice is unbranded:\n%s", screenText(a))
	}
}
