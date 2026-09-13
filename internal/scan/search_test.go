package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecursiveSearch(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"reports/Annual.TXT", "reports/nested/data.csv", ".cache/visible.txt", "reports/.secret.txt", "elsewhere/data.csv"} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("payload"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tree, done, err := Start(context.Background(), root, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, done)
	cases := []struct {
		query Query
		want  int
	}{
		{Query{Text: "txt"}, 3},
		{Query{Text: `(?i)\.txt$`, Regex: true}, 3},
		{Query{Text: `\.txt$`, Regex: true}, 2},
		{Query{Text: "txt", HideHidden: true}, 1},
		{Query{Text: "nested/data", FullPath: true}, 1},
		{Query{Text: "nested/data"}, 0},
		{Query{Text: "data.csv"}, 2},
		{Query{Text: ""}, 0},
	}
	for _, tt := range cases {
		got, err := tree.Search(context.Background(), tt.query)
		if err != nil {
			t.Fatal(err)
		}
		if got.Matches != tt.want || len(got.Entries) != tt.want {
			t.Fatalf("%+v: got %+v", tt.query, got)
		}
	}
	if _, err := tree.Search(context.Background(), Query{Text: "[", Regex: true}); err == nil {
		t.Fatal("invalid regex accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tree.Search(ctx, Query{Text: "data"}); err != context.Canceled {
		t.Fatalf("cancellation ignored: %v", err)
	}
	// Searching must use the index, not reread the files from disk.
	if err := os.RemoveAll(filepath.Join(root, "reports")); err != nil {
		t.Fatal(err)
	}
	got, err := tree.Search(context.Background(), Query{Text: "Annual"})
	if err != nil || got.Matches != 1 {
		t.Fatal("search did not use in-memory index")
	}
}
func TestSearchDuringScan(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 1500; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%04d", i)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tree, done, err := Start(context.Background(), root, Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err := tree.Search(context.Background(), Query{Text: "file"})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
			got, err := tree.Search(context.Background(), Query{Text: "file"})
			if err != nil || got.Matches != 1500 {
				t.Fatalf("final results: %d %v", got.Matches, err)
			}
			return
		default:
		}
	}
}
func TestSearchLimitAndHiddenAncestors(t *testing.T) {
	root := &Node{Name: "/root/.explicit", Dir: true}
	tree := &Tree{Root: root}
	for i := 0; i < SearchLimit+2; i++ {
		tree.index = append(tree.index, &Node{Name: fmt.Sprint(i), Parent: root})
	}
	got, err := tree.Search(context.Background(), Query{Text: ".", Regex: true, HideHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Matches != SearchLimit+2 || len(got.Entries) != SearchLimit {
		t.Fatalf("limit/count incorrect: %d/%d", got.Matches, len(got.Entries))
	}
}
func TestModifiedTimestamp(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dated")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	// Move mtime forward so the filesystem can preserve an earlier birth time.
	want := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, want, want); err != nil {
		t.Fatal(err)
	}
	tree, done, err := Start(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, done)
	node := tree.Snapshot(tree.Root, false).Entries[0].Node
	if node.Modified != want.Unix() {
		t.Fatalf("mtime got %d want %d", node.Modified, want.Unix())
	}
	if node.HasCreated && node.Created == node.Modified {
		t.Fatal("birth time was substituted with mtime")
	}
}
func BenchmarkSearch(b *testing.B) {
	root := &Node{Name: "/root", Dir: true}
	tree := &Tree{Root: root}
	for i := 0; i < 100000; i++ {
		tree.index = append(tree.index, &Node{Name: fmt.Sprintf("file-%06d.log", i), Parent: root})
	}
	for _, regex := range []bool{false, true} {
		b.Run(fmt.Sprint(regex), func(b *testing.B) {
			q := Query{Text: "999", Regex: regex}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, err := tree.Search(context.Background(), q)
				if err != nil || len(r.Entries) == 0 {
					b.Fatal("no matches")
				}
			}
		})
	}
}
func TestSearchFilenameControls(t *testing.T) {
	root := &Node{Name: "/", Dir: true}
	tree := &Tree{Root: root, index: []*Node{{Name: "a\nb.txt", Parent: root}}}
	r, err := tree.Search(context.Background(), Query{Text: "txt"})
	if err != nil || len(r.Entries) != 1 || !strings.Contains(r.Entries[0].Name, "\n") {
		t.Fatal("search altered stored filename")
	}
}
