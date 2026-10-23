package snapshot

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/volumes"
)

func scanFixture(t *testing.T) *scan.Tree {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"a/b", "c", "unicodé dir"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, size := range map[string]int{"a/b/f1": 5000, "a/f2": 10, "c/f3": 70000, "unicodé dir/ü": 1, "top": 3} {
		if err := os.WriteFile(filepath.Join(root, name), bytes.Repeat([]byte("x"), size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tree, done, err := scan.Start(context.Background(), root, scan.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	<-done
	return tree
}

func equal(t *testing.T, a, b *scan.Node, path string) {
	t.Helper()
	if a.Name != b.Name || a.Dir != b.Dir || a.Allocated != b.Allocated || a.Apparent != b.Apparent ||
		a.Modified != b.Modified || a.HasCreated != b.HasCreated || a.Created != b.Created || len(a.Children) != len(b.Children) {
		t.Fatalf("%s differs: %+v vs %+v", path, *a, *b)
	}
	for i := range a.Children {
		if b.Children[i].Parent != b {
			t.Fatalf("%s/%s has the wrong parent", path, b.Children[i].Name)
		}
		equal(t, a.Children[i], b.Children[i], path+"/"+a.Children[i].Name)
	}
}

func TestRoundTrip(t *testing.T) {
	tree := scanFixture(t)
	meta := Describe(tree, time.Unix(1700000000, 0), "v-test")
	meta.Label = "before upgrade"
	meta.Space = &volumes.Space{Total: 100, Free: 40, Available: 30}
	var buf bytes.Buffer
	if err := Write(&buf, meta, tree); err != nil {
		t.Fatal(err)
	}
	got, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	equal(t, tree.Root, got.Tree.Root, "")
	if got.Meta.ID != meta.ID || got.Meta.Label != "before upgrade" || got.Meta.Space.Free != 40 || got.Meta.Root != tree.Root.Path() {
		t.Fatalf("meta lost in transit: %+v", got.Meta)
	}
	if !got.Meta.ScannedAt.Equal(time.Unix(1700000000, 0)) || got.Meta.Format != Format {
		t.Fatalf("time or format wrong: %+v", got.Meta)
	}
	// A restored tree searches and snapshots like a live one.
	res, err := got.Tree.Search(context.Background(), scan.Query{Text: "f3"})
	if err != nil || res.Matches != 1 {
		t.Fatalf("search over a restored tree: %+v %v", res, err)
	}
	if v := got.Tree.Snapshot(got.Tree.Root, false); !v.Stats.Done || v.Stats.Files != 5 {
		t.Fatalf("restored stats: %+v", v.Stats)
	}
	header, err := ReadMeta(bytes.NewReader(buf.Bytes()))
	if err != nil || header.ID != meta.ID {
		t.Fatalf("header alone: %+v %v", header, err)
	}
}

func TestRejectsDamage(t *testing.T) {
	tree := scanFixture(t)
	var buf bytes.Buffer
	if err := Write(&buf, Describe(tree, time.Now(), "v"), tree); err != nil {
		t.Fatal(err)
	}
	good := buf.Bytes()
	cases := map[string][]byte{
		"empty":     nil,
		"not ours":  []byte("PK\x03\x04 some zip file"),
		"truncated": good[:len(good)-20],
		"header":    good[:len(magic)+3],
	}
	for name, data := range cases {
		if _, err := Decode(data); err == nil {
			t.Errorf("%s: decoded without error", name)
		}
	}
}

func TestRejectsNewerFormat(t *testing.T) {
	header := []byte(`{"format": 99, "id": "x"}`)
	var buf bytes.Buffer
	buf.WriteString(magic)
	var n [binary.MaxVarintLen64]byte
	buf.Write(n[:binary.PutUvarint(n[:], uint64(len(header)))])
	buf.Write(header)
	_, err := Decode(buf.Bytes())
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("want a newer-format error, got %v", err)
	}
}
