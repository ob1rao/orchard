package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
)

func TestFindShortcuts(t *testing.T) {
	a := testApp(t)
	for _, key := range []*tcell.EventKey{tcell.NewEventKey(tcell.KeyRune, "f", tcell.ModNone), tcell.NewEventKey(tcell.KeyCtrlF, "", tcell.ModNone)} {
		a.key(context.Background(), key)
		if a.find == nil {
			t.Fatal("find shortcut failed")
		}
		a.closeFind()
	}
	a.searching = true
	a.key(context.Background(), tcell.NewEventKey(tcell.KeyRune, "f", tcell.ModNone))
	if a.find != nil || a.filter != "f" {
		t.Fatal("f should remain text while filtering")
	}
}
func TestViewerNavigationAndTail(t *testing.T) {
	root := t.TempDir()
	var text strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&text, "line %03d\n", i)
	}
	path := filepath.Join(root, "example.log")
	if err := os.WriteFile(path, []byte(text.String()), 0600); err != nil {
		t.Fatal(err)
	}
	a := appForPath(t, root)
	defer a.closeViewer()
	key := func(k tcell.Key, s string) { a.key(context.Background(), tcell.NewEventKey(k, s, tcell.ModNone)) }
	key(tcell.KeyRune, "v")
	if a.viewer == nil || a.viewer.page.Lines[0].Text != "line 000" {
		t.Fatal("v did not start at beginning")
	}
	a.draw()
	key(tcell.KeyDown, "")
	if a.viewer.page.Lines[0].Text != "line 001" {
		t.Fatal("down did not scroll")
	}
	key(tcell.KeyPgDn, "")
	if a.viewer.page.Offset <= 9 {
		t.Fatal("page down failed")
	}
	key(tcell.KeyHome, "")
	if a.viewer.page.Offset != 0 {
		t.Fatal("home failed")
	}
	key(tcell.KeyEscape, "")
	if a.viewer != nil || a.entries[a.selected].Name != "example.log" {
		t.Fatal("viewer did not return to selection")
	}
	key(tcell.KeyRune, "t")
	if a.viewer == nil || !a.viewer.page.EOF {
		t.Fatal("t did not open final page")
	}
	lines := a.viewer.page.Lines
	if lines[len(lines)-1].Text != "line 099" {
		t.Fatal("last line missing")
	}
	key(tcell.KeyUp, "")
	if a.viewer.page.EOF {
		t.Fatal("tail cannot scroll backwards")
	}
	key(tcell.KeyRune, "q")
	if a.viewer != nil {
		t.Fatal("q did not close viewer")
	}
	key(tcell.KeyRune, "v")
	key(tcell.KeyRune, "f")
	if a.viewer != nil || a.find == nil {
		t.Fatal("find from viewer failed")
	}
	a.closeFind()
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != text.String() {
		t.Fatal("viewer modified file")
	}
}
func TestViewerControlsAreSafeText(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config"), []byte("a\t界e\u0301\x1b[31mred\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := appForPath(t, root)
	a.openViewer(false)
	defer a.closeViewer()
	a.draw()
	row := string(a.screen.(*testScreen).rows[5])
	if strings.ContainsRune(row, '\x1b') || !strings.Contains(row, "red") {
		t.Fatalf("unsafe or missing content: %q", row)
	}
	a.viewerKey(tcell.NewEventKey(tcell.KeyRight, "", tcell.ModNone))
	a.draw()
	if a.viewer.column != 8 {
		t.Fatal("horizontal scroll failed")
	}
}
