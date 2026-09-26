// Package scan builds a live, inode-aware usage tree without following symlinks.
package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type Node struct {
	Name                string
	Parent              *Node
	Dir                 bool
	HasCreated          bool
	Modified, Created   int64 // Unix seconds; Created is valid only when HasCreated is true.
	Allocated, Apparent uint64
	Children            []*Node
}

func (n *Node) Path() string {
	var parts []string
	for p := n; p != nil; p = p.Parent {
		parts = append(parts, p.Name)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return filepath.Join(parts...)
}
func (n *Node) Size(apparent bool) uint64 {
	if apparent {
		return n.Apparent
	}
	return n.Allocated
}

type Stats struct {
	Files, Directories, Errors, Skipped, Hardlinks uint64
	Allocated, Apparent                            uint64
	Done, Cancelled                                bool
	LastError                                      string
	Elapsed                                        time.Duration
}
type Entry struct {
	Node *Node
	Name string
	Dir  bool
	Size uint64
}
type View struct {
	Entries []Entry
	Size    uint64
	Stats   Stats
}
type Tree struct {
	mu      sync.RWMutex
	Root    *Node
	stats   Stats
	started time.Time
	index   []*Node // append-only search index, protected by mu
}

func (t *Tree) Snapshot(n *Node, apparent bool) View {
	t.mu.RLock()
	v := View{Size: n.Size(apparent), Stats: t.stats, Entries: make([]Entry, 0, len(n.Children))}
	v.Stats.Allocated = t.Root.Allocated
	v.Stats.Apparent = t.Root.Apparent
	if !v.Stats.Done {
		v.Stats.Elapsed = time.Since(t.started)
	}
	for _, c := range n.Children {
		v.Entries = append(v.Entries, Entry{c, c.Name, c.Dir, c.Size(apparent)})
	}
	t.mu.RUnlock()
	sort.Slice(v.Entries, func(i, j int) bool {
		if v.Entries[i].Size == v.Entries[j].Size {
			return v.Entries[i].Name < v.Entries[j].Name
		}
		return v.Entries[i].Size > v.Entries[j].Size
	})
	return v
}

// Options carries scan settings. A Workers of zero tunes the count to the
// storage behind the scanned path.
type Options struct{ Workers int }

type identity struct{ dev, ino uint64 }
type job struct {
	node *Node
	path string
	id   identity
}
type item struct {
	name                string
	dir                 bool
	allocated, apparent uint64
	id                  identity
	links               uint64
	mode                uint32
	modified, created   int64
	hasCreated          bool
	mount               uint64
	hasMount            bool
}

// boundary marks the filesystem a scan started on. Btrfs gives every subvolume
// its own device number while keeping it in one mount, so comparing devices
// alone silently drops /home or /.snapshots from a root scan. Mount IDs say
// exactly what a device number only approximates; kernels before 5.8 and
// platforms without statx report none, and fall back to the device.
type boundary struct {
	dev, mount uint64
	hasMount   bool
}

func (b boundary) holds(it item) bool {
	if b.hasMount && it.hasMount {
		return it.mount == b.mount
	}
	return it.id.dev == b.dev
}

type batch struct {
	job   job
	items []item
	err   error
	done  bool
}

func Start(ctx context.Context, path string, opt Options) (*Tree, <-chan struct{}, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	// Resolve an explicitly selected symlink once; discovered symlinks are never traversed.
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", path)
	}
	st := info.Sys().(*syscall.Stat_t)
	id := identity{uint64(st.Dev), uint64(st.Ino)}
	root := &Node{Name: path, Dir: true, Allocated: uint64(max(0, st.Blocks)) * 512, Apparent: uint64(max(0, info.Size())), Modified: info.ModTime().Unix()}
	bound := boundary{dev: id.dev}
	if meta, err := readMetadata(unix.AT_FDCWD, path); err == nil && meta.id == id {
		root.Created, root.HasCreated = meta.created, meta.hasCreated
		bound.mount, bound.hasMount = meta.mount, meta.hasMount
	}
	t := &Tree{Root: root, started: time.Now(), stats: Stats{Directories: 1}}
	done := make(chan struct{})
	workers := opt.Workers
	if workers <= 0 {
		workers = WorkersFor(path)
	}
	workers = min(workers, 64)
	go func() {
		defer close(done)
		t.run(ctx, job{root, path, id}, bound, workers)
	}()
	return t, done, nil
}

// chunk is one directory read: the entries it produced, any per-entry metadata
// failures, and whether the directory is now exhausted. Per-entry failures are
// reported without abandoning the rest of the directory.
type chunk struct {
	items []item
	errs  []error
	done  bool
	err   error
}

// cancelEvery bounds how long a cancelled scan keeps making metadata calls.
// Checking once per entry costs more than it saves; on a stalled network mount
// each call can block, so the window cannot be much wider either.
const cancelEvery = 64

// countDirents sizes a chunk exactly, so a directory read never grows and
// copies its entry slice. Walking the record lengths costs far less than the
// metadata calls that follow.
func countDirents(buf []byte) int {
	n := 0
	for off := 0; off < len(buf); {
		reclen := direntReclen(buf, off)
		if reclen < direntNameOffset || off+reclen > len(buf) {
			break
		}
		off += reclen
		n++
	}
	return n
}

func readDirectory(ctx context.Context, j job, out chan<- batch, r *dirReader) {
	send := func(b batch) bool {
		select {
		case out <- b:
			return true
		case <-ctx.Done():
			return false
		}
	}
	fd, err := unix.Open(j.path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		send(batch{job: j, err: err, done: true})
		return
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		send(batch{job: j, err: err, done: true})
		return
	}
	if (identity{uint64(st.Dev), uint64(st.Ino)}) != j.id {
		send(batch{job: j, err: fmt.Errorf("directory changed during scan"), done: true})
		return
	}
	r.open(fd)
	for {
		if ctx.Err() != nil {
			return
		}
		c := r.read(ctx, fd)
		for _, e := range c.errs {
			if !send(batch{job: j, err: e}) {
				return
			}
		}
		if !send(batch{job: j, items: c.items, err: c.err, done: c.done}) || c.done {
			return
		}
	}
}

func (t *Tree) run(ctx context.Context, root job, bound boundary, workers int) {
	jobs := make(chan job)
	results := make(chan batch, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each worker keeps its own directory buffers for the whole scan.
			r := newDirReader()
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-jobs:
					if !ok {
						return
					}
					readDirectory(ctx, j, results, r)
				}
			}
		}()
	}
	defer func() {
		close(jobs)
		wg.Wait()
		t.mu.Lock()
		t.stats.Done = true
		t.stats.Cancelled = ctx.Err() != nil
		t.stats.Elapsed = time.Since(t.started)
		t.mu.Unlock()
	}()
	queue := []job{root}
	head, active := 0, 0
	dirs := map[identity]bool{root.id: true}
	links := map[identity]bool{}
	for head < len(queue) || active > 0 {
		var dispatch chan job
		var next job
		if head < len(queue) {
			dispatch = jobs
			next = queue[head]
		}
		select {
		case <-ctx.Done():
			return
		case dispatch <- next:
			queue[head] = job{}
			head++
			active++
			if head >= 1024 && head*2 >= len(queue) {
				queue = append([]job(nil), queue[head:]...)
				head = 0
			}
		case b := <-results:
			if b.done {
				active--
			}
			t.mu.Lock()
			if b.err != nil {
				t.stats.Errors++
				t.stats.LastError = fmt.Sprintf("%s: %v", b.job.path, b.err)
			}
			var allocated, apparent uint64
			for _, it := range b.items {
				if !bound.holds(it) {
					t.stats.Skipped++
					continue
				}
				if it.dir {
					if dirs[it.id] {
						t.stats.Skipped++
						continue
					}
					dirs[it.id] = true
				}
				if !it.dir && it.links > 1 {
					if links[it.id] {
						t.stats.Hardlinks++
						it.allocated = 0
						it.apparent = 0
					} else {
						links[it.id] = true
					}
				}
				n := &Node{Name: it.name, Parent: b.job.node, Dir: it.dir, Allocated: it.allocated, Apparent: it.apparent, Modified: it.modified, Created: it.created, HasCreated: it.hasCreated}
				b.job.node.Children = append(b.job.node.Children, n)
				t.index = append(t.index, n)
				allocated += it.allocated
				apparent += it.apparent
				if it.dir {
					t.stats.Directories++
					queue = append(queue, job{n, filepath.Join(b.job.path, it.name), it.id})
				} else {
					t.stats.Files++
				}
			}
			for n := b.job.node; n != nil; n = n.Parent {
				n.Allocated += allocated
				n.Apparent += apparent
			}
			t.mu.Unlock()
		}
	}
}
