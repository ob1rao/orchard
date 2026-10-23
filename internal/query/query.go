// Package query filters entries across saved snapshots.
//
// An expression is a list of space-separated terms that must all match:
//
//	report          name contains "report" (ignoring case)
//	re:^core\.\d+$  name matches a regular expression
//	path:/var/log   path relative to the scan root contains this, or matches it as a glob when it has * ? [
//	ext:iso         file extension, without the dot
//	type:file       file or dir
//	size>1G         size comparison: > >= < <= =, with K M G T P (powers of 1000) or KiB MiB GiB TiB
//	modified<2024-01-01   modified before or after a date (YYYY-MM-DD)
//	age>90d         modified longer ago than this (d, w, m = 30 days, y = 365 days, or a Go duration)
//	machine:web1    only this machine's snapshots
//
// A term starting with "-" is negated. Quote a term with double quotes to
// include spaces.
package query

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ob1rao/orchard/internal/scan"
)

type Entry struct {
	Node *scan.Node
	Path string // relative to the scan root, starting with "/"
}

type term struct {
	negate  bool
	machine string // only meaningful to snapshot selection
	match   func(e Entry, size uint64, now time.Time) bool
}

// Query is a parsed expression.
type Query struct {
	terms    []term
	Machines []string // machine names the expression restricts to
	Apparent bool
	Source   string
}

func Parse(expr string) (*Query, error) {
	q := &Query{Source: expr}
	fields, err := split(expr)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, errors.New("empty query")
	}
	for _, f := range fields {
		t, err := parseTerm(f)
		if err != nil {
			return nil, err
		}
		if t.machine != "" {
			if !t.negate {
				q.Machines = append(q.Machines, t.machine)
			}
		}
		q.terms = append(q.terms, t)
	}
	return q, nil
}

func split(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	quoted, have := false, false
	for _, r := range s {
		switch {
		case r == '"':
			quoted, have = !quoted, true
		case unicode.IsSpace(r) && !quoted:
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if quoted {
		return nil, errors.New("unterminated quote")
	}
	if have {
		out = append(out, cur.String())
	}
	return out, nil
}

var comparison = regexp.MustCompile(`^(size|modified|age)(>=|<=|>|<|=)(.*)$`)

func parseTerm(f string) (term, error) {
	var t term
	if len(f) > 1 && f[0] == '-' {
		t.negate, f = true, f[1:]
	}
	if m := comparison.FindStringSubmatch(f); m != nil {
		if m[3] == "" {
			return t, fmt.Errorf("%s%s needs a value, like size>1G", m[1], m[2])
		}
		cmp, err := comparer(m[2])
		if err != nil {
			return t, err
		}
		switch m[1] {
		case "size":
			n, err := ParseSize(m[3])
			if err != nil {
				return t, err
			}
			t.match = func(_ Entry, size uint64, _ time.Time) bool { return cmp(compareU(size, n)) }
		case "modified":
			d, err := time.ParseInLocation("2006-01-02", m[3], time.Local)
			if err != nil {
				return t, fmt.Errorf("modified wants a date like 2024-01-31: %q", m[3])
			}
			at := d.Unix()
			t.match = func(e Entry, _ uint64, _ time.Time) bool { return cmp(compareI(e.Node.Modified, at)) }
		case "age":
			d, err := ParseAge(m[3])
			if err != nil {
				return t, err
			}
			t.match = func(e Entry, _ uint64, now time.Time) bool {
				return cmp(compareI(now.Unix()-e.Node.Modified, int64(d.Seconds())))
			}
		}
		return t, nil
	}
	key, value, hasKey := strings.Cut(f, ":")
	if !hasKey || value == "" {
		text := strings.ToLower(f)
		t.match = func(e Entry, _ uint64, _ time.Time) bool { return strings.Contains(strings.ToLower(e.Node.Name), text) }
		return t, nil
	}
	switch strings.ToLower(key) {
	case "name":
		text := strings.ToLower(value)
		t.match = func(e Entry, _ uint64, _ time.Time) bool { return strings.Contains(strings.ToLower(e.Node.Name), text) }
	case "re", "regex":
		r, err := regexp.Compile(value)
		if err != nil {
			return t, fmt.Errorf("re: %w", err)
		}
		t.match = func(e Entry, _ uint64, _ time.Time) bool { return r.MatchString(e.Node.Name) }
	case "path":
		if strings.ContainsAny(value, "*?[") {
			if _, err := path.Match(value, ""); err != nil {
				return t, fmt.Errorf("path glob: %w", err)
			}
			t.match = func(e Entry, _ uint64, _ time.Time) bool {
				ok, _ := path.Match(value, e.Path)
				return ok
			}
		} else {
			t.match = func(e Entry, _ uint64, _ time.Time) bool { return strings.Contains(e.Path, value) }
		}
	case "ext":
		ext := "." + strings.ToLower(strings.TrimPrefix(value, "."))
		t.match = func(e Entry, _ uint64, _ time.Time) bool {
			return !e.Node.Dir && strings.ToLower(filepath.Ext(e.Node.Name)) == ext
		}
	case "type":
		switch strings.ToLower(value) {
		case "f", "file":
			t.match = func(e Entry, _ uint64, _ time.Time) bool { return !e.Node.Dir }
		case "d", "dir", "directory":
			t.match = func(e Entry, _ uint64, _ time.Time) bool { return e.Node.Dir }
		default:
			return t, fmt.Errorf("type wants file or dir, not %q", value)
		}
	case "machine", "host":
		t.machine = value
		t.match = func(Entry, uint64, time.Time) bool { return true }
	default:
		return t, fmt.Errorf("unknown query key %q (try name: re: path: ext: type: machine: size> modified< age>)", key)
	}
	return t, nil
}

func comparer(op string) (func(int) bool, error) {
	switch op {
	case ">":
		return func(c int) bool { return c > 0 }, nil
	case ">=":
		return func(c int) bool { return c >= 0 }, nil
	case "<":
		return func(c int) bool { return c < 0 }, nil
	case "<=":
		return func(c int) bool { return c <= 0 }, nil
	case "=":
		return func(c int) bool { return c == 0 }, nil
	}
	return nil, fmt.Errorf("unknown comparison %q", op)
}
func compareU(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
func compareI(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

var sizePattern = regexp.MustCompile(`^(?i)([0-9]+(?:\.[0-9]+)?)\s*([kmgtp]?)(i?)(b?)$`)

// ParseSize reads sizes like 500, 1.5G, 10MB or 4GiB.
func ParseSize(s string) (uint64, error) {
	m := sizePattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("size %q: use a number with an optional K, M, G, T or P", s)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}
	unit := 1000.0
	if m[3] != "" {
		unit = 1024
	}
	exp := strings.Index("kmgtp", strings.ToLower(m[2])) + 1
	if m[2] == "" {
		exp = 0
	}
	for range exp {
		v *= unit
	}
	return uint64(v), nil
}

// ParseAge reads durations like 30d, 2w, 6m, 1y or 36h.
func ParseAge(s string) (time.Duration, error) {
	if len(s) > 1 {
		n, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err == nil {
			day := 24 * time.Hour
			switch s[len(s)-1] {
			case 'd':
				return time.Duration(n * float64(day)), nil
			case 'w':
				return time.Duration(n * float64(7*day)), nil
			case 'm':
				return time.Duration(n * float64(30*day)), nil
			case 'y':
				return time.Duration(n * float64(365*day)), nil
			}
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("age %q: use a duration like 30d, 2w, 6m or 1y", s)
	}
	return d, nil
}

// WantsMachine reports whether a machine's snapshots are in scope.
func (q *Query) WantsMachine(name string) bool {
	for _, t := range q.terms {
		if t.machine != "" && strings.EqualFold(t.machine, name) == t.negate {
			return false
		}
	}
	return true
}

// Match tests one entry.
func (q *Query) Match(e Entry, now time.Time) bool {
	size := e.Node.Size(q.Apparent)
	for _, t := range q.terms {
		if t.match(e, size, now) == t.negate {
			return false
		}
	}
	return true
}

// Hit is a matching entry with the snapshot it came from.
type Hit struct {
	Entry
	Size     uint64
	Snapshot int // index into the snapshots searched
}

// Result is a capped list of hits, largest first.
type Result struct {
	Hits    []Hit
	Matches int
	Bytes   uint64 // total of every match, which can double count nested directories
}

// Run searches trees. Hits beyond limit are counted but not kept; a limit of
// zero keeps everything.
func (q *Query) Run(trees []*scan.Tree, limit int, now time.Time) Result {
	var res Result
	for i, t := range trees {
		root := t.Root
		for _, n := range t.Nodes() {
			e := Entry{Node: n, Path: relative(n, root)}
			if !q.Match(e, now) {
				continue
			}
			size := n.Size(q.Apparent)
			res.Matches++
			res.Bytes += size
			res.Hits = append(res.Hits, Hit{Entry: e, Size: size, Snapshot: i})
			// A broad query across a fleet can match millions of entries;
			// trimming as it goes keeps memory proportional to the limit.
			if limit > 0 && len(res.Hits) >= 4*limit+1024 {
				res.Hits = top(res.Hits, limit)
			}
		}
	}
	res.Hits = top(res.Hits, limit)
	return res
}

func top(hits []Hit, limit int) []Hit {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Size != hits[j].Size {
			return hits[i].Size > hits[j].Size
		}
		return hits[i].Path < hits[j].Path
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func relative(n, root *scan.Node) string {
	var parts []string
	for p := n; p != nil && p != root; p = p.Parent {
		parts = append(parts, p.Name)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return "/" + path.Join(parts...)
}
