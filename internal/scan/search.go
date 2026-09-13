package scan

import (
	"context"
	"regexp"
	"sort"
	"strings"
)

const SearchLimit = 10000

type Query struct {
	Text                                  string
	Regex, FullPath, HideHidden, Apparent bool
}
type SearchResult struct {
	Entries []Entry
	Matches int
}

// Search inspects only the in-memory index. Copying short pages keeps the scan
// writer responsive, and matching/sorting never runs with the tree locked.
// It searches the entries discovered at invocation; callers can refresh later.
func (t *Tree) Search(ctx context.Context, q Query) (SearchResult, error) {
	var result SearchResult
	var match func(string) bool
	if q.Regex {
		r, err := regexp.Compile(q.Text)
		if err != nil {
			return result, err
		}
		match = r.MatchString
	} else {
		text := strings.ToLower(q.Text)
		match = func(s string) bool { return strings.Contains(strings.ToLower(s), text) }
	}
	if q.Text == "" {
		return result, nil
	}
	t.mu.RLock()
	end := len(t.index)
	t.mu.RUnlock()
	var page [512]Entry
	for start := 0; start < end; start += len(page) {
		if err := ctx.Err(); err != nil {
			return SearchResult{}, err
		}
		count := min(len(page), end-start)
		t.mu.RLock()
		for i, n := range t.index[start : start+count] {
			page[i] = Entry{Node: n, Name: n.Name, Dir: n.Dir, Size: n.Size(q.Apparent)}
		}
		t.mu.RUnlock()
		for _, e := range page[:count] {
			if err := ctx.Err(); err != nil {
				return SearchResult{}, err
			}
			if q.HideHidden && hiddenAncestor(e.Node, t.Root) {
				continue
			}
			target := e.Name
			if q.FullPath {
				target = e.Node.Path()
			}
			if !match(target) {
				continue
			}
			result.Matches++
			if len(result.Entries) < SearchLimit {
				result.Entries = append(result.Entries, e)
			}
		}
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if a.Size == b.Size {
			return a.Node.Path() < b.Node.Path()
		}
		return a.Size > b.Size
	})
	return result, nil
}
func hiddenAncestor(n, root *Node) bool {
	for ; n != nil && n != root; n = n.Parent {
		if strings.HasPrefix(n.Name, ".") {
			return true
		}
	}
	return false
}
