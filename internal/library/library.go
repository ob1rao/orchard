// Package library keeps saved snapshots and machine profiles on disk.
//
// Layout under the data directory:
//
//	snapshots/<machine>/<id>.orchard   one file per saved scan
//	machines.json                      saved machine profiles
//
// The directory is $ORCHARD_HOME when set, otherwise $XDG_DATA_HOME/orchard,
// otherwise ~/.local/share/orchard.
package library

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/snapshot"
)

const Ext = ".orchard"

type Library struct{ Dir string }

// DefaultDir resolves the data directory without creating it.
func DefaultDir() (string, error) {
	if d := os.Getenv("ORCHARD_HOME"); d != "" {
		return d, nil
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "orchard"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "orchard"), nil
}

func Open() (*Library, error) {
	d, err := DefaultDir()
	if err != nil {
		return nil, err
	}
	return &Library{Dir: d}, nil
}

// Entry is one saved snapshot: its header and where it lives.
type Entry struct {
	snapshot.Meta
	Path string `json:"path"`
	Size int64  `json:"file_bytes"`
}

// SafeName turns a machine name into one directory component.
func SafeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, s)
	s = strings.Trim(s, ".")
	if s == "" {
		return "unnamed"
	}
	return s
}

func (l *Library) snapshotDir() string { return filepath.Join(l.Dir, "snapshots") }

// Save writes a tree to the library and returns its entry. The meta's ID is
// assigned when empty.
func (l *Library) Save(meta snapshot.Meta, t *scan.Tree) (Entry, error) {
	if meta.ID == "" {
		meta.ID = snapshot.NewID()
	}
	if meta.Machine == "" {
		meta.Machine = snapshot.Hostname()
	}
	dir := filepath.Join(l.snapshotDir(), SafeName(meta.Machine))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Entry{}, err
	}
	path := filepath.Join(dir, meta.ID+Ext)
	if err := writeAtomic(path, func(f *os.File) error { return snapshot.Write(f, meta, t) }); err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Meta: meta, Path: path, Size: info.Size()}, nil
}

// Import copies a snapshot file into the library unless its ID is already
// there. It reports whether anything was added.
func (l *Library) Import(src string) (Entry, bool, error) {
	meta, err := snapshot.ReadMetaFile(src)
	if err != nil {
		return Entry{}, false, fmt.Errorf("%s: %w", src, err)
	}
	if existing, err := l.ByID(meta.ID); err == nil {
		return existing, false, nil
	}
	in, err := os.Open(src)
	if err != nil {
		return Entry{}, false, err
	}
	defer in.Close()
	return l.importReader(meta, in)
}

func (l *Library) importReader(meta snapshot.Meta, r io.Reader) (Entry, bool, error) {
	if meta.ID == "" || SafeName(meta.ID) != meta.ID {
		return Entry{}, false, errors.New("snapshot has no usable ID")
	}
	dir := filepath.Join(l.snapshotDir(), SafeName(meta.Machine))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Entry{}, false, err
	}
	path := filepath.Join(dir, meta.ID+Ext)
	err := writeAtomic(path, func(f *os.File) error {
		_, err := f.ReadFrom(r)
		return err
	})
	if err != nil {
		return Entry{}, false, err
	}
	// Decode what was written: an archive entry is only trusted once it reads.
	if _, err := snapshot.ReadFile(path); err != nil {
		os.Remove(path)
		return Entry{}, false, err
	}
	info, _ := os.Stat(path)
	return Entry{Meta: meta, Path: path, Size: info.Size()}, true, nil
}

func writeAtomic(path string, fill func(*os.File) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".partial-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := fill(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// List returns every readable snapshot, newest first. Unreadable files are
// skipped rather than hiding the rest of the library.
func (l *Library) List() ([]Entry, error) {
	var out []Entry
	machines, err := os.ReadDir(l.snapshotDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	for _, m := range machines {
		if !m.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(l.snapshotDir(), m.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), Ext) {
				continue
			}
			path := filepath.Join(l.snapshotDir(), m.Name(), f.Name())
			meta, err := snapshot.ReadMetaFile(path)
			if err != nil {
				continue
			}
			var size int64
			if info, err := f.Info(); err == nil {
				size = info.Size()
			}
			out = append(out, Entry{Meta: meta, Path: path, Size: size})
		}
	}
	SortNewest(out)
	return out, nil
}

func SortNewest(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		if !es[i].ScannedAt.Equal(es[j].ScannedAt) {
			return es[i].ScannedAt.After(es[j].ScannedAt)
		}
		return es[i].ID < es[j].ID
	})
}

func (l *Library) ByID(id string) (Entry, error) {
	all, err := l.List()
	if err != nil {
		return Entry{}, err
	}
	for _, e := range all {
		if e.ID == id {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("no snapshot %q", id)
}

// Latest returns the newest snapshot of each machine and scanned root.
func Latest(all []Entry) []Entry {
	seen := map[string]bool{}
	var out []Entry
	for _, e := range all { // all is newest first
		key := e.Machine + "\x00" + e.Root
		if !seen[key] {
			seen[key] = true
			out = append(out, e)
		}
	}
	return out
}

// Resolve finds the snapshot a reference names. A reference is, in order of
// precedence: a path to a snapshot file; a machine name, meaning its newest
// snapshot, optionally followed by ~N for the Nth one before it; or a unique
// ID prefix.
func (l *Library) Resolve(ref string) (Entry, error) {
	if ref == "" {
		return Entry{}, errors.New("empty snapshot reference")
	}
	if strings.ContainsRune(ref, os.PathSeparator) || strings.HasSuffix(ref, Ext) {
		if info, err := os.Stat(ref); err == nil && !info.IsDir() {
			meta, err := snapshot.ReadMetaFile(ref)
			if err != nil {
				return Entry{}, fmt.Errorf("%s: %w", ref, err)
			}
			return Entry{Meta: meta, Path: ref, Size: info.Size()}, nil
		}
	}
	all, err := l.List()
	if err != nil {
		return Entry{}, err
	}
	name, back := ref, 0
	if i := strings.LastIndexByte(ref, '~'); i > 0 {
		n, err := strconv.Atoi(ref[i+1:])
		if err == nil && n >= 0 {
			name, back = ref[:i], n
		}
	}
	var history []Entry
	for _, e := range all {
		if strings.EqualFold(e.Machine, name) {
			history = append(history, e)
		}
	}
	if len(history) > 0 {
		if back >= len(history) {
			return Entry{}, fmt.Errorf("%s has only %d snapshots", name, len(history))
		}
		return history[back], nil
	}
	var matches []Entry
	for _, e := range all {
		if strings.HasPrefix(e.ID, strings.ToLower(ref)) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return Entry{}, fmt.Errorf("no snapshot or machine matches %q (see: orchard snapshots)", ref)
	}
	return Entry{}, fmt.Errorf("%q matches %d snapshots; use more of the ID", ref, len(matches))
}

// Load decodes the snapshot an entry points at.
func Load(e Entry) (*snapshot.Snapshot, error) { return snapshot.ReadFile(e.Path) }

// Delete removes a snapshot from the library.
func (l *Library) Delete(e Entry) error {
	rel, err := filepath.Rel(l.snapshotDir(), e.Path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("%s is not inside the library", e.Path)
	}
	if err := os.Remove(e.Path); err != nil {
		return err
	}
	// Leave no empty machine directory behind.
	_ = os.Remove(filepath.Dir(e.Path))
	return nil
}

// Age formats how long ago a time was, for listings.
func Age(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
