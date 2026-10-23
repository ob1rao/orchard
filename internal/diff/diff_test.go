package diff

import (
	"testing"

	"github.com/ob1rao/orchard/internal/scan"
)

// build makes a tree from "path=size" leaves; directories sum their
// children, as a scan's do.
func build(files map[string]uint64) *scan.Node {
	root := &scan.Node{Name: "/root", Dir: true}
	for path, size := range files {
		n := root
		parts := split(path)
		for i, part := range parts {
			var next *scan.Node
			for _, c := range n.Children {
				if c.Name == part {
					next = c
				}
			}
			if next == nil {
				next = &scan.Node{Name: part, Parent: n, Dir: i < len(parts)-1}
				n.Children = append(n.Children, next)
			}
			n = next
		}
		n.Allocated, n.Apparent = size, size
		for p := n.Parent; p != nil; p = p.Parent {
			p.Allocated += size
			p.Apparent += size
		}
	}
	return root
}

func split(p string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			out = append(out, p[start:i])
			start = i + 1
		}
	}
	return out
}

func child(n *Node, name string) *Node {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestCompare(t *testing.T) {
	before := build(map[string]uint64{"a/keep": 10, "a/grow": 100, "gone/x": 50, "same/y": 5})
	after := build(map[string]uint64{"a/keep": 10, "a/grow": 400, "new/z": 70, "same/y": 5})
	root := Compare(before, after)
	if root.Delta(false) != 320 || root.FilesAdded != 1 || root.FilesRemoved != 1 || root.FilesChanged != 1 {
		t.Fatalf("root: delta %d, +%d -%d ~%d", root.Delta(false), root.FilesAdded, root.FilesRemoved, root.FilesChanged)
	}
	for name, want := range map[string]Status{"a": Changed, "gone": Removed, "new": Added, "same": Same} {
		if got := child(root, name); got == nil || got.Status != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	if s := child(child(root, "a"), "keep").Status; s != Same {
		t.Errorf("unchanged file marked %v", s)
	}
	if p := child(child(root, "gone"), "x").Path(); p != "/gone/x" {
		t.Errorf("path %q", p)
	}
	sorted := Sorted(root, false, true)
	if len(sorted) != 3 || sorted[0].Name != "a" || sorted[1].Name != "new" || sorted[2].Name != "gone" {
		t.Errorf("sorted by change: %v %v %v", sorted[0].Name, sorted[1].Name, sorted[2].Name)
	}
	top := Top(root, false, 2)
	if len(top) != 2 || top[0].Name != "grow" || top[1].Name != "z" {
		t.Errorf("top files: %v", top)
	}
}

func TestModifiedFileIsChanged(t *testing.T) {
	before := build(map[string]uint64{"f": 10})
	after := build(map[string]uint64{"f": 10})
	after.Children[0].Modified = 99
	if s := Compare(before, after).Children[0].Status; s != Changed {
		t.Errorf("rewritten same-size file: %v", s)
	}
}
