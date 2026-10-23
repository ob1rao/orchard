// Package diff compares two scan trees by path relative to their roots, so a
// snapshot of one machine can be compared with another's even when they were
// scanned from different directories.
package diff

import (
	"path"
	"sort"

	"github.com/ob1rao/orchard/internal/scan"
)

type Status uint8

const (
	Same Status = iota
	Changed
	Added
	Removed
)

func (s Status) String() string {
	return [...]string{"same", "changed", "added", "removed"}[s]
}

// Node is one path present on either side. Sizes include descendants.
type Node struct {
	Name                    string
	Parent                  *Node
	Dir                     bool
	Status                  Status
	BeforeAlloc, AfterAlloc uint64
	BeforeApp, AfterApp     uint64
	BeforeMod, AfterMod     int64
	// Files counts the files below a directory that were added, removed or
	// changed; a file counts itself.
	FilesAdded, FilesRemoved, FilesChanged uint64
	Children                               []*Node
}

func (n *Node) Before(apparent bool) uint64 {
	if apparent {
		return n.BeforeApp
	}
	return n.BeforeAlloc
}
func (n *Node) After(apparent bool) uint64 {
	if apparent {
		return n.AfterApp
	}
	return n.AfterAlloc
}

// Delta is After minus Before.
func (n *Node) Delta(apparent bool) int64 {
	return int64(n.After(apparent)) - int64(n.Before(apparent))
}

// Magnitude is the absolute delta, which is what a treemap of change weighs.
func (n *Node) Magnitude(apparent bool) uint64 {
	d := n.Delta(apparent)
	if d < 0 {
		return uint64(-d)
	}
	return uint64(d)
}

// Path is relative to the compared roots; the root itself is "/".
func (n *Node) Path() string {
	var parts []string
	for p := n; p != nil && p.Parent != nil; p = p.Parent {
		parts = append(parts, p.Name)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return "/" + path.Join(parts...)
}

// Compare builds the union of both trees. Either side may be nil.
func Compare(before, after *scan.Node) *Node {
	root := compare(before, after, nil)
	root.Name = "/"
	return root
}

func compare(a, b *scan.Node, parent *Node) *Node {
	n := &Node{Parent: parent}
	switch {
	case a == nil:
		n.Name, n.Dir, n.Status = b.Name, b.Dir, Added
	case b == nil:
		n.Name, n.Dir, n.Status = a.Name, a.Dir, Removed
	default:
		n.Name, n.Dir = b.Name, a.Dir || b.Dir
	}
	if a != nil {
		n.BeforeAlloc, n.BeforeApp, n.BeforeMod = a.Allocated, a.Apparent, a.Modified
	}
	if b != nil {
		n.AfterAlloc, n.AfterApp, n.AfterMod = b.Allocated, b.Apparent, b.Modified
	}
	var ac, bc []*scan.Node
	if a != nil && a.Dir {
		ac = a.Children
	}
	if b != nil && b.Dir {
		bc = b.Children
	}
	if len(ac)+len(bc) > 0 {
		byName := make(map[string]*scan.Node, len(ac))
		for _, c := range ac {
			byName[c.Name] = c
		}
		n.Children = make([]*Node, 0, max(len(ac), len(bc)))
		for _, c := range bc {
			old := byName[c.Name]
			delete(byName, c.Name)
			n.Children = append(n.Children, compare(old, c, n))
		}
		for _, c := range ac {
			if _, gone := byName[c.Name]; gone {
				n.Children = append(n.Children, compare(c, nil, n))
			}
		}
	}
	if !n.Dir {
		switch n.Status {
		case Added:
			n.FilesAdded = 1
		case Removed:
			n.FilesRemoved = 1
		default:
			if a.Dir != b.Dir || n.BeforeApp != n.AfterApp || n.BeforeAlloc != n.AfterAlloc || n.BeforeMod != n.AfterMod {
				n.Status, n.FilesChanged = Changed, 1
			}
		}
		return n
	}
	for _, c := range n.Children {
		n.FilesAdded += c.FilesAdded
		n.FilesRemoved += c.FilesRemoved
		n.FilesChanged += c.FilesChanged
	}
	if n.Status == Same && (n.FilesAdded+n.FilesRemoved+n.FilesChanged > 0 || n.BeforeAlloc != n.AfterAlloc || n.BeforeApp != n.AfterApp || a.Dir != b.Dir) {
		n.Status = Changed
	}
	return n
}

// Sorted returns a directory's children, largest change first, optionally
// leaving out the ones that did not change.
func Sorted(n *Node, apparent, hideSame bool) []*Node {
	out := make([]*Node, 0, len(n.Children))
	for _, c := range n.Children {
		if hideSame && c.Status == Same {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		mi, mj := out[i].Magnitude(apparent), out[j].Magnitude(apparent)
		if mi != mj {
			return mi > mj
		}
		if out[i].Status != out[j].Status {
			return out[i].Status > out[j].Status
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Top returns the n files with the largest change anywhere in the tree.
func Top(root *Node, apparent bool, n int) []*Node {
	var files []*Node
	var walk func(*Node)
	walk = func(d *Node) {
		for _, c := range d.Children {
			if c.Status == Same {
				continue
			}
			if c.Dir {
				walk(c)
			} else {
				files = append(files, c)
			}
		}
	}
	walk(root)
	sort.SliceStable(files, func(i, j int) bool { return files[i].Magnitude(apparent) > files[j].Magnitude(apparent) })
	if len(files) > n {
		files = files[:n]
	}
	return files
}
