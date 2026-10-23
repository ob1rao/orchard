package scan

import "time"

// Restore wraps a finished tree that was built elsewhere, such as one decoded
// from a saved snapshot, so it can be browsed and searched like a live scan.
// Directory sizes must already include their descendants.
func Restore(root *Node, stats Stats) *Tree {
	stats.Done = true
	t := &Tree{Root: root, stats: stats, started: time.Now().Add(-stats.Elapsed)}
	var walk func(*Node)
	walk = func(n *Node) {
		for _, c := range n.Children {
			t.index = append(t.index, c)
			walk(c)
		}
	}
	walk(root)
	return t
}

// Read calls fn with the tree held against concurrent scan updates. fn must
// not keep the root beyond the call if the scan is still running.
func (t *Tree) Read(fn func(root *Node, stats Stats)) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	stats := t.stats
	stats.Allocated, stats.Apparent = t.Root.Allocated, t.Root.Apparent
	if !stats.Done {
		stats.Elapsed = time.Since(t.started)
	}
	fn(t.Root, stats)
}

// Nodes returns every entry discovered so far, excluding the root.
func (t *Tree) Nodes() []*Node {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return append([]*Node(nil), t.index...)
}

// Stats reports the scan's progress without copying any directory.
func (t *Tree) Stats() Stats {
	var s Stats
	t.Read(func(_ *Node, stats Stats) { s = stats })
	return s
}
