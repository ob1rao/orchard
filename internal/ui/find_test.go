package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/scan"
)

func completeFind(t *testing.T, a *App) {
	t.Helper()
	a.find.next = time.Time{}
	if !a.pollFind() {
		t.Fatal("search did not start")
	}
	select {
	case update := <-a.findResults:
		a.acceptFind(update)
	case <-time.After(5 * time.Second):
		t.Fatal("search hung")
	}
}
func TestFindNavigationAndRegex(t *testing.T) {
	a := testApp(t)
	a.screen = newTestScreen(120, 32)
	a.key(context.Background(), tcell.NewEventKey(tcell.KeyCtrlF, "", tcell.ModNone))
	if a.find == nil {
		t.Fatal("Ctrl-F did not open search")
	}
	defer a.closeFind()
	a.findKey(tcell.NewEventKey(tcell.KeyRune, "data", tcell.ModNone))
	completeFind(t, a)
	if len(a.find.entries) != 1 {
		t.Fatal("nested file not found")
	}
	a.draw()
	screen := a.screen.(*testScreen)
	all := ""
	for _, row := range screen.rows {
		all += string(row) + "\n"
	}
	if !strings.Contains(all, "MODIFIED (local)") || !strings.Contains(all, "Created:") {
		t.Fatal("search timestamps missing")
	}
	a.findKey(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if a.find != nil || a.current.Name != "folder" || a.entries[a.selected].Name != "data" {
		t.Fatal("match not revealed and selected")
	}
	a.openFind()
	a.findKey(tcell.NewEventKey(tcell.KeyTab, "", tcell.ModNone))
	a.findKey(tcell.NewEventKey(tcell.KeyRune, "[", tcell.ModNone))
	completeFind(t, a)
	if a.find.err == "" {
		t.Fatal("invalid regex not reported")
	}
	a.findKey(tcell.NewEventKey(tcell.KeyCtrlU, "", tcell.ModNone))
	a.findKey(tcell.NewEventKey(tcell.KeyRune, `folder/data$`, tcell.ModNone))
	a.findKey(tcell.NewEventKey(tcell.KeyF3, "", tcell.ModNone))
	completeFind(t, a)
	if len(a.find.entries) != 1 {
		t.Fatal("full-path regex failed")
	}
	a.findKey(tcell.NewEventKey(tcell.KeyF4, "", tcell.ModNone))
	if !a.hideHidden {
		t.Fatal("hidden toggle failed")
	}
	a.findKey(tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModNone))
	if a.find != nil {
		t.Fatal("escape did not close search")
	}
}
func TestFindIgnoresStaleUpdates(t *testing.T) {
	a := testApp(t)
	a.openFind()
	defer a.closeFind()
	serial := a.findSerial
	a.find.query = "new"
	a.queueFind()
	a.acceptFind(findUpdate{serial: serial, result: scan.SearchResult{Entries: []scan.Entry{{Name: "stale"}}, Matches: 1}})
	if len(a.find.entries) != 0 {
		t.Fatal("stale result replaced new query")
	}
}
func TestFindMouseReveal(t *testing.T) {
	a := testApp(t)
	a.screen = newTestScreen(120, 32)
	a.openFind()
	a.find.query = "data"
	a.queueFind()
	completeFind(t, a)
	a.draw()
	for i := 0; i < 2; i++ {
		a.mouse(context.Background(), tcell.NewEventMouse(5, 7, tcell.Button1, tcell.ModNone))
		a.mouse(context.Background(), tcell.NewEventMouse(5, 7, tcell.ButtonNone, tcell.ModNone))
	}
	if a.find != nil || a.current.Name != "folder" {
		t.Fatal("double-click did not reveal match")
	}
}
func TestUnavailableCreationDate(t *testing.T) {
	a := testApp(t)
	a.screen = newTestScreen(80, 24)
	n := &scan.Node{Modified: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC).Unix()}
	a.drawDates(n, 80, 24)
	if got := string(a.screen.(*testScreen).rows[19]); !strings.Contains(got, "Created:  unavailable") {
		t.Fatalf("unknown creation timestamp misrepresented: %q", got)
	}
	if createdDate(n) != "—" {
		t.Fatal("date column should be unknown")
	}
	n.HasCreated = true
	n.Created = 0
	if createdDate(n) == "—" {
		t.Fatal("known Unix epoch birth time lost")
	}
}

func TestWideDirectoryDates(t *testing.T) {
	a := testApp(t)
	a.screen = newTestScreen(160, 32)
	a.draw()
	screen := a.screen.(*testScreen)
	header := string(screen.rows[5][:a.listWidth])
	if !strings.Contains(header, "MODIFIED") || !strings.Contains(header, "CREATED") {
		t.Fatalf("missing date columns: %q", header)
	}
	row := string(screen.rows[a.listTop][:a.listWidth])
	n := a.entries[0].Node
	if !strings.Contains(row, date(n.Modified)) || !strings.Contains(row, createdDate(n)) {
		t.Fatalf("missing dates: %q", row)
	}
}
