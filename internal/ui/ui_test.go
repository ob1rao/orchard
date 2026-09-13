package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/diskmap/internal/scan"
	"github.com/ob1rao/diskmap/internal/treemap"
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
	if Bytes(1024) != "1.0 KiB" || Bytes(0) != "0 B" || Bytes(1<<40) != "1.0 TiB" {
		t.Fatal("invalid byte formatting")
	}
}
