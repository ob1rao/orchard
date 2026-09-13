package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "data"), make([]byte, 8193), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "nested", "data"), filepath.Join(root, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "cycle")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(root, "sparse"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return root
}
func wait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("scan deadlocked")
	}
}
func TestAccountingAndLinks(t *testing.T) {
	root := fixture(t)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	var allocated, apparent uint64
	seen := map[identity]bool{}
	err = filepath.Walk(root, func(_ string, i os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		s := i.Sys().(*syscall.Stat_t)
		id := identity{uint64(s.Dev), uint64(s.Ino)}
		if seen[id] {
			return nil
		}
		seen[id] = true
		allocated += uint64(s.Blocks) * 512
		apparent += uint64(i.Size())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			tree, done, err := Start(context.Background(), root, Options{workers})
			if err != nil {
				t.Fatal(err)
			}
			wait(t, done)
			v := tree.Snapshot(tree.Root, false)
			if v.Size != allocated || v.Stats.Apparent != apparent {
				t.Fatalf("sizes got %d/%d want %d/%d", v.Size, v.Stats.Apparent, allocated, apparent)
			}
			if v.Stats.Hardlinks != 1 || v.Stats.Files != 4 || v.Stats.Directories != 2 || v.Stats.Errors != 0 || !v.Stats.Done {
				t.Fatalf("stats: %+v", v.Stats)
			}
			if v.Stats.Apparent <= v.Stats.Allocated {
				t.Fatal("sparse file not distinguished")
			}
			for _, e := range v.Entries {
				if e.Name == "cycle" && e.Dir {
					t.Fatal("followed symlink")
				}
				if e.Node.Path() != filepath.Join(canonicalRoot, e.Name) {
					t.Fatal("bad path")
				}
			}
		})
	}
}
func TestLargeDirectoryAndConcurrentSnapshots(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 1300; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d", i)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tree, done, err := Start(context.Background(), root, Options{4})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = tree.Snapshot(tree.Root, false)
			}
		}
	}()
	wait(t, done)
	wg.Wait()
	v := tree.Snapshot(tree.Root, false)
	if len(v.Entries) != 1300 || v.Stats.Files != 1300 {
		t.Fatalf("lost batch: %+v", v.Stats)
	}
}
func TestManyDirectoriesSingleWorker(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 1100; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprint(i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	tree, done, err := Start(context.Background(), root, Options{1})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, done)
	if tree.Snapshot(tree.Root, false).Stats.Directories != 1101 {
		t.Fatal("lost queued directory")
	}
}
func TestCancellationAndInvalidRoot(t *testing.T) {
	root := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tree, done, err := Start(ctx, root, Options{8})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, done)
	if !tree.Snapshot(tree.Root, false).Stats.Cancelled {
		t.Fatal("missing cancelled state")
	}
	if _, _, err = Start(context.Background(), filepath.Join(root, "missing"), Options{}); err == nil {
		t.Fatal("accepted missing path")
	}
	if _, _, err = Start(context.Background(), filepath.Join(root, "sparse"), Options{}); err == nil {
		t.Fatal("accepted file as root")
	}
}
func TestPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	root := t.TempDir()
	p := filepath.Join(root, "denied")
	if err := os.Mkdir(p, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(p, 0700)
	tree, done, err := Start(context.Background(), root, Options{2})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, done)
	if tree.Snapshot(tree.Root, false).Stats.Errors != 1 {
		t.Fatal("unreadable directory not reported")
	}
}
func BenchmarkScan(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 10000; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprint(i)), []byte("x"), 0600); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, done, err := Start(context.Background(), root, Options{4})
		if err != nil {
			b.Fatal(err)
		}
		<-done
		if tree.Snapshot(tree.Root, false).Stats.Files != 10000 {
			b.Fatal("missing files")
		}
	}
}
