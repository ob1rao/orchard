package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/snapshot"
)

func tree(t *testing.T) *scan.Tree {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	tr, done, err := scan.Start(context.Background(), root, scan.Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	return tr
}

func save(t *testing.T, l *Library, tr *scan.Tree, machine string, at time.Time) Entry {
	t.Helper()
	meta := snapshot.Describe(tr, at, "test")
	meta.Machine = machine
	e, err := l.Save(meta, tr)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSaveListResolve(t *testing.T) {
	l := &Library{Dir: t.TempDir()}
	tr := tree(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := save(t, l, tr, "web1", base)
	mid := save(t, l, tr, "web1", base.Add(time.Hour))
	newest := save(t, l, tr, "web1", base.Add(2*time.Hour))
	other := save(t, l, tr, "db/1", base.Add(30*time.Minute))

	all, err := l.List()
	if err != nil || len(all) != 4 {
		t.Fatalf("list: %v %v", all, err)
	}
	if all[0].ID != newest.ID || all[3].ID != old.ID {
		t.Fatalf("not newest first: %v", all)
	}
	if filepath.Base(filepath.Dir(other.Path)) != "db_1" {
		t.Fatalf("machine name not made safe for a path: %s", other.Path)
	}
	for ref, want := range map[string]string{
		"web1": newest.ID, "WEB1~1": mid.ID, "web1~2": old.ID, "db/1": other.ID,
		old.ID[:6]: old.ID, newest.Path: newest.ID,
	} {
		got, err := l.Resolve(ref)
		if err != nil || got.ID != want {
			t.Errorf("Resolve(%q) = %s, %v; want %s", ref, got.ID, err, want)
		}
	}
	if _, err := l.Resolve("web1~3"); err == nil {
		t.Error("resolved past the oldest snapshot")
	}
	if _, err := l.Resolve("zzzz"); err == nil {
		t.Error("resolved a reference that names nothing")
	}
	if latest := Latest(all); len(latest) != 2 {
		t.Errorf("latest per machine and root: %v", latest)
	}
	if err := l.Delete(mid); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.Resolve("web1~1"); got.ID != old.ID {
		t.Errorf("history did not close up after delete: %s", got.ID)
	}
	if err := l.Delete(Entry{Path: "/etc/passwd"}); err == nil {
		t.Error("deleted a file outside the library")
	}
}

func TestArchiveRoundTrip(t *testing.T) {
	src := &Library{Dir: t.TempDir()}
	tr := tree(t)
	a := save(t, src, tr, "web1", time.Now().Add(-time.Hour))
	b := save(t, src, tr, "web2", time.Now())
	archive := filepath.Join(t.TempDir(), "old.tar")
	if err := Export(archive, []Entry{a, b}); err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest(archive)
	if err != nil || len(m.Snapshots) != 2 {
		t.Fatalf("manifest: %+v %v", m, err)
	}
	dst := &Library{Dir: t.TempDir()}
	res, err := dst.ImportArchive(archive)
	if err != nil || len(res.Added) != 2 {
		t.Fatalf("import: %+v %v", res, err)
	}
	again, err := dst.ImportArchive(archive)
	if err != nil || len(again.Added) != 0 || len(again.Skipped) != 2 {
		t.Fatalf("second import should skip everything: %+v %v", again, err)
	}
	got, err := dst.Resolve("web2")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Load(got)
	if err != nil || snap.Tree.Root.Apparent != tr.Root.Apparent {
		t.Fatalf("imported snapshot unreadable: %v", err)
	}
	if _, err := dst.ImportArchive(got.Path); err == nil {
		t.Error("imported a snapshot file as if it were an archive")
	}
}

func TestMachines(t *testing.T) {
	l := &Library{Dir: t.TempDir()}
	if err := l.SaveMachine(Machine{Name: "web1", Host: "ops@web1", Paths: []string{"/srv"}}); err != nil {
		t.Fatal(err)
	}
	if err := l.SaveMachine(Machine{Name: "a-db", Host: "db"}); err != nil {
		t.Fatal(err)
	}
	if err := l.SaveMachine(Machine{Name: "WEB1", Host: "ops@web1.internal"}); err != nil {
		t.Fatal(err)
	}
	ms, err := l.Machines()
	if err != nil || len(ms) != 2 || ms[0].Name != "a-db" {
		t.Fatalf("machines: %+v %v", ms, err)
	}
	m, err := l.Machine("web1")
	if err != nil || m.Host != "ops@web1.internal" || m.Added.IsZero() || m.DefaultPath() != "/" {
		t.Fatalf("replace by name: %+v %v", m, err)
	}
	for _, bad := range []Machine{{Name: "", Host: "h"}, {Name: "a b", Host: "h"}, {Name: "ok"}} {
		if err := l.SaveMachine(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if err := l.RemoveMachine("a-db"); err != nil {
		t.Fatal(err)
	}
	if err := l.RemoveMachine("a-db"); err == nil {
		t.Error("removed a machine twice")
	}
}
