package query

import (
	"testing"
	"time"

	"github.com/ob1rao/orchard/internal/scan"
)

var now = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

func fixture() *scan.Tree {
	root := &scan.Node{Name: "/srv", Dir: true}
	add := func(parent *scan.Node, name string, dir bool, size uint64, age time.Duration) *scan.Node {
		n := &scan.Node{Name: name, Parent: parent, Dir: dir, Allocated: size, Apparent: size, Modified: now.Add(-age).Unix()}
		parent.Children = append(parent.Children, n)
		return n
	}
	logs := add(root, "logs", true, 3_100_000, 0)
	add(logs, "app.log", false, 3_000_000, 24*time.Hour)
	add(logs, "old.log", false, 100_000, 400*24*time.Hour)
	add(root, "disk.ISO", false, 2_000_000_000, 10*24*time.Hour)
	add(root, ".cache", true, 0, 0)
	return scan.Restore(root, scan.Stats{})
}

func run(t *testing.T, expr string) []string {
	t.Helper()
	q, err := Parse(expr)
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	var paths []string
	for _, h := range q.Run([]*scan.Tree{fixture()}, 0, now).Hits {
		paths = append(paths, h.Path)
	}
	return paths
}

func TestQueries(t *testing.T) {
	cases := map[string][]string{
		"log":                      {"/logs", "/logs/app.log", "/logs/old.log"},
		"ext:iso":                  {"/disk.ISO"},
		"type:file size>1M":        {"/disk.ISO", "/logs/app.log"},
		"size>=2G":                 {"/disk.ISO"},
		"type:dir":                 {"/logs", "/.cache"},
		"age>1y":                   {"/logs/old.log"},
		"modified<2026-05-25 -iso": {"/logs/old.log"},
		"path:/logs/*":             {"/logs/app.log", "/logs/old.log"},
		`re:^[a-z]+\.log$`:         {"/logs/app.log", "/logs/old.log"},
		`"app.log"`:                {"/logs/app.log"},
	}
	for expr, want := range cases {
		got := run(t, expr)
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", expr, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: got %v, want %v", expr, got, want)
				break
			}
		}
	}
}

func TestBadQueries(t *testing.T) {
	for _, expr := range []string{"", "size>", "size>lots", "type:socket", "re:(", "colour:red", `"open`, "modified<yesterday", "age>forever"} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("%q parsed", expr)
		}
	}
}

func TestMachineScope(t *testing.T) {
	q, _ := Parse("machine:web1 log")
	if !q.WantsMachine("WEB1") || q.WantsMachine("db") {
		t.Error("machine: should select only that machine")
	}
	q, _ = Parse("-machine:web1 log")
	if q.WantsMachine("web1") || !q.WantsMachine("db") {
		t.Error("-machine: should exclude that machine")
	}
}

func TestLimitKeepsLargest(t *testing.T) {
	q, _ := Parse("type:file")
	res := q.Run([]*scan.Tree{fixture()}, 1, now)
	if res.Matches != 3 || len(res.Hits) != 1 || res.Hits[0].Path != "/disk.ISO" {
		t.Fatalf("limit: %+v", res)
	}
}

func TestSizes(t *testing.T) {
	for in, want := range map[string]uint64{"500": 500, "1k": 1000, "1.5G": 1_500_000_000, "2MiB": 2 << 20, "3TB": 3e12} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}
